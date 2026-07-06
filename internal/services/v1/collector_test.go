package v1_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
	"github.com/kubev2v/assisted-migration-agent/test"
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

func mockCollectorBuilder(st *store.Store, eventSrv *services.EventService, connectErr, collectErr, processErr error) func(models.Credentials) work.WorkBuilder2[models.CollectorStatus, models.CollectorResult] {
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
						if err := eventSrv.AddInventoryUpdateEvent(ctx, r.Inventory); err != nil {
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
		db       *sql.DB
		st       *store.Store
		srv      *services.CollectorService
		eventSrv *services.EventService
		invSrv   *services.InventoryService
		credsSvc *services.CredentialsService
		tmpDir   string
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "collector-test-*")
		Expect(err).NotTo(HaveOccurred())

		db, err = store.NewConnection(nil, filepath.Join(tmpDir, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())
		Expect(st.Migrate(ctx, "")).To(Succeed())
		Expect(st.InitCollection(ctx)).To(Succeed())
		invSrv = services.NewInventoryService(st)
		eventSrv = services.NewEventService(st)

		km, err := crypto.NewKeyManager("")
		Expect(err).NotTo(HaveOccurred())
		credsSvc = services.NewCredentialsService(st).WithKeyManager(km)
		creds := models.Credentials{
			URL:      "https://vcenter.example.com",
			Username: "admin",
			Password: "secret",
		}
		err = credsSvc.Save(ctx, km.Key(), "credentials", creds)
		Expect(err).NotTo(HaveOccurred())

		srv = services.NewCollectorService(invSrv, mockCollectorBuilder(st, eventSrv, nil, nil, nil), credsSvc)
	})

	AfterEach(func() {
		if srv != nil {
			srv.Stop()
		}
		if db != nil {
			_ = db.Close()
		}
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
				events, _ := eventSrv.Events(ctx)
				return events
			}).Should(HaveLen(1))

			events, err := eventSrv.Events(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(events[0].Kind).To(Equal(models.InventoryUpdateEvent))
			Expect(events[0].Data).To(MatchJSON(`{"vms":[]}`))
		})

		It("should set error state when connection fails", func() {
			srv = services.NewCollectorService(invSrv,
				mockCollectorBuilder(st, eventSrv, errors.New("connection failed"), nil, nil), credsSvc)

			err := srv.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.CollectorStateType {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateError))

			status := srv.GetStatus()
			Expect(status.Error.Error()).To(ContainSubstring("connection failed"))
		})

		It("should set error state when collection fails", func() {
			srv = services.NewCollectorService(invSrv,
				mockCollectorBuilder(st, eventSrv, nil, errors.New("collection failed"), nil), credsSvc)

			err := srv.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.CollectorStateType {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateError))

			status := srv.GetStatus()
			Expect(status.Error.Error()).To(ContainSubstring("collection failed"))
		})

		It("should set error state when processor fails", func() {
			srv = services.NewCollectorService(invSrv,
				mockCollectorBuilder(st, eventSrv, nil, nil, errors.New("processing failed")), credsSvc)

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

			srv = services.NewCollectorService(invSrv,
				blockingCollectorBuilder(gate), credsSvc)
			Expect(srv.Start(ctx)).To(Succeed())

			err := srv.Start(ctx)
			Expect(err).To(HaveOccurred())
		})

		It("should be a no-op when already in collected state", func() {
			err := srv.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.CollectorStateType {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateCollected))

			err = srv.Start(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(srv.GetStatus().State).To(Equal(models.CollectorStateCollected))
		})
	})

	Context("NewCollectorService with existing inventory", func() {
		It("should start in collected state when inventory exists", func() {
			err := st.Inventory().Save(ctx, []byte(`{"vms":[]}`))
			Expect(err).NotTo(HaveOccurred())

			collectorSrv := services.NewCollectorService(invSrv, nil, credsSvc)
			Expect(collectorSrv.GetStatus().State).To(Equal(models.CollectorStateCollected))
		})
	})

	Context("Stop cancellation", func() {
		It("should cancel running collection and return to ready", func() {
			gate := make(chan struct{})
			srv = services.NewCollectorService(invSrv,
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
