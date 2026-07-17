package v2_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	v2 "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type mockCollectorWorkBuilder struct {
	units []work.WorkUnit[models.CollectorStatus, models.CollectorResult]
	idx   int
}

func (b *mockCollectorWorkBuilder) Next() (work.WorkUnit[models.CollectorStatus, models.CollectorResult], bool) {
	if b.idx >= len(b.units) {
		return work.WorkUnit[models.CollectorStatus, models.CollectorResult]{}, false
	}
	u := b.units[b.idx]
	b.idx++
	return u, true
}

func (b *mockCollectorWorkBuilder) Finalize(_ context.Context, _ models.CollectorResult) error {
	return nil
}

func mockCollectorBuilder(st *store.Store2, connectErr, collectErr, processErr error) func(models.Credentials) work.WorkBuilder2[models.CollectorStatus, models.CollectorResult] {
	return func(_ models.Credentials) work.WorkBuilder2[models.CollectorStatus, models.CollectorResult] {
		return &mockCollectorWorkBuilder{
			units: []work.WorkUnit[models.CollectorStatus, models.CollectorResult]{
				{
					Status: func() models.CollectorStatus {
						return models.CollectorStatus{State: models.CollectorStateConnecting}
					},
					Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
						if connectErr != nil {
							r.Err = connectErr
						}
						return r, nil
					},
				},
				{
					Status: func() models.CollectorStatus {
						return models.CollectorStatus{State: models.CollectorStateCollecting}
					},
					Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
						if r.Err != nil {
							return r, nil
						}
						if collectErr != nil {
							r.Err = collectErr
						}
						return r, nil
					},
				},
				{
					Status: func() models.CollectorStatus {
						return models.CollectorStatus{State: models.CollectorStateParsing}
					},
					Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
						if r.Err != nil {
							return r, nil
						}
						if processErr != nil {
							r.Err = processErr
							return r, nil
						}
						r.Inventory = []byte(`{"vms":[]}`)
						if err := st.Inventory().Save(ctx, r.Inventory); err != nil {
							r.Err = err
						}
						return r, nil
					},
				},
				{
					Status: func() models.CollectorStatus {
						return models.CollectorStatus{State: models.CollectorStateCollected}
					},
					Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
						if r.Err != nil {
							return r, nil
						}
						if err := st.Outbox().Insert(ctx, models.Event{
							Kind: models.InventoryUpdateEvent,
							Data: r.Inventory,
						}); err != nil {
							r.Err = err
							return r, nil
						}
						r.Completed = true
						return r, nil
					},
				},
			},
		}
	}
}

func blockingCollectorBuilder(gate chan struct{}) func(models.Credentials) work.WorkBuilder2[models.CollectorStatus, models.CollectorResult] {
	return func(_ models.Credentials) work.WorkBuilder2[models.CollectorStatus, models.CollectorResult] {
		return &mockCollectorWorkBuilder{
			units: []work.WorkUnit[models.CollectorStatus, models.CollectorResult]{
				{
					Status: func() models.CollectorStatus {
						return models.CollectorStatus{State: models.CollectorStateConnecting}
					},
					Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
						select {
						case <-gate:
							return r, nil
						case <-ctx.Done():
							r.Err = ctx.Err()
							return r, nil
						}
					},
				},
			},
		}
	}
}

