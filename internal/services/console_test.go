package services_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
)

var _ = Describe("Console Service", func() {
	var (
		pool   *store.Pool
		mgr    *services.ServiceManager
		st     *store.Store
		cfg    config.Agent
		tmpDir string
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "console-services.test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)
		dbPath := filepath.Join(tmpDir, "agent.duckdb")
		mainDB, err := pool.NewDatabase(store.MainDatabaseID, dbPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(context.Background(), func(ctx context.Context, db *sql.DB) error {
			return migrations.RunMain(ctx, db)
		})).To(Succeed())
		pool.Add(mainDB)

		st, err = mainDB.Store()
		Expect(err).NotTo(HaveOccurred())

		// mgr has no collector or collection DB registered; GetCollector/LatestEventService
		// resolve to "not found", which the console pipeline treats as "nothing to send".
		mgr = services.NewServiceManager(services.WithPool(pool))

		cfg = config.Agent{
			ID:             uuid.New().String(),
			SourceID:       uuid.New().String(),
			UpdateInterval: 50 * time.Millisecond,
		}
	})

	AfterEach(func() {
		pool.Close()
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	Context("SetMode", func() {
		// Given a console service in connected mode that has recorded a transient error
		// When we switch to disconnected mode
		// Then the stale error should be cleared from status
		It("should clear stale error when switching to disconnected mode", func() {
			// Arrange
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, client, mgr, st)
			Expect(err).NotTo(HaveOccurred())

			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Wait for the transient error to be recorded
			Eventually(func() error {
				return consoleSrv.Status().Error
			}, 500*time.Millisecond).ShouldNot(BeNil())

			// Act
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeDisconnected)).To(BeNil())

			// Assert
			Expect(consoleSrv.Status().Error).To(BeNil())
		})
	})
})
