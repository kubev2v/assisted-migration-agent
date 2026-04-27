package services_test

import (
	"context"
	"database/sql"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

func mockCollectorBuilder(st *store.Store, eventSrv *services.EventService, connectErr, collectErr, processErr error) func(models.Credentials) models.WorkBuilder[models.CollectorStatus, models.CollectorResult] {
	return func(_ models.Credentials) models.WorkBuilder[models.CollectorStatus, models.CollectorResult] {
		return models.NewSliceWorkBuilder([]models.WorkUnit[models.CollectorStatus, models.CollectorResult]{
			{
				Status: func() models.CollectorStatus {
					return models.CollectorStatus{State: models.CollectorStateConnecting}
				},
				Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
					if connectErr != nil {
						return r, connectErr
					}
					return r, nil
				},
			},
			{
				Status: func() models.CollectorStatus {
					return models.CollectorStatus{State: models.CollectorStateCollecting}
				},
				Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
					if collectErr != nil {
						return r, collectErr
					}
					return r, nil
				},
			},
			{
				Status: func() models.CollectorStatus {
					return models.CollectorStatus{State: models.CollectorStateParsing}
				},
				Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
					if processErr != nil {
						return r, processErr
					}
					r.Inventory = []byte(`{"vms":[]}`)
					return r, st.Inventory().Save(ctx, r.Inventory)
				},
			},
			{
				Status: func() models.CollectorStatus {
					return models.CollectorStatus{State: models.CollectorStateCollected}
				},
				Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
					if err := eventSrv.AddInventoryUpdateEvent(ctx, r.Inventory); err != nil {
						return r, err
					}
					return r, nil
				},
			},
		})
	}
}

