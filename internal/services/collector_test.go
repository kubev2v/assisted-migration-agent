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
	"github.com/kubev2v/assisted-migration-agent/pkg/collector"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
	libmodel "github.com/kubev2v/forklift/pkg/lib/inventory/model"
)

type mockCollector struct {
	verifyErr  error
	collectErr error
}

func (m *mockCollector) VerifyCredentials(ctx context.Context, creds *models.Credentials) error {
	return m.verifyErr
}

func (m *mockCollector) Collect(ctx context.Context, creds *models.Credentials) error {
	return m.collectErr
}

func (m *mockCollector) DB() libmodel.DB {
	return nil
}

func (m *mockCollector) Close() {}

type mockProcessor struct {
	store      *store.Store
	processErr error
}

func (m *mockProcessor) Process(ctx context.Context, c collector.Collector) error {
	if m.processErr != nil {
		return m.processErr
	}
	return m.store.Inventory().Save(ctx, []byte(`{"vms":[]}`))
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
		srv = services.NewCollectorService(sched, st, &mockCollector{}, &mockProcessor{store: st})
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
		It("should create a service with ready state", func() {
			status := srv.GetStatus(ctx)
			Expect(status.State).To(Equal(models.CollectorStateReady))
		})
	})

	Describe("GetStatus", func() {
		It("should return ready state initially", func() {
			status := srv.GetStatus(ctx)
			Expect(status.State).To(Equal(models.CollectorStateReady))
		})
	})

	Describe("Stop", func() {
		It("should reset state to ready", func() {
			err := srv.Stop(ctx)
			Expect(err).NotTo(HaveOccurred())

			status := srv.GetStatus(ctx)
			Expect(status.State).To(Equal(models.CollectorStateReady))
		})
	})

	Describe("Start", func() {
		It("should verify credentials and start collection", func() {
			creds := &models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}

			err := srv.Start(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.CollectorState {
				return srv.GetStatus(ctx).State
			}).Should(Equal(models.CollectorStateCollected))

			inv, err := st.Inventory().Get(context.TODO())
			Expect(err).NotTo(HaveOccurred())
			Expect(inv).ToNot(BeNil())
		})

		It("should return error when credentials verification fails", func() {
			mockColl := &mockCollector{verifyErr: errors.New("connection refused")}
			srv = services.NewCollectorService(sched, st, mockColl, &mockProcessor{store: st})

			creds := &models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}

			err := srv.Start(ctx, creds)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() models.CollectorState {
				return srv.GetStatus(ctx).State
			}).Should(Equal(models.CollectorStateError))
		})

		It("should set error state when collection fails", func() {
			mockColl := &mockCollector{collectErr: errors.New("collection failed")}
			srv = services.NewCollectorService(sched, st, mockColl, &mockProcessor{store: st})

			creds := &models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}

			err := srv.Start(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.CollectorState {
				return srv.GetStatus(ctx).State
			}).Should(Equal(models.CollectorStateError))

			status := srv.GetStatus(ctx)
			Expect(status.Error).To(ContainSubstring("collection failed"))
		})

		It("should set error state when processor fails", func() {
			mockProc := &mockProcessor{store: st, processErr: errors.New("processing failed")}
			srv = services.NewCollectorService(sched, st, &mockCollector{}, mockProc)

			creds := &models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}

			err := srv.Start(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.CollectorState {
				return srv.GetStatus(ctx).State
			}).Should(Equal(models.CollectorStateError))

			status := srv.GetStatus(ctx)
			Expect(status.Error).To(ContainSubstring("processing failed"))
		})

		It("should return error when collection already in progress", func() {
			creds := &models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}

			err := srv.Start(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			err = srv.Start(ctx, creds)
			Expect(err).To(HaveOccurred())
		})
	})
})
