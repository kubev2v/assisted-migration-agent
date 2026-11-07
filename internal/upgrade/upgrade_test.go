package upgrade_test

import (
	"context"
	"database/sql"
	"encoding/json"
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

	"github.com/kubev2v/assisted-migration-agent/internal/datalock"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/internal/upgrade"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
	"github.com/kubev2v/assisted-migration-agent/test"
)

func TestUpgrade(t *testing.T) { RegisterFailHandler(Fail); RunSpecs(t, "Host upgrade lifecycle") }

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
		lifecycle, err = upgrade.NewService(folder, upgrade.Options{}, runtime, time.Second)
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
		_, err := os.Stat(filepath.Join(folder, "updates/validation/attempt-1.json"))
		Expect(os.IsNotExist(err)).To(BeTrue())
		lifecycle.EndRequest()
		auth := upgrade.Authorization{ContractVersion: 1, AttemptID: request.AttemptID, Phase: "aborted", PreviousImage: oldImage, CandidateImage: newImage}
		data, err := json.Marshal(auth)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(folder, "updates/authorization.json"), data, 0600)).To(Succeed())
		Eventually(func() error {
			return lifecycle.Transition(upgrade.Request{Action: "resume", AttemptID: request.AttemptID})
		}, time.Second).Should(Succeed())
		Expect(lifecycle.Status().State).To(Equal("normal"))
		Expect(lifecycle.BeginRequest()).To(BeTrue())
		lifecycle.EndRequest()
		Consistently(lifecycle.Restart(), 50*time.Millisecond).ShouldNot(Receive())
		request.AttemptID = "retry-after-timeout"
		Expect(lifecycle.Transition(request)).To(Succeed())
		Eventually(func() string { return lifecycle.Status().State }, 3*time.Second).Should(Equal("maintenance"))
	})
	It("keeps schema changes excluded by the same application data lock", func() {
		lock, err := datalock.Acquire(folder)
		Expect(err).NotTo(HaveOccurred())
		defer func() { Expect(lock.Close()).To(Succeed()) }()
		recovery, err := migrations.NewRecovery(folder)
		Expect(err).NotTo(HaveOccurred())
		_, err = recovery.Run(context.Background(), migrations.Request{Operation: "prepare", AttemptID: "locked", PreviousImage: oldImage, CandidateImage: newImage})
		Expect(err).To(MatchError(ContainSubstring("busy")))
		other, err := datalock.Acquire(folder)
		Expect(err).To(HaveOccurred())
		Expect(other).To(BeNil())
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
	Context("prepared probation", func() {
		var opts upgrade.Options
		var auth upgrade.Authorization
		BeforeEach(func() {
			Expect(lifecycle.Transition(request)).To(Succeed())
			Eventually(func() string { return lifecycle.Status().State }, 3*time.Second).Should(Equal("maintenance"))
			recovery, err := migrations.NewRecovery(folder)
			Expect(err).NotTo(HaveOccurred())
			_, err = recovery.Run(context.Background(), migrations.Request{Operation: "prepare", AttemptID: request.AttemptID, PreviousImage: oldImage, CandidateImage: newImage})
			Expect(err).NotTo(HaveOccurred())
			auth = upgrade.Authorization{ContractVersion: 1, AttemptID: request.AttemptID, Phase: "validating", PreviousImage: oldImage, CandidateImage: newImage}
			data, err := json.Marshal(auth)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(folder, "updates/authorization.json"), data, 0600)).To(Succeed())
			opts = upgrade.Options{AttemptID: request.AttemptID, ImageID: newImage}
		})
		It("gates normal startup and requires durable commit before release", func() {
			Expect(upgrade.Authorize(folder, upgrade.Options{ImageID: newImage}, false)).NotTo(Succeed())
			probation, err := upgrade.NewService(folder, opts, nil, time.Second)
			Expect(err).NotTo(HaveOccurred())
			defer probation.Close()
			Expect(probation.ValidateProbation(context.Background())).To(Succeed())
			Expect(probation.BeginRequest()).To(BeFalse())
			release := upgrade.Request{Action: "release", AttemptID: request.AttemptID}
			Expect(probation.Transition(release)).NotTo(Succeed())
			auth.Phase = "committed"
			data, err := json.Marshal(auth)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(folder, "updates/authorization.json"), data, 0600)).To(Succeed())
			Expect(probation.Transition(release)).To(Succeed())
			Expect(probation.Transition(release)).To(Succeed())
			Eventually(probation.Restart()).Should(Receive(Equal(upgrade.Options{ImageID: newImage})))
			Expect(upgrade.Authorize(folder, upgrade.Options{ImageID: oldImage}, false)).NotTo(Succeed())
			Expect(upgrade.Authorize(folder, upgrade.Options{ImageID: newImage}, false)).To(Succeed())
			normal, err := upgrade.NewService(folder, upgrade.Options{ImageID: newImage}, nil, time.Second)
			Expect(err).NotTo(HaveOccurred())
			defer normal.Close()
			Expect(normal.Transition(release)).To(Succeed())
			Consistently(normal.Restart(), 50*time.Millisecond).ShouldNot(Receive())
		})
		It("validates the previous image in recovery probation before releasing old work", func() {
			auth.Phase = "restoring"
			data, err := json.Marshal(auth)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(folder, "updates/authorization.json"), data, 0600)).To(Succeed())
			oldOpts := upgrade.Options{AttemptID: request.AttemptID, ImageID: oldImage}
			old, err := upgrade.NewService(folder, oldOpts, nil, time.Second)
			Expect(err).NotTo(HaveOccurred())
			defer old.Close()
			Expect(old.ValidateProbation(context.Background())).To(Succeed())
			release := upgrade.Request{Action: "release", AttemptID: request.AttemptID}
			Expect(old.Transition(release)).NotTo(Succeed())
			auth.Phase = "rolled-back"
			data, err = json.Marshal(auth)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(folder, "updates/authorization.json"), data, 0600)).To(Succeed())
			Expect(old.Transition(release)).To(Succeed())
			Eventually(old.Restart()).Should(Receive(Equal(upgrade.Options{ImageID: oldImage})))
		})
		It("rejects lost saved records even when the store can still be opened", func() {
			db, err := sql.Open("duckdb", filepath.Join(folder, "collection_100.duckdb"))
			Expect(err).NotTo(HaveOccurred())
			_, err = db.Exec(`DELETE FROM inventory`)
			Expect(err).NotTo(HaveOccurred())
			Expect(db.Close()).To(Succeed())
			probation, err := upgrade.NewService(folder, opts, nil, time.Second)
			Expect(err).NotTo(HaveOccurred())
			defer probation.Close()
			Expect(probation.ValidateProbation(context.Background())).To(MatchError(ContainSubstring("saved records differ")))
		})
		It("rejects an asset change without releasing probation", func() {
			probation, err := upgrade.NewService(folder, opts, nil, time.Second)
			Expect(err).NotTo(HaveOccurred())
			defer probation.Close()
			Expect(os.WriteFile(filepath.Join(folder, "credentials.key"), []byte(strings.Repeat("x", 32)), 0600)).To(Succeed())
			Expect(probation.ValidateProbation(context.Background())).NotTo(Succeed())
			Expect(probation.Status().State).To(Equal("probation"))
		})
		It("rejects image/attempt mismatches and unsafe reboot phases", func() {
			Expect(upgrade.Authorize(folder, upgrade.Options{AttemptID: "other", ImageID: newImage}, false)).NotTo(Succeed())
			Expect(upgrade.Authorize(folder, upgrade.Options{AttemptID: request.AttemptID, ImageID: oldImage}, false)).NotTo(Succeed())
			for _, phase := range []string{"preparing", "prepared", "applying", "rolling-back", "recovery-failed"} {
				auth.Phase = phase
				data, err := json.Marshal(auth)
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(filepath.Join(folder, "updates/authorization.json"), data, 0600)).To(Succeed())
				Expect(upgrade.Authorize(folder, opts, false)).NotTo(Succeed())
				Expect(upgrade.Authorize(folder, upgrade.Options{}, false)).NotTo(Succeed())
			}
		})
	})
})