func blockingCollectorBuilder(gate chan struct{}) func(models.Credentials) models.WorkBuilder[models.CollectorStatus, models.CollectorResult] {
	return func(_ models.Credentials) models.WorkBuilder[models.CollectorStatus, models.CollectorResult] {
		return models.NewSliceWorkBuilder([]models.WorkUnit[models.CollectorStatus, models.CollectorResult]{
			{
				Status: func() models.CollectorStatus {
					return models.CollectorStatus{State: models.CollectorStateConnecting}
				},
				Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
					select {
					case <-gate:
						return r, nil
					case <-ctx.Done():
						return r, ctx.Err()
					}
				},
			},
		})
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
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())
		invSrv = services.NewInventoryService(st)
		eventSrv = services.NewEventService(st)
		srv = services.NewCollectorService(st, invSrv, eventSrv, "", "").
			WithWorkBuilder(mockCollectorBuilder(st, eventSrv, nil, nil, nil))
	})

	AfterEach(func() {
		if srv != nil {
			srv.Stop()
		}
		if db != nil {
			_ = db.Close()
		}
	})

	Context("NewCollectorService", func() {
		// Given a freshly created collector service
		// When we check its status
		// Then it should be in ready state
		It("should create a service with ready state", func() {
			// Arrange & Act
			status := srv.GetStatus()

			// Assert
			Expect(status.State).To(Equal(models.CollectorStateReady))
		})
	})

	Context("GetStatus", func() {
		// Given a collector service that has not been started
		// When GetStatus is called
		// Then it should return ready state
		It("should return ready state initially", func() {
			// Arrange & Act
			status := srv.GetStatus()

			// Assert
			Expect(status.State).To(Equal(models.CollectorStateReady))
		})
	})

	Context("Stop", func() {
		// Given a collector service that has not been started
		// When Stop is called
		// Then the state should remain ready
		It("should reset state to ready", func() {
			// Act
			srv.Stop()

			// Assert
			status := srv.GetStatus()
			Expect(status.State).To(Equal(models.CollectorStateReady))
		})
	})

	Context("Start", func() {
		// Given a collector service with mock work units that succeed
		// When Start is called with valid credentials
		// Then the pipeline should complete and state should be collected
		It("should verify credentials and start collection", func() {
			// Arrange
			creds := models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}

			// Act
			err := srv.Start(ctx, creds)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.CollectorStateType {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateCollected))

			inv, err := st.Inventory().Get(context.TODO())
			Expect(err).NotTo(HaveOccurred())
			Expect(inv).ToNot(BeNil())
		})

		// Given a collector service with mock work units that succeed
		// When Start is called and collection completes
		// Then an inventory update event should be written to the outbox
		It("should write an inventory update event to the outbox on successful collection", func() {
			// Arrange
			creds := models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}

			// Act
			err := srv.Start(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			// Assert
			Eventually(func() []models.Event {
				events, _ := eventSrv.Events(ctx)
				return events
			}).Should(HaveLen(1))

			events, err := eventSrv.Events(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(events[0].Kind).To(Equal(models.InventoryUpdateEvent))
			Expect(events[0].Data).To(MatchJSON(`{"vms":[]}`))
		})

		// Given a collector service where the connect step fails
		// When Start is called
		// Then the state should transition to error with the connect error message
		It("should set error state when connection fails", func() {
			// Arrange
			srv = services.NewCollectorService(st, invSrv, eventSrv, "", "").
				WithWorkBuilder(mockCollectorBuilder(st, eventSrv, errors.New("connection failed"), nil, nil))
			creds := models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}

			// Act
			err := srv.Start(ctx, creds)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.CollectorStateType {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateError))

			status := srv.GetStatus()
			Expect(status.Error.Error()).To(ContainSubstring("connection failed"))
		})

		// Given a collector service where the collect step fails
		// When Start is called
		// Then the state should transition to error with the collection error message
		It("should set error state when collection fails", func() {
			// Arrange
			srv = services.NewCollectorService(st, invSrv, eventSrv, "", "").
				WithWorkBuilder(mockCollectorBuilder(st, eventSrv, nil, errors.New("collection failed"), nil))
			creds := models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}

			// Act
			err := srv.Start(ctx, creds)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.CollectorStateType {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateError))

			status := srv.GetStatus()
			Expect(status.Error.Error()).To(ContainSubstring("collection failed"))
		})

		// Given a collector service where the process step fails
		// When Start is called
		// Then the state should transition to error with the processing error message
		It("should set error state when processor fails", func() {
			// Arrange
			srv = services.NewCollectorService(st, invSrv, eventSrv, "", "").
				WithWorkBuilder(mockCollectorBuilder(st, eventSrv, nil, nil, errors.New("processing failed")))
			creds := models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}

			// Act
			err := srv.Start(ctx, creds)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.CollectorStateType {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateError))

			status := srv.GetStatus()
			Expect(status.Error.Error()).To(ContainSubstring("processing failed"))
		})

		// Given a collector service with a blocking work unit that is already running
		// When Start is called a second time
		// Then it should return a collection-in-progress error
		It("should return error when collection already in progress", func() {
			// Arrange
			gate := make(chan struct{})
			defer close(gate)

			srv = services.NewCollectorService(st, invSrv, eventSrv, "", "").
				WithWorkBuilder(blockingCollectorBuilder(gate))
			creds := models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}
			Expect(srv.Start(ctx, creds)).To(Succeed())

			// Act
			err := srv.Start(ctx, creds)

			// Assert
			Expect(err).To(HaveOccurred())
		})

		// Given a collector service that has already collected successfully
		// When Start is called again
		// Then it should be a no-op and remain in collected state
		It("should be a no-op when already in collected state", func() {
			// Arrange
			creds := models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}
			err := srv.Start(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.CollectorStateType {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateCollected))

			// Act
			err = srv.Start(ctx, creds)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(srv.GetStatus().State).To(Equal(models.CollectorStateCollected))
		})
	})

	Context("NewCollectorService with existing inventory", func() {
		// Given a store that already has inventory data
		// When a new CollectorService is created
		// Then it should start in collected state
		It("should start in collected state when inventory exists", func() {
			// Arrange
			err := st.Inventory().Save(ctx, []byte(`{"vms":[]}`))
			Expect(err).NotTo(HaveOccurred())

			// Act
			collectorSrv := services.NewCollectorService(st, invSrv, eventSrv, "", "")

			// Assert
			Expect(collectorSrv.GetStatus().State).To(Equal(models.CollectorStateCollected))
		})
	})

	Context("Stop cancellation", func() {
		// Given a collector service with a blocking work unit that is running
		// When Stop is called
		// Then the state should return to ready or collected
		It("should cancel running collection and return to ready", func() {
			// Arrange
			gate := make(chan struct{})
			srv = services.NewCollectorService(st, invSrv, eventSrv, "", "").
				WithWorkBuilder(blockingCollectorBuilder(gate))
			creds := models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}
			err := srv.Start(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			// Act
			srv.Stop()

			// Assert
			state := srv.GetStatus().State
			Expect(state).To(BeElementOf(models.CollectorStateReady, models.CollectorStateCollected))
		})

		// Given a collector service that has not been started
		// When Stop is called
		// Then it should not panic and state should remain ready
		It("should be safe to call Stop when not running", func() {
			// Act
			srv.Stop()

			// Assert
			Expect(srv.GetStatus().State).To(Equal(models.CollectorStateReady))
		})
	})
})