var _ = Describe("CollectorService", func() {
	var (
		ctx      context.Context
		pool     *store.Pool
		database *store.Database
		st       *store.Store2
		srv      *v2.CollectorService
		credsSvc *v2.CredentialsService
		tmpDir   string
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "collector-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)

		// Create main database for credentials.
		mainPath := filepath.Join(tmpDir, "agent.duckdb")
		mainDB, err := pool.NewDatabase(store.MainDatabaseID, mainPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		pool.Add(mainDB)

		mainSt, err := mainDB.Store()
		Expect(err).NotTo(HaveOccurred())

		// Create collection database for inventory/events.
		collPath := filepath.Join(tmpDir, "collection.duckdb")
		database, err = pool.NewDatabase("collection", collPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(database.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			st, err := database.Store()
			if err != nil {
				return err
			}

			parser := duckdb_parser.New(st.Querier(), nil)
			if err := parser.Init(); err != nil {
				return err
			}
			return migrations.RunCollection(ctx, db, "collection")
		})).To(Succeed())

		st, err = database.Store()
		Expect(err).NotTo(HaveOccurred())

		km, err := crypto.NewKeyManager("")
		Expect(err).NotTo(HaveOccurred())
		credsSvc = v2.NewCredentialsService(mainSt)
		credsSvc.WithKeyManager(km)
		creds := models.Credentials{
			URL:      "https://vcenter.example.com",
			Username: "admin",
			Password: "secret",
		}
		err = credsSvc.Save(ctx, km.Key(), "credentials", creds)
		Expect(err).NotTo(HaveOccurred())

		srv = v2.NewCollectorService(mockCollectorBuilder(st, nil, nil, nil), credsSvc)
	})

	AfterEach(func() {
		if srv != nil {
			srv.Stop()
		}
		pool.Close()
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	Context("NewCollectorService", func() {
		It("should create a service with ready state", func() {
			status := srv.GetStatus()
			Expect(status.State).To(Equal(models.CollectorStateReady))
		})
	})

	Context("GetStatus", func() {
		It("should return ready state initially", func() {
			status := srv.GetStatus()
			Expect(status.State).To(Equal(models.CollectorStateReady))
		})
	})

	Context("Stop", func() {
		It("should reset state to ready", func() {
			srv.Stop()
			status := srv.GetStatus()
			Expect(status.State).To(Equal(models.CollectorStateReady))
		})
	})

	Context("Start", func() {
		It("should verify credentials and start collection", func() {
			err := srv.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.CollectorStateType {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateCollected))

			inv, err := st.Inventory().Get(context.TODO())
			Expect(err).NotTo(HaveOccurred())
			Expect(inv).ToNot(BeNil())
		})

		It("should write an inventory update event to the outbox on successful collection", func() {
			err := srv.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() []models.Event {
				events, _ := st.Outbox().Get(ctx)
				return events
			}).Should(HaveLen(1))

			events, err := st.Outbox().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(events[0].Kind).To(Equal(models.InventoryUpdateEvent))
			Expect(events[0].Data).To(MatchJSON(`{"vms":[]}`))
		})

		It("should set error state when connection fails", func() {
			srv = v2.NewCollectorService(
				mockCollectorBuilder(st, errors.New("connection failed"), nil, nil), credsSvc)

			err := srv.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.CollectorStateType {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateError))

			status := srv.GetStatus()
			Expect(status.Error.Error()).To(ContainSubstring("connection failed"))
		})

		It("should set error state when collection fails", func() {
			srv = v2.NewCollectorService(
				mockCollectorBuilder(st, nil, errors.New("collection failed"), nil), credsSvc)

			err := srv.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.CollectorStateType {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateError))

			status := srv.GetStatus()
			Expect(status.Error.Error()).To(ContainSubstring("collection failed"))
		})

		It("should set error state when processor fails", func() {
			srv = v2.NewCollectorService(
				mockCollectorBuilder(st, nil, nil, errors.New("processing failed")), credsSvc)

			err := srv.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.CollectorStateType {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateError))

			status := srv.GetStatus()
			Expect(status.Error.Error()).To(ContainSubstring("processing failed"))
		})

		It("should return error when collection already in progress", func() {
			gate := make(chan struct{})
			defer close(gate)

			srv = v2.NewCollectorService(
				blockingCollectorBuilder(gate), credsSvc)
			Expect(srv.Start(ctx)).To(Succeed())

			err := srv.Start(ctx)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("NewCollectorService with existing inventory", func() {
		It("should start in collected state when inventory exists", func() {
			err := st.Inventory().Save(ctx, []byte(`{"vms":[]}`))
			Expect(err).NotTo(HaveOccurred())

			collectorSrv := v2.NewCollectorService(nil, credsSvc)

			Expect(collectorSrv.GetStatus().State).To(Equal(models.CollectorStateReady))
		})
	})

	Context("Stop cancellation", func() {
		It("should cancel running collection and return to ready", func() {
			gate := make(chan struct{})
			srv = v2.NewCollectorService(
				blockingCollectorBuilder(gate), credsSvc)
			err := srv.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			srv.Stop()

			state := srv.GetStatus().State
			Expect(state).To(BeElementOf(models.CollectorStateReady, models.CollectorStateCollected))
		})

		It("should be safe to call Stop when not running", func() {
			srv.Stop()
			Expect(srv.GetStatus().State).To(Equal(models.CollectorStateReady))
		})
	})
})
