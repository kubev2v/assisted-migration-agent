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
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

type mockWorkBuilder struct {
	verifyErr  error
	collectErr error
	processErr error
	store      *store.Store
}

func (m *mockWorkBuilder) WithCredentials(creds *models.Credentials) models.WorkBuilder {
	return m
}

func (m *mockWorkBuilder) Build() []models.WorkUnit {
	return []models.WorkUnit{
		m.connecting(),
		m.collecting(),
		m.collected(),
	}
}

func (m *mockWorkBuilder) connecting() models.WorkUnit {
	return models.WorkUnit{
		Status: func() models.CollectorStatus {
			return models.CollectorStatus{State: models.CollectorStateConnecting}
		},
		Work: func() func(ctx context.Context) (any, error) {
			return func(ctx context.Context) (any, error) {
				if m.verifyErr != nil {
					return nil, m.verifyErr
				}
				return nil, nil
			}
		},
	}
}

func (m *mockWorkBuilder) collecting() models.WorkUnit {
	return models.WorkUnit{
		Status: func() models.CollectorStatus {
			return models.CollectorStatus{State: models.CollectorStateCollecting}
		},
		Work: func() func(ctx context.Context) (any, error) {
			return func(ctx context.Context) (any, error) {
				if m.collectErr != nil {
					return nil, m.collectErr
				}
				if m.processErr != nil {
					return nil, m.processErr
				}
				// Save mock inventory
				return nil, m.store.Inventory().Save(ctx, []byte(`{"vms":[]}`))
			}
		},
	}
}

func (m *mockWorkBuilder) collected() models.WorkUnit {
	return models.WorkUnit{
		Status: func() models.CollectorStatus {
			return models.CollectorStatus{State: models.CollectorStateCollected}
		},
		Work: func() func(ctx context.Context) (any, error) {
			return func(ctx context.Context) (any, error) { return nil, nil }
		},
	}
}

var _ = Describe("CollectorService", func() {
	var (
		ctx   context.Context
		db    *sql.DB
		st    *store.Store
		sched *scheduler.Scheduler
		srv   *services.CollectorService
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db)
		sched = scheduler.NewScheduler(1)
		srv = services.NewCollectorService(sched, st, &mockWorkBuilder{store: st})
	})

	AfterEach(func() {
		if sched != nil {
			sched.Close()
		}
		if db != nil {
			db.Close()
		}
	})

	Describe("NewCollectorService", func() {
		// Given a newly created collector service
		// When we check the status
		// Then the state should be ready
		It("should create a service with ready state", func() {
			// Act
			status := srv.GetStatus()

			// Assert
			Expect(status.State).To(Equal(models.CollectorStateReady))
		})
	})

	Describe("GetStatus", func() {
		// Given a collector service that has not been started
		// When we get the status
		// Then it should return ready state
		It("should return ready state initially", func() {
			// Act
			status := srv.GetStatus()

			// Assert
			Expect(status.State).To(Equal(models.CollectorStateReady))
		})
	})

	Describe("Stop", func() {
		// Given a collector service
		// When we stop the collector
		// Then the state should reset to ready
		It("should reset state to ready", func() {
			// Act
			srv.Stop()
			status := srv.GetStatus()

			// Assert
			Expect(status.State).To(Equal(models.CollectorStateReady))
		})
	})

	Describe("Start", func() {
		// Given a collector service with valid credentials
		// When we start the collector
		// Then it should reach collected state and inventory should be available
		It("should verify credentials and start collection", func() {
			// Arrange
			creds := &models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}

			// Act
			err := srv.Start(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			// Assert
			Eventually(func() models.CollectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateCollected))

			inv, err := st.Inventory().Get(context.TODO())
			Expect(err).NotTo(HaveOccurred())
			Expect(inv).ToNot(BeNil())
		})

		// Given a collector service with a work builder that fails verification
		// When we start the collector
		// Then it should reach error state
		It("should return error when credentials verification fails", func() {
			// Arrange
			srv = services.NewCollectorService(sched, st, &mockWorkBuilder{
				store:     st,
				verifyErr: errors.New("connection refused"),
			})
			creds := &models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}

			// Act
			err := srv.Start(ctx, creds)
			Expect(err).ToNot(HaveOccurred())

			// Assert
			Eventually(func() models.CollectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateError))
		})

		// Given a collector service with a work builder that fails collection
		// When we start the collector
		// Then it should reach error state with collection failed message
		It("should set error state when collection fails", func() {
			// Arrange
			srv = services.NewCollectorService(sched, st, &mockWorkBuilder{
				store:      st,
				collectErr: errors.New("collection failed"),
			})
			creds := &models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}

			// Act
			err := srv.Start(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			// Assert
			Eventually(func() models.CollectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateError))

			status := srv.GetStatus()
			Expect(status.Error.Error()).To(ContainSubstring("collection failed"))
		})

		// Given a collector service with a work builder that fails processing
		// When we start the collector
		// Then it should reach error state with processing failed message
		It("should set error state when processor fails", func() {
			// Arrange
			srv = services.NewCollectorService(sched, st, &mockWorkBuilder{
				store:      st,
				processErr: errors.New("processing failed"),
			})
			creds := &models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}

			// Act
			err := srv.Start(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			// Assert
			Eventually(func() models.CollectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.CollectorStateError))

			status := srv.GetStatus()
			Expect(status.Error.Error()).To(ContainSubstring("processing failed"))
		})

		// Given a collector service that is already running
		// When we try to start it again
		// Then it should return an error
		It("should return error when collection already in progress", func() {
			// Arrange
			creds := &models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}
			err := srv.Start(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = srv.Start(ctx, creds)

			// Assert
			Expect(err).To(HaveOccurred())
		})
	})
})
