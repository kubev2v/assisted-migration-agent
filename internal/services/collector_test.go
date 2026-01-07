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
			Expect(status.HasCredentials).To(BeFalse())
		})
	})

	Describe("GetStatus", func() {
		It("should return ready state initially", func() {
			status := srv.GetStatus(ctx)
			Expect(status.State).To(Equal(models.CollectorStateReady))
		})

		It("should report HasCredentials when credentials exist", func() {
			creds := &models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}
			err := st.Credentials().Save(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			status := srv.GetStatus(ctx)
			Expect(status.HasCredentials).To(BeTrue())
		})
	})

	Describe("GetCredentials", func() {
		It("should return error when no credentials exist", func() {
			_, err := srv.GetCredentials(ctx)
			Expect(err).To(HaveOccurred())
		})

		It("should return credentials when they exist", func() {
			creds := &models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}
			err := st.Credentials().Save(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			retrieved, err := srv.GetCredentials(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.URL).To(Equal(creds.URL))
		})
	})

	Describe("HasCredentials", func() {
		It("should return false when no credentials exist", func() {
			has, err := srv.HasCredentials(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(has).To(BeFalse())
		})

		It("should return true when credentials exist", func() {
			creds := &models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}
			err := st.Credentials().Save(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			has, err := srv.HasCredentials(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(has).To(BeTrue())
		})
	})

	Describe("GetInventory", func() {
		It("should return error when no inventory exists", func() {
			_, err := srv.GetInventory(ctx)
			Expect(err).To(HaveOccurred())
		})

		It("should return inventory when it exists", func() {
			data := []byte(`{"vms": []}`)
			err := st.Inventory().Save(ctx, data)
			Expect(err).NotTo(HaveOccurred())

			inv, err := srv.GetInventory(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(inv.Data).To(Equal(data))
		})
	})

	Describe("HasInventory", func() {
		It("should return false when no inventory exists", func() {
			has, err := srv.HasInventory(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(has).To(BeFalse())
		})

		It("should return true when inventory exists", func() {
			data := []byte(`{"vms": []}`)
			err := st.Inventory().Save(ctx, data)
			Expect(err).NotTo(HaveOccurred())

			has, err := srv.HasInventory(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(has).To(BeTrue())
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

			has, err := srv.HasInventory(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(has).To(BeTrue())
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
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("connection refused"))

			status := srv.GetStatus(ctx)
			Expect(status.State).To(Equal(models.CollectorStateError))
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

		It("should save credentials after successful verification", func() {
			creds := &models.Credentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}

			err := srv.Start(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			saved, err := srv.GetCredentials(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(saved.URL).To(Equal(creds.URL))
			Expect(saved.Username).To(Equal(creds.Username))
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
