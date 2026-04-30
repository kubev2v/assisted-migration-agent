package store_test

import (
	"context"
	"database/sql"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("RightSizingStore", func() {
	var (
		ctx context.Context
		db  *sql.DB
		s   *store.Store
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		s = store.NewStore(db, test.NewMockValidator())
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	testReport := func() models.RightSizingReport {
		return models.RightSizingReport{
			VCenter:             "https://vcenter.example.com/sdk",
			ClusterID:           "domain-c123",
			IntervalID:          7200,
			WindowStart:         time.Now().Add(-720 * time.Hour).UTC(),
			WindowEnd:           time.Now().UTC(),
			ExpectedSampleCount: 360,
		}
	}

	Describe("CreateReport", func() {
		It("should insert a report and return a non-empty UUID", func() {
			id, err := s.RightSizing().CreateReport(ctx, testReport(), 10, 5)
			Expect(err).NotTo(HaveOccurred())
			Expect(id).NotTo(BeEmpty())
		})

		It("should set expected_batch_count to ceil(vmCount/batchSize)", func() {
			// ceil(10/3) = 4
			id, err := s.RightSizing().CreateReport(ctx, testReport(), 10, 3)
			Expect(err).NotTo(HaveOccurred())

			var expectedBatches, writtenBatches int
			err = db.QueryRow(
				`SELECT expected_batch_count, written_batch_count FROM rightsizing_reports WHERE id = ?`, id,
			).Scan(&expectedBatches, &writtenBatches)
			Expect(err).NotTo(HaveOccurred())
			Expect(expectedBatches).To(Equal(4))
			Expect(writtenBatches).To(Equal(0))
		})

		It("should persist vcenter and cluster_id correctly", func() {
			r := testReport()
			id, err := s.RightSizing().CreateReport(ctx, r, 1, 1)
			Expect(err).NotTo(HaveOccurred())

			var vcenter, clusterID string
			err = db.QueryRow(
				`SELECT vcenter, cluster_id FROM rightsizing_reports WHERE id = ?`, id,
			).Scan(&vcenter, &clusterID)
			Expect(err).NotTo(HaveOccurred())
			Expect(vcenter).To(Equal(r.VCenter))
			Expect(clusterID).To(Equal(r.ClusterID))
		})

		It("should return an error when batchSize is zero", func() {
			_, err := s.RightSizing().CreateReport(ctx, testReport(), 10, 0)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("WriteBatch", func() {
		It("should insert metric rows for all metrics with non-zero SampleCount", func() {
			id, _ := s.RightSizing().CreateReport(ctx, testReport(), 1, 1)

			metrics := []models.RightSizingMetric{
				{VMName: "vm-a", MOID: "vm-100", MetricKey: "cpu.usagemhz.average", SampleCount: 360, Average: 500, P95: 1200, P99: 1500, Max: 2000, Latest: 450},
				{VMName: "vm-a", MOID: "vm-100", MetricKey: "mem.consumed.average", SampleCount: 360, Average: 2048, P95: 3000, P99: 3500, Max: 4096, Latest: 2100},
			}
			Expect(s.RightSizing().WriteBatch(ctx, id, metrics)).To(Succeed())

			var count int
			Expect(db.QueryRow(`SELECT COUNT(*) FROM rightsizing_metrics WHERE report_id = ?`, id).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(2))
		})

		It("should skip metrics with zero SampleCount", func() {
			id, _ := s.RightSizing().CreateReport(ctx, testReport(), 1, 1)

			metrics := []models.RightSizingMetric{
				{VMName: "vm-a", MOID: "vm-100", MetricKey: "cpu.usagemhz.average", SampleCount: 0},
			}
			Expect(s.RightSizing().WriteBatch(ctx, id, metrics)).To(Succeed())

			var count int
			Expect(db.QueryRow(`SELECT COUNT(*) FROM rightsizing_metrics WHERE report_id = ?`, id).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(0))
		})

		It("should return nil and insert nothing when given an empty slice", func() {
			id, _ := s.RightSizing().CreateReport(ctx, testReport(), 1, 1)
			Expect(s.RightSizing().WriteBatch(ctx, id, nil)).To(Succeed())

			var count int
			Expect(db.QueryRow(`SELECT COUNT(*) FROM rightsizing_metrics WHERE report_id = ?`, id).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(0))
		})

		It("should be idempotent on duplicate rows", func() {
			id, _ := s.RightSizing().CreateReport(ctx, testReport(), 1, 1)

			metrics := []models.RightSizingMetric{
				{VMName: "vm-a", MOID: "vm-100", MetricKey: "cpu.usagemhz.average", SampleCount: 10, Average: 100},
			}
			Expect(s.RightSizing().WriteBatch(ctx, id, metrics)).To(Succeed())
			Expect(s.RightSizing().WriteBatch(ctx, id, metrics)).To(Succeed()) // duplicate

			var count int
			Expect(db.QueryRow(`SELECT COUNT(*) FROM rightsizing_metrics WHERE report_id = ?`, id).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(1))
		})
	})

	Describe("IncrementWrittenBatchCount", func() {
		It("should increment written_batch_count by 1 per call", func() {
			id, _ := s.RightSizing().CreateReport(ctx, testReport(), 4, 2)

			Expect(s.RightSizing().IncrementWrittenBatchCount(ctx, id)).To(Succeed())
			Expect(s.RightSizing().IncrementWrittenBatchCount(ctx, id)).To(Succeed())

			var written int
			Expect(db.QueryRow(`SELECT written_batch_count FROM rightsizing_reports WHERE id = ?`, id).Scan(&written)).To(Succeed())
			Expect(written).To(Equal(2))
		})
	})

	Describe("WithTx atomicity", func() {
		It("should roll back both WriteBatch and IncrementWrittenBatchCount on error", func() {
			id, _ := s.RightSizing().CreateReport(ctx, testReport(), 1, 1)

			err := s.WithTx(ctx, func(txCtx context.Context) error {
				metrics := []models.RightSizingMetric{
					{VMName: "vm-a", MOID: "vm-100", MetricKey: "cpu.usagemhz.average", SampleCount: 10, Average: 100},
				}
				if err := s.RightSizing().WriteBatch(txCtx, id, metrics); err != nil {
					return err
				}
				if err := s.RightSizing().IncrementWrittenBatchCount(txCtx, id); err != nil {
					return err
				}
				return errors.New("simulated failure — rolls back everything")
			})
			Expect(err).To(HaveOccurred())

			var count int
			Expect(db.QueryRow(`SELECT COUNT(*) FROM rightsizing_metrics WHERE report_id = ?`, id).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(0))

			var written int
			Expect(db.QueryRow(`SELECT written_batch_count FROM rightsizing_reports WHERE id = ?`, id).Scan(&written)).To(Succeed())
			Expect(written).To(Equal(0))
		})
	})

	Describe("UpdateExpectedBatchCount", func() {
		It("should update expected_batch_count after VM discovery", func() {
			id, err := s.RightSizing().CreateReport(ctx, testReport(), 0, 1)
			Expect(err).NotTo(HaveOccurred())

			// ceil(10/3) = 4
			Expect(s.RightSizing().UpdateExpectedBatchCount(ctx, id, 10, 3)).To(Succeed())

			var expected int
			Expect(db.QueryRow(
				`SELECT expected_batch_count FROM rightsizing_reports WHERE id = ?`, id,
			).Scan(&expected)).To(Succeed())
			Expect(expected).To(Equal(4))
		})

		It("should return an error when batchSize is zero", func() {
			id, _ := s.RightSizing().CreateReport(ctx, testReport(), 0, 1)
			Expect(s.RightSizing().UpdateExpectedBatchCount(ctx, id, 10, 0)).To(HaveOccurred())
		})
	})

	// seedReport creates a report and writes one VM with two metrics.
	// Returns the report ID.
	seedReport := func(vcenter string) string {
		r := models.RightSizingReport{
			VCenter:             vcenter,
			ClusterID:           "domain-c123",
			IntervalID:          7200,
			WindowStart:         time.Now().Add(-720 * time.Hour).UTC(),
			WindowEnd:           time.Now().UTC(),
			ExpectedSampleCount: 360,
		}
		id, err := s.RightSizing().CreateReport(ctx, r, 1, 1)
		Expect(err).NotTo(HaveOccurred())

		metrics := []models.RightSizingMetric{
			{VMName: "vm-a", MOID: "vm-100", MetricKey: "cpu.usagemhz.average",
				SampleCount: 360, Average: 500, P95: 1200, P99: 1500, Max: 2000, Latest: 450},
			{VMName: "vm-a", MOID: "vm-100", MetricKey: "mem.consumed.average",
				SampleCount: 360, Average: 2048, P95: 3000, P99: 3500, Max: 4096, Latest: 2100},
		}
		Expect(s.RightSizing().WriteBatch(ctx, id, metrics)).To(Succeed())
		return id
	}

	Describe("ListReports", func() {
		It("should return an empty slice when no reports exist", func() {
			reports, err := s.RightSizing().ListReports(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(reports).NotTo(BeNil())
			Expect(reports).To(BeEmpty())
		})

		It("should return report metadata without VM metrics", func() {
			id := seedReport("https://vc1.example.com")

			reports, err := s.RightSizing().ListReports(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(reports).To(HaveLen(1))
			Expect(reports[0].ID).To(Equal(id))
			Expect(reports[0].VCenter).To(Equal("https://vc1.example.com"))
		})

		It("should return multiple reports with correct data for each", func() {
			id1 := seedReport("https://vc1.example.com")
			id2 := seedReport("https://vc2.example.com")

			reports, err := s.RightSizing().ListReports(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(reports).To(HaveLen(2))
			ids := []string{reports[0].ID, reports[1].ID}
			Expect(ids).To(ConsistOf(id1, id2))
		})
	})

	Describe("GetReport", func() {
		It("should return the report with all VM metrics", func() {
			id := seedReport("https://vc1.example.com")

			report, err := s.RightSizing().GetReport(ctx, id)
			Expect(err).NotTo(HaveOccurred())
			Expect(report.ID).To(Equal(id))
			Expect(report.VMs).To(HaveLen(1))
			Expect(report.VMs[0].Metrics).To(HaveKey("cpu.usagemhz.average"))
			cpu := report.VMs[0].Metrics["cpu.usagemhz.average"]
			Expect(cpu.SampleCount).To(Equal(360))
			Expect(cpu.Average).To(Equal(500.0))
		})

		It("should return a ResourceNotFoundError for unknown ID", func() {
			_, err := s.RightSizing().GetReport(ctx, "no-such-id")
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})
	})

	Describe("ListInventoryVMs", func() {
		It("should return empty when vinfo has no rows", func() {
			vms, err := s.RightSizing().ListInventoryVMs(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(BeEmpty())
		})
	})

	Describe("WriteVMWarnings", func() {
		It("should insert warning rows for VMs with no data", func() {
			id, _ := s.RightSizing().CreateReport(ctx, testReport(), 2, 1)
			warnings := []models.VMWarning{
				{MOID: "vm-100", VMName: "vm-a", Warning: "vCenter returned no data for this VM"},
				{MOID: "vm-200", VMName: "vm-b", Warning: "query succeeded but returned no samples"},
			}
			Expect(s.RightSizing().WriteVMWarnings(ctx, id, warnings)).To(Succeed())

			var count int
			Expect(db.QueryRow(`SELECT COUNT(*) FROM rightsizing_vm_warnings WHERE report_id = ?`, id).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(2))
		})

		It("should be idempotent on duplicate (report_id, moid)", func() {
			id, _ := s.RightSizing().CreateReport(ctx, testReport(), 1, 1)
			w := []models.VMWarning{{MOID: "vm-100", VMName: "vm-a", Warning: "no data"}}
			Expect(s.RightSizing().WriteVMWarnings(ctx, id, w)).To(Succeed())
			Expect(s.RightSizing().WriteVMWarnings(ctx, id, w)).To(Succeed())

			var count int
			Expect(db.QueryRow(`SELECT COUNT(*) FROM rightsizing_vm_warnings WHERE report_id = ?`, id).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(1))
		})

		It("should return nil and insert nothing for empty input", func() {
			id, _ := s.RightSizing().CreateReport(ctx, testReport(), 0, 1)
			Expect(s.RightSizing().WriteVMWarnings(ctx, id, nil)).To(Succeed())
		})
	})

	Describe("ComputeAndStoreUtilization", func() {
		It("should compute and store utilization rows for VMs with metrics", func() {
			id, _ := s.RightSizing().CreateReport(ctx, testReport(), 1, 1)
			Expect(s.RightSizing().WriteBatch(ctx, id, []models.RightSizingMetric{
				{VMName: "vm-a", MOID: "vm-100", MetricKey: "cpu.usage.average",
					SampleCount: 360, Average: 5000, P95: 8000, Max: 9000, Latest: 4500},
				{VMName: "vm-a", MOID: "vm-100", MetricKey: "disk.used.latest",
					SampleCount: 1, Average: 5000000, Latest: 5000000},
				{VMName: "vm-a", MOID: "vm-100", MetricKey: "disk.provisioned.latest",
					SampleCount: 1, Average: 10000000, Latest: 10000000},
			})).To(Succeed())
			Expect(s.RightSizing().IncrementWrittenBatchCount(ctx, id)).To(Succeed())

			Expect(s.RightSizing().ComputeAndStoreUtilization(ctx, id)).To(Succeed())

			var count int
			Expect(db.QueryRow(`SELECT COUNT(*) FROM rightsizing_vm_utilization WHERE report_id = ?`, id).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(1))

			var cpuP95, diskPct float64
			Expect(db.QueryRow(
				`SELECT cpu_p95_pct, disk_pct FROM rightsizing_vm_utilization WHERE report_id = ? AND moid = ?`,
				id, "vm-100",
			).Scan(&cpuP95, &diskPct)).To(Succeed())
			Expect(cpuP95).To(BeNumerically("~", 80.0, 0.01))  // 8000 / 100
			Expect(diskPct).To(BeNumerically("~", 50.0, 0.01)) // 5000000 / 10000000 * 100
		})

		It("should be idempotent via ON CONFLICT DO NOTHING", func() {
			id, _ := s.RightSizing().CreateReport(ctx, testReport(), 1, 1)
			Expect(s.RightSizing().WriteBatch(ctx, id, []models.RightSizingMetric{
				{VMName: "vm-a", MOID: "vm-100", MetricKey: "cpu.usage.average",
					SampleCount: 1, Average: 5000, Latest: 5000},
			})).To(Succeed())
			Expect(s.RightSizing().IncrementWrittenBatchCount(ctx, id)).To(Succeed())
			Expect(s.RightSizing().ComputeAndStoreUtilization(ctx, id)).To(Succeed())
			Expect(s.RightSizing().ComputeAndStoreUtilization(ctx, id)).To(Succeed()) // second call

			var count int
			Expect(db.QueryRow(`SELECT COUNT(*) FROM rightsizing_vm_utilization WHERE report_id = ?`, id).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(1))
		})
	})

	Describe("GetVMUtilization", func() {
		It("should return utilization for a VM from the latest completed report", func() {
			id, _ := s.RightSizing().CreateReport(ctx, testReport(), 1, 1)
			Expect(s.RightSizing().WriteBatch(ctx, id, []models.RightSizingMetric{
				{VMName: "vm-a", MOID: "vm-100", MetricKey: "cpu.usage.average",
					SampleCount: 10, Average: 5000, P95: 8000, Max: 9000, Latest: 4500},
				{VMName: "vm-a", MOID: "vm-100", MetricKey: "disk.used.latest",
					SampleCount: 1, Latest: 5000000},
				{VMName: "vm-a", MOID: "vm-100", MetricKey: "disk.provisioned.latest",
					SampleCount: 1, Latest: 10000000},
			})).To(Succeed())
			Expect(s.RightSizing().IncrementWrittenBatchCount(ctx, id)).To(Succeed())
			Expect(s.RightSizing().ComputeAndStoreUtilization(ctx, id)).To(Succeed())

			d, err := s.RightSizing().GetVMUtilization(ctx, "vm-100")
			Expect(err).NotTo(HaveOccurred())
			Expect(d.MOID).To(Equal("vm-100"))
			Expect(d.CpuP95).To(BeNumerically("~", 80.0, 0.01))
			Expect(d.Disk).To(BeNumerically("~", 50.0, 0.01))
		})

		It("should return ResourceNotFoundError when no data exists", func() {
			_, err := s.RightSizing().GetVMUtilization(ctx, "vm-no-data")
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})
	})

	Describe("GetReport merges warning-only VMs", func() {
		It("should include VMs with no data alongside VMs with metrics", func() {
			id, _ := s.RightSizing().CreateReport(ctx, testReport(), 2, 1)
			Expect(s.RightSizing().WriteBatch(ctx, id, []models.RightSizingMetric{
				{VMName: "vm-a", MOID: "vm-100", MetricKey: "cpu.usagemhz.average", SampleCount: 360, Average: 500},
			})).To(Succeed())
			Expect(s.RightSizing().WriteVMWarnings(ctx, id, []models.VMWarning{
				{MOID: "vm-200", VMName: "vm-b", Warning: "vCenter returned no data for this VM"},
			})).To(Succeed())

			report, err := s.RightSizing().GetReport(ctx, id)
			Expect(err).NotTo(HaveOccurred())
			Expect(report.VMs).To(HaveLen(2))

			vmByMOID := map[string]models.RightsizingVMReport{}
			for _, vm := range report.VMs {
				vmByMOID[vm.MOID] = vm
			}
			Expect(vmByMOID["vm-100"].Metrics).To(HaveKey("cpu.usagemhz.average"))
			Expect(vmByMOID["vm-100"].Warnings).To(BeEmpty())
			Expect(vmByMOID["vm-200"].Metrics).To(BeEmpty())
			Expect(vmByMOID["vm-200"].Warnings).To(ConsistOf("vCenter returned no data for this VM"))
		})
	})
})
