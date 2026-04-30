package services_test

import (
	"context"
	"database/sql"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	rsig "github.com/kubev2v/assisted-migration-agent/pkg/rightsizing"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("RightsizingService", func() {
	var (
		ctx context.Context
		db  *sql.DB
		st  *store.Store
		svc *services.RightsizingService
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())
		Expect(st.Migrate(ctx)).To(Succeed())

		svc = services.NewRightsizingService(st)
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	// seedReport creates a report with one VM and one metric via the store.
	seedReport := func(vcenter string) string {
		r := models.RightSizingReport{
			VCenter:             vcenter,
			ClusterID:           "domain-c123",
			IntervalID:          7200,
			WindowStart:         time.Now().Add(-720 * time.Hour).UTC(),
			WindowEnd:           time.Now().UTC(),
			ExpectedSampleCount: 360,
		}
		id, err := st.RightSizing().CreateReport(ctx, r, 1, 1)
		Expect(err).NotTo(HaveOccurred())

		Expect(st.RightSizing().WriteBatch(ctx, id, []models.RightSizingMetric{
			{VMName: "vm-a", MOID: "vm-100", MetricKey: "cpu.usagemhz.average",
				SampleCount: 360, Average: 500, P95: 1200, P99: 1500, Max: 2000, Latest: 450},
		})).To(Succeed())
		return id
	}

	Describe("ListReports", func() {
		It("should return an empty slice when no reports exist", func() {
			reports, err := svc.ListReports(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(reports).To(BeEmpty())
		})

		It("should return report metadata without VM metrics", func() {
			id := seedReport("https://vcenter.example.com")

			reports, err := svc.ListReports(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(reports).To(HaveLen(1))
			Expect(reports[0].ID).To(Equal(id))
		})
	})

	Describe("GetReport", func() {
		It("should return the report with all VM metrics", func() {
			id := seedReport("https://vcenter.example.com")

			report, err := svc.GetReport(ctx, id)
			Expect(err).NotTo(HaveOccurred())
			Expect(report.ID).To(Equal(id))
			Expect(report.VMs).To(HaveLen(1))
			Expect(report.VMs[0].Metrics["cpu.usagemhz.average"].SampleCount).To(Equal(360))
		})

		It("should return a ResourceNotFoundError for unknown IDs", func() {
			_, err := svc.GetReport(ctx, "does-not-exist")
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})
	})

	Describe("TriggerCollection", func() {
		It("should create a report shell in DuckDB and return it immediately", func() {
			svc.WithWorkBuilder(func(reportID string, cfg rsig.Config, discoverVMs bool, st *store.Store, start, end time.Time) *services.RightsizingCollectionHandle {
				return &services.RightsizingCollectionHandle{
					Builder: work.NewSliceWorkBuilder([]work.WorkUnit[models.RightsizingCollectionStatus, models.RightsizingCollectionResult]{
						{
							Status: func() models.RightsizingCollectionStatus {
								return models.RightsizingCollectionStatus{State: models.RightsizingCollectionStateCompleted}
							},
							Work: func(ctx context.Context, result models.RightsizingCollectionResult) (models.RightsizingCollectionResult, error) {
								return result, nil
							},
						},
					}),
					LogoutFn: func() {},
				}
			})

			params := models.RightsizingParams{
				Credentials: models.Credentials{URL: "https://vc.example.com", Username: "admin", Password: "secret"},
				LookbackH:   720,
				IntervalID:  7200,
				BatchSize:   5,
			}
			report, err := svc.TriggerCollection(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(report.ID).NotTo(BeEmpty())
			Expect(report.VCenter).To(Equal("https://vc.example.com"))

			// Shell must be persisted in the DB.
			summaries, err := svc.ListReports(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(summaries).To(HaveLen(1))
			Expect(summaries[0].ID).To(Equal(report.ID))
		})

		It("should reject a second TriggerCollection while one is running", func() {
			blockCh := make(chan struct{})
			svc.WithWorkBuilder(func(reportID string, cfg rsig.Config, discoverVMs bool, st *store.Store, start, end time.Time) *services.RightsizingCollectionHandle {
				return &services.RightsizingCollectionHandle{
					Builder: work.NewSliceWorkBuilder([]work.WorkUnit[models.RightsizingCollectionStatus, models.RightsizingCollectionResult]{
						{
							Status: func() models.RightsizingCollectionStatus {
								return models.RightsizingCollectionStatus{State: models.RightsizingCollectionStateConnecting}
							},
							Work: func(ctx context.Context, result models.RightsizingCollectionResult) (models.RightsizingCollectionResult, error) {
								<-blockCh
								return result, nil
							},
						},
					}),
					LogoutFn: func() {},
				}
			})

			params := models.RightsizingParams{
				Credentials: models.Credentials{URL: "https://vc.example.com", Username: "admin", Password: "secret"},
			}
			_, err := svc.TriggerCollection(ctx, params)
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.TriggerCollection(ctx, params)
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsOperationInProgressError(err)).To(BeTrue())

			close(blockCh)
		})
	})
})
