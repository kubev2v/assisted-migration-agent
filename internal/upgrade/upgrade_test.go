package upgrade_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	pkgstore "github.com/kubev2v/migration-planner/pkg/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/internal/upgrade"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
	"github.com/kubev2v/assisted-migration-agent/test"
)

func TestUpgrade(t *testing.T) { RegisterFailHandler(Fail); RunSpecs(t, "Host upgrade lifecycle") }

type heldCollection struct {
	started    bool
	finalizing chan struct{}
	finish     chan struct{}
}

func (b *heldCollection) Build() work.WorkBuilder[models.CollectorStatus, models.CollectorResult] {
	return b
}
func (b *heldCollection) Next() (work.WorkUnit[models.CollectorStatus, models.CollectorResult], bool) {
	if b.started {
		return work.WorkUnit[models.CollectorStatus, models.CollectorResult]{}, false
	}
	b.started = true
	return work.WorkUnit[models.CollectorStatus, models.CollectorResult]{
		Status: func() models.CollectorStatus { return models.CollectorStatus{State: models.CollectorStateCollected} },
		Work: func(_ context.Context, _ models.CollectorResult) (models.CollectorResult, error) {
			return models.CollectorResult{Completed: true}, nil
		},
	}, true
}
func (b *heldCollection) Finalize(ctx context.Context, _ models.CollectorResult) error {
	close(b.finalizing)
	select {
	case <-b.finish:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ = Describe("Host coordinated upgrades", func() {
	var folder, oldImage, newImage string
	var lifecycle *upgrade.Service
	var request upgrade.Request
	BeforeEach(func() {
		folder = GinkgoT().TempDir()
		oldImage = "sha256:" + strings.Repeat("a", 64)
		newImage = "sha256:" + strings.Repeat("b", 64)
		db, err := sql.Open("duckdb", filepath.Join(folder, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())
		Expect(migrations.RunMain(context.Background(), db)).To(Succeed())
		Expect(db.Close()).To(Succeed())
		db, err = sql.Open("duckdb", filepath.Join(folder, "collection_100.duckdb"))
		Expect(err).NotTo(HaveOccurred())
		Expect(duckdb_parser.New(pkgstore.NewQueryInterceptor(db), test.NewMockValidator()).Init()).To(Succeed())
		Expect(migrations.RunCollection(context.Background(), db, "collection_100")).To(Succeed())
		_, err = db.Exec(`INSERT INTO inventory(id,data,is_subset) VALUES ('saved','{}',false)`)
		Expect(err).NotTo(HaveOccurred())
		Expect(db.Close()).To(Succeed())
		Expect(os.WriteFile(filepath.Join(folder, "credentials.key"), []byte(strings.Repeat("k", 32)), 0600)).To(Succeed())
		pool := store.NewPool(time.Minute)
		runtime := services.NewServiceManager(folder, services.WithPool(pool))
		lifecycle, err = upgrade.NewService(folder, runtime, time.Second)
		Expect(err).NotTo(HaveOccurred())
		request = upgrade.Request{Action: "maintenance", AttemptID: "attempt-1", PreviousImage: oldImage, CandidateImage: newImage}
	})
	AfterEach(func() { lifecycle.Close() })

	It("validates real stores without changing database bytes or replacing assets", func() {
		before, err := os.ReadFile(filepath.Join(folder, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())
		snapshot, err := store.ReadUpgradeSnapshot(context.Background(), folder)
		Expect(err).NotTo(HaveOccurred())
		Expect(snapshot.Databases).To(Equal([]string{"agent.duckdb", "collection_100.duckdb"}))
		Expect(snapshot.Assets).To(HaveKey("credentials.key"))
		after, err := os.ReadFile(filepath.Join(folder, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())
		Expect(after).To(Equal(before))
	})
	It("closes admission immediately and waits for admitted requests", func() {
		Expect(lifecycle.BeginRequest()).To(BeTrue())
		Expect(lifecycle.Transition(request)).To(Succeed())
		Expect(lifecycle.BeginRequest()).To(BeFalse())
		Consistently(func() string { return lifecycle.Status().State }, 100*time.Millisecond).Should(Equal("draining"))
		lifecycle.EndRequest()
		Eventually(func() string { return lifecycle.Status().State }, 3*time.Second).Should(Equal("maintenance"))
		Expect(lifecycle.Transition(request)).To(Succeed())
		other := request
		other.AttemptID = "another"
		Expect(lifecycle.Transition(other)).NotTo(Succeed())
	})
	It("times out without touching stores or canceling active requests", func() {
		Expect(lifecycle.BeginRequest()).To(BeTrue())
		Expect(lifecycle.Transition(request)).To(Succeed())
		Eventually(func() string { return lifecycle.Status().State }, 3*time.Second).Should(Equal("failed"))
		Expect(lifecycle.Status().Error).To(ContainSubstring("no migration has run"))
		Expect(lifecycle.Status().ServicesClosed).To(BeFalse())
		_, err := os.Stat(filepath.Join(folder, "updates/validation/attempt-1.json"))
		Expect(os.IsNotExist(err)).To(BeTrue())
		Eventually(func() error {
			return lifecycle.Transition(upgrade.Request{Action: "resume", AttemptID: request.AttemptID})
		}, time.Second).Should(Succeed())
		Expect(lifecycle.Status().State).To(Equal("normal"))
		Expect(lifecycle.BeginRequest()).To(BeTrue())
		lifecycle.EndRequest()
		Expect(lifecycle.Transition(upgrade.Request{Action: "resume", AttemptID: request.AttemptID})).To(Succeed())
		lifecycle.EndRequest()
		request.AttemptID = "retry-after-timeout"
		Expect(lifecycle.Transition(request)).To(Succeed())
		Eventually(func() string { return lifecycle.Status().State }, 3*time.Second).Should(Equal("maintenance"))
	})
	It("preserves a collection still finalizing when draining times out", func() {
		lifecycle.Close()
		builder := &heldCollection{finalizing: make(chan struct{}), finish: make(chan struct{})}
		runtime := services.NewServiceManager(folder, services.WithPool(store.NewPool(time.Minute)), services.WithCollectorWorkBuilder(builder))
		DeferCleanup(func() { runtime.Stop(context.Background()) })
		var err error
		lifecycle, err = upgrade.NewService(folder, runtime, 100*time.Millisecond)
		Expect(err).NotTo(HaveOccurred())
		_, err = runtime.StartCollecting(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Eventually(builder.finalizing).Should(BeClosed())
		Expect(lifecycle.Transition(request)).To(Succeed())
		Eventually(func() string { return lifecycle.Status().State }).Should(Equal("failed"))
		Expect(lifecycle.Status().ServicesClosed).To(BeFalse())
		Expect(runtime.UpgradeBusy()).To(BeTrue())
		Eventually(func() error {
			return lifecycle.Transition(upgrade.Request{Action: "resume", AttemptID: request.AttemptID})
		}).Should(Succeed())
		close(builder.finish)
		Eventually(runtime.UpgradeBusy).Should(BeFalse())
		Expect(runtime.GetCollectorStatus().State).To(Equal(models.CollectorStateCollected))
	})
	It("requires a fresh container after maintenance closes services even when the attempt is aborted", func() {
		Expect(lifecycle.Transition(request)).To(Succeed())
		Eventually(func() string { return lifecycle.Status().State }, 3*time.Second).Should(Equal("maintenance"))
		Expect(lifecycle.Transition(upgrade.Request{Action: "resume", AttemptID: request.AttemptID})).NotTo(Succeed())
		Expect(lifecycle.BeginRequest()).To(BeFalse())
		Expect(lifecycle.Status().ServicesClosed).To(BeTrue())
	})
	It("rejects corrupt collections and leaves their bytes intact", func() {
		path := filepath.Join(folder, "collection_200.duckdb")
		Expect(os.WriteFile(path, []byte("corrupt"), 0600)).To(Succeed())
		_, err := store.ReadUpgradeSnapshot(context.Background(), folder)
		Expect(err).To(HaveOccurred())
		data, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal("corrupt"))
	})
	It("reports closed services when baseline validation fails after draining", func() {
		Expect(os.WriteFile(filepath.Join(folder, "collection_200.duckdb"), []byte("corrupt"), 0600)).To(Succeed())
		Expect(lifecycle.Transition(request)).To(Succeed())
		Eventually(func() string { return lifecycle.Status().State }, 3*time.Second).Should(Equal("failed"))
		Expect(lifecycle.Status().ServicesClosed).To(BeTrue())
		Expect(lifecycle.BeginRequest()).To(BeFalse())
	})
	It("validates credential decryption without creating or repairing a key", func() {
		db, err := sql.Open("duckdb", filepath.Join(folder, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())
		encrypted, err := crypto.NewCrypto().Encrypt([]byte(strings.Repeat("k", 32)), models.Credentials{URL: "https://vcenter", Username: "saved-user", Password: "secret"})
		Expect(err).NotTo(HaveOccurred())
		_, err = db.Exec(`INSERT INTO credentials(id,url,username,password) VALUES ('credentials',?,?,?)`, encrypted.URL, encrypted.Username, encrypted.Password)
		Expect(err).NotTo(HaveOccurred())
		Expect(db.Close()).To(Succeed())
		_, err = store.ReadUpgradeSnapshot(context.Background(), folder)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(folder, "credentials.key"), []byte("invalid"), 0600)).To(Succeed())
		_, err = store.ReadUpgradeSnapshot(context.Background(), folder)
		Expect(err).To(MatchError(ContainSubstring("key is missing or invalid")))
		data, err := os.ReadFile(filepath.Join(folder, "credentials.key"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal("invalid"))
	})
	Context("offline saved-data validation", func() {
		BeforeEach(func() {
			Expect(lifecycle.Transition(request)).To(Succeed())
			Eventually(func() string { return lifecycle.Status().State }, 3*time.Second).Should(Equal("maintenance"))
			recovery, err := migrations.NewRecovery(folder)
			Expect(err).NotTo(HaveOccurred())
			_, err = recovery.Run(context.Background(), migrations.Request{Operation: "prepare", AttemptID: request.AttemptID, PreviousImage: oldImage, CandidateImage: newImage})
			Expect(err).NotTo(HaveOccurred())
		})
		It("validates either image against its own expected schemas without any persistent writes", func() {
			before := map[string][]byte{}
			Expect(filepath.WalkDir(folder, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					return nil
				}
				data, err := os.ReadFile(path)
				before[path] = data
				return err
			})).To(Succeed())
			for _, image := range []string{newImage, oldImage} {
				Expect(upgrade.Validate(context.Background(), folder, request.AttemptID, image)).To(Succeed())
			}
			after := map[string][]byte{}
			Expect(filepath.WalkDir(folder, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					return nil
				}
				data, err := os.ReadFile(path)
				after[path] = data
				return err
			})).To(Succeed())
			Expect(after).To(Equal(before))
		})
		It("rejects lost saved data even if migration histories still match", func() {
			db, err := sql.Open("duckdb", filepath.Join(folder, "collection_100.duckdb"))
			Expect(err).NotTo(HaveOccurred())
			_, err = db.Exec(`DELETE FROM inventory`)
			Expect(err).NotTo(HaveOccurred())
			Expect(db.Close()).To(Succeed())
			Expect(upgrade.Validate(context.Background(), folder, request.AttemptID, newImage)).To(MatchError(ContainSubstring("saved records differ")))
		})
		It("rejects application data that migration verification accepts", func() {
			recovery, err := migrations.NewRecovery(folder)
			Expect(err).NotTo(HaveOccurred())
			_, err = recovery.Run(context.Background(), migrations.Request{Operation: "apply", AttemptID: request.AttemptID})
			Expect(err).NotTo(HaveOccurred())
			db, err := sql.Open("duckdb", filepath.Join(folder, "collection_100.duckdb"))
			Expect(err).NotTo(HaveOccurred())
			_, err = db.Exec(`UPDATE inventory SET data = 'invalid JSON'`)
			Expect(err).NotTo(HaveOccurred())
			Expect(db.Close()).To(Succeed())
			_, err = recovery.Run(context.Background(), migrations.Request{Operation: "verify", AttemptID: request.AttemptID})
			Expect(err).NotTo(HaveOccurred())
			Expect(upgrade.Validate(context.Background(), folder, request.AttemptID, newImage)).To(MatchError(ContainSubstring("inventory JSON is invalid")))
		})
		It("rejects changed retained assets", func() {
			Expect(os.WriteFile(filepath.Join(folder, "credentials.key"), []byte(strings.Repeat("x", 32)), 0600)).To(Succeed())
			Expect(upgrade.Validate(context.Background(), folder, request.AttemptID, newImage)).To(MatchError(ContainSubstring("retained assets differ")))
		})
		It("rejects other images and attempts", func() {
			Expect(upgrade.Validate(context.Background(), folder, request.AttemptID, "sha256:"+strings.Repeat("c", 64))).NotTo(Succeed())
			Expect(upgrade.Validate(context.Background(), folder, "another", newImage)).NotTo(Succeed())
			Expect(upgrade.Validate(context.Background(), folder, "../unsafe", newImage)).NotTo(Succeed())
		})
		It("honors cancellation without changing existing data", func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			Expect(upgrade.Validate(ctx, folder, request.AttemptID, newImage)).To(MatchError(context.Canceled))
		})
	})
})
