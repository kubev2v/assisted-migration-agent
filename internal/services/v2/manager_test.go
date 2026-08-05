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

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	v2 "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
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

	Describe("GetCollectorStatus", func() {
		var (
			ctx context.Context
			mgr *v2.ServiceManager
		)

		BeforeEach(func() {
			ctx = context.Background()
		})

		It("returns ready when no collector and no collection exists", func() {
			mgr = v2.NewServiceManager(
				v2.WithConfig(cfg),
				v2.WithPool(pool),
				v2.WithKeyManager(keyMgr),
			)
			Expect(mgr.Initialize()).To(Succeed())
			defer mgr.Stop(ctx)

			status := mgr.GetCollectorStatus()
			Expect(status.State).To(Equal(models.CollectorStateReady))
		})

		It("returns collected when no collector but persisted inventory exists", func() {
			collPath := filepath.Join(tmpDir, "collection_status.duckdb")
			collDB, err := pool.NewDatabase("coll-status", collPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			Expect(collDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
				s, err := collDB.Store()
				if err != nil {
					return err
				}
				parser := duckdb_parser.New(s.Querier(), nil)
				if err := parser.Init(); err != nil {
					return err
				}
				return migrations.RunCollection(ctx, db, "collection_status")
			})).To(Succeed())
			pool.Add(collDB)

			st, err := collDB.Store()
			Expect(err).NotTo(HaveOccurred())
			Expect(st.Inventory().Save(ctx, []byte(`{"vms":1}`))).To(Succeed())

			mgr = v2.NewServiceManager(
				v2.WithConfig(cfg),
				v2.WithPool(pool),
				v2.WithKeyManager(keyMgr),
			)
			Expect(mgr.Initialize()).To(Succeed())
			defer mgr.Stop(ctx)

			status := mgr.GetCollectorStatus()
			Expect(status.State).To(Equal(models.CollectorStateCollected))
		})

		It("returns ready when collection exists but has no inventory", func() {
			collPath := filepath.Join(tmpDir, "collection_empty.duckdb")
			collDB, err := pool.NewDatabase("coll-empty", collPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			Expect(collDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
				s, err := collDB.Store()
				if err != nil {
					return err
				}
				parser := duckdb_parser.New(s.Querier(), nil)
				if err := parser.Init(); err != nil {
					return err
				}
				return migrations.RunCollection(ctx, db, "collection_empty")
			})).To(Succeed())
			pool.Add(collDB)

			mgr = v2.NewServiceManager(
				v2.WithConfig(cfg),
				v2.WithPool(pool),
				v2.WithKeyManager(keyMgr),
			)
			Expect(mgr.Initialize()).To(Succeed())
			defer mgr.Stop(ctx)

			status := mgr.GetCollectorStatus()
			Expect(status.State).To(Equal(models.CollectorStateReady))
		})
	})

	Describe("mutual exclusion", func() {
		var (
			ctx context.Context
			mgr *v2.ServiceManager
			st  *store.Store2
		)

		BeforeEach(func() {
			ctx = context.Background()

			// Create a collection DB so pool.Latest() works for InspectorService().
			collPath := filepath.Join(tmpDir, "collection.duckdb")
			collDB, err := pool.NewDatabase("collection", collPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			Expect(collDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
				s, err := collDB.Store()
				if err != nil {
					return err
				}
				parser := duckdb_parser.New(s.Querier(), nil)
				if err := parser.Init(); err != nil {
					return err
				}
				return migrations.RunCollection(ctx, db, "collection")
			})).To(Succeed())
			pool.Add(collDB)

			st, err = collDB.Store()
			Expect(err).NotTo(HaveOccurred())

			// Insert VMs so the inspector has something to inspect.
			for _, vm := range []struct{ id, name string }{
				{"vm-1", "test-vm-1"},
				{"vm-2", "test-vm-2"},
			} {
				_, err := st.Querier().ExecContext(ctx, `
					INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory")
					VALUES (?, ?, 'poweredOn', 'cluster-a', 4096)
				`, vm.id, vm.name)
				Expect(err).NotTo(HaveOccurred())
			}

			// Save credentials pointing to vcsim.
			mainDB, err := pool.Get(store.MainDatabaseID)
			Expect(err).NotTo(HaveOccurred())
			mainSt, err := mainDB.Store()
			Expect(err).NotTo(HaveOccurred())

			credsSvc := v2.NewCredentialsService(mainSt)
			credsSvc.WithKeyManager(keyMgr)
			Expect(credsSvc.Save(ctx, keyMgr.Key(), "credentials", models.Credentials{
				URL:      "https://localhost:8989/sdk",
				Username: "user",
				Password: "pass",
				SkipTLS:  true,
			})).To(Succeed())

			mgr = v2.NewServiceManager(
				v2.WithConfig(cfg),
				v2.WithPool(pool),
				v2.WithKeyManager(keyMgr),
			)
			Expect(mgr.Initialize()).To(Succeed())
		})

		AfterEach(func() {
			mgr.Stop(ctx)
		})

		It("allows CreateCollector when no inspector is running", func() {
			collector, err := mgr.CreateCollector()
			Expect(err).NotTo(HaveOccurred())
			Expect(collector).NotTo(BeNil())
		})

		It("allows InspectorService when no collectors exist", func() {
			inspector, err := mgr.InspectorService()
			Expect(err).NotTo(HaveOccurred())
			Expect(inspector).NotTo(BeNil())
		})

		It("allows InspectorService when collector is created but not started", func() {
			_, err := mgr.CreateCollector()
			Expect(err).NotTo(HaveOccurred())

			inspector, err := mgr.InspectorService()
			Expect(err).NotTo(HaveOccurred())
			Expect(inspector).NotTo(BeNil())
		})

		It("rejects InspectorService when a collector is running", func() {
			gate := make(chan struct{})
			defer close(gate)

			collector, err := mgr.CreateCollector()
			Expect(err).NotTo(HaveOccurred())
			collector.WithWorkBuilder(blockingCollectorBuilder(gate))
			Expect(collector.Start(ctx)).To(Succeed())

			Eventually(func() bool {
				return collector.GetStatus().State.IsRunning()
			}).Should(BeTrue())

			_, err = mgr.InspectorService()
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsOperationInProgressError(err)).To(BeTrue())
		})

		It("rejects CreateCollector when inspector is busy", func() {
			inspector, err := mgr.InspectorService()
			Expect(err).NotTo(HaveOccurred())

			builder := newMockInspectionBuilder().withStore(st).withWorkDelay(5 * time.Second)
			inspector.WithInspectionBuilder(builder.builder())
			Expect(inspector.Start(ctx, []string{"vm-1"})).To(Succeed())

			Eventually(func() bool { return inspector.IsBusy() }).Should(BeTrue())

			_, err = mgr.CreateCollector()
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsOperationInProgressError(err)).To(BeTrue())
		})

		It("allows InspectorService after running collector is stopped", func() {
			gate := make(chan struct{})
			defer close(gate)

			collector, err := mgr.CreateCollector()
			Expect(err).NotTo(HaveOccurred())
			collector.WithWorkBuilder(blockingCollectorBuilder(gate))
			Expect(collector.Start(ctx)).To(Succeed())

			Eventually(func() bool {
				return collector.GetStatus().State.IsRunning()
			}).Should(BeTrue())

			Expect(mgr.StopCollector()).To(Succeed())

			inspector, err := mgr.InspectorService()
			Expect(err).NotTo(HaveOccurred())
			Expect(inspector).NotTo(BeNil())
		})

		It("allows CreateCollector after inspector finishes", func() {
			inspector, err := mgr.InspectorService()
			Expect(err).NotTo(HaveOccurred())

			builder := newMockInspectionBuilder().withStore(st)
			inspector.WithInspectionBuilder(builder.builder())
			Expect(inspector.Start(ctx, []string{"vm-1"})).To(Succeed())

			Eventually(func() bool { return inspector.IsBusy() }, 10*time.Second).Should(BeFalse())

			collector, err := mgr.CreateCollector()
			Expect(err).NotTo(HaveOccurred())
			Expect(collector).NotTo(BeNil())
		})

		It("returns same inspector instance while busy", func() {
			inspector1, err := mgr.InspectorService()
			Expect(err).NotTo(HaveOccurred())

			builder := newMockInspectionBuilder().withStore(st).withWorkDelay(5 * time.Second)
			inspector1.WithInspectionBuilder(builder.builder())
			Expect(inspector1.Start(ctx, []string{"vm-1"})).To(Succeed())

			Eventually(func() bool { return inspector1.IsBusy() }).Should(BeTrue())

			inspector2, err := mgr.InspectorService()
			Expect(err).NotTo(HaveOccurred())
			Expect(inspector2).To(BeIdenticalTo(inspector1))
		})

		It("recreates inspector instance when idle", func() {
			inspector1, err := mgr.InspectorService()
			Expect(err).NotTo(HaveOccurred())

			inspector2, err := mgr.InspectorService()
			Expect(err).NotTo(HaveOccurred())
			Expect(inspector2).NotTo(BeIdenticalTo(inspector1))
		})

		It("succeeds when stopping with no collector", func() {
			Expect(mgr.StopCollector()).To(Succeed())
		})

		It("allows InspectorService after Stop clears running collectors", func() {
			gate := make(chan struct{})
			defer close(gate)

			collector, err := mgr.CreateCollector()
			Expect(err).NotTo(HaveOccurred())
			collector.WithWorkBuilder(blockingCollectorBuilder(gate))
			Expect(collector.Start(ctx)).To(Succeed())

			Eventually(func() bool {
				return collector.GetStatus().State.IsRunning()
			}).Should(BeTrue())

			mgr.Stop(ctx)

			inspector, err := mgr.InspectorService()
			Expect(err).NotTo(HaveOccurred())
			Expect(inspector).NotTo(BeNil())
		})
	})
})
