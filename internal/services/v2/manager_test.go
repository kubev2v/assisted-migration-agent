package v2_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	v2 "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
)

var _ = Describe("ServiceManager", func() {
	var (
		pool   *store.Pool
		cfg    *config.Configuration
		keyMgr *crypto.KeyManager
		tmpDir string
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "manager-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)
		dbPath := filepath.Join(tmpDir, "agent.duckdb")
		mainDB, err := pool.NewDatabase(store.MainDatabaseID, dbPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(context.Background(), func(ctx context.Context, db *sql.DB) error {
			return migrations.RunMain(ctx, db)
		})).To(Succeed())
		pool.Add(mainDB)

		keyMgr, err = crypto.NewKeyManager("")
		Expect(err).NotTo(HaveOccurred())

		cfg = config.NewConfigurationWithOptionsAndDefaults(
			config.WithAgent(config.Agent{
				ID:       uuid.New().String(),
				SourceID: uuid.New().String(),
				Mode:     "disconnected",
			}),
		)
	})

	AfterEach(func() {
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	Describe("NewServiceManager", func() {
		It("creates a service manager with all options", func() {
			mgr := v2.NewServiceManager(
				v2.WithConfig(cfg),
				v2.WithPool(pool),
				v2.WithKeyManager(keyMgr),
			)
			Expect(mgr).NotTo(BeNil())
		})
	})

	Describe("Initialize", func() {
		It("fails when config is nil", func() {
			mgr := v2.NewServiceManager(
				v2.WithPool(pool),
				v2.WithKeyManager(keyMgr),
			)
			err := mgr.Initialize()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("config is required"))
		})

		It("fails when pool is nil", func() {
			mgr := v2.NewServiceManager(
				v2.WithConfig(cfg),
				v2.WithKeyManager(keyMgr),
			)
			err := mgr.Initialize()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("pool is required"))
		})

		It("fails when key manager is nil", func() {
			mgr := v2.NewServiceManager(
				v2.WithConfig(cfg),
				v2.WithPool(pool),
			)
			err := mgr.Initialize()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("key manager is required"))
		})
	})

	Describe("Stop", func() {
		It("does not panic on uninitialized manager", func() {
			mgr := v2.NewServiceManager()
			Expect(func() { mgr.Stop(context.Background()) }).NotTo(Panic())
		})
	})
})
