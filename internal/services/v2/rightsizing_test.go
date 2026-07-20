package v2_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	v2 "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("RightsizingService", func() {
	var (
		ctx      context.Context
		pool     *store.Pool
		database *store.Database
		st       *store.Store2
		sqlDB    *sql.DB
		svc      *v2.RightsizingService
		tmpDir   string
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "rightsizing-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)
		dbPath := filepath.Join(tmpDir, "test.duckdb")
		database, err = pool.NewDatabase("test", dbPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())

		Expect(database.Migrate(ctx, func(ctx context.Context, d *sql.DB) error {
			sqlDB = d
			s, err := database.Store()
			if err != nil {
				return err
			}
			if err := duckdb_parser.New(s.Querier(), nil).Init(); err != nil {
				return err
			}
			return migrations.RunCollection(ctx, d, "test")
		})).To(Succeed())

		st, err = database.Store()
		Expect(err).NotTo(HaveOccurred())

		svc = v2.NewRightsizingService(st)
	})

	AfterEach(func() {
		if database != nil {
			_ = database.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	seedVMs := func(vms ...struct{ id, name string }) {
		for _, vm := range vms {
			_, err := sqlDB.ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "CPUs", "Memory", "Template")
				VALUES (?, ?, 'poweredOn', 'cluster-a', 4, 4096, false)
			`, vm.id, vm.name)
			Expect(err).NotTo(HaveOccurred())
		}
	}

	seedReportWithMetrics := func() string {
		r := models.RightSizingReport{
			VCenter:             "https://vcenter.example.com",
			ClusterID:           "domain-c123",
			IntervalID:          7200,
			WindowStart:         time.Now().Add(-720 * time.Hour).UTC(),
			WindowEnd:           time.Now().UTC(),
			ExpectedSampleCount: 360,
		}
		id, _, err := st.RightSizing().CreateReport(ctx, r, 1, 1)
		Expect(err).NotTo(HaveOccurred())

		Expect(st.RightSizing().WriteBatch(ctx, id, []models.RightSizingMetric{
			{VMName: "vm-a", MOID: "vm-100", MetricKey: "cpu.usagemhz.average",
				SampleCount: 360, Average: 500, P95: 1200, P99: 1500, Max: 2000, Latest: 450},
			{VMName: "vm-a", MOID: "vm-100", MetricKey: "cpu.usage.average",
				SampleCount: 360, Average: 5000, P95: 8000, P99: 9000, Max: 9500, Latest: 4500},
			{VMName: "vm-a", MOID: "vm-100", MetricKey: "mem.consumed.average",
				SampleCount: 360, Average: 2048000, P95: 3072000, P99: 3500000, Max: 4000000, Latest: 2000000},
			{VMName: "vm-a", MOID: "vm-100", MetricKey: "disk.used.latest",
				SampleCount: 360, Average: 500000, P95: 600000, P99: 650000, Max: 700000, Latest: 500000},
			{VMName: "vm-a", MOID: "vm-100", MetricKey: "disk.provisioned.latest",
				SampleCount: 360, Average: 1000000, P95: 1000000, P99: 1000000, Max: 1000000, Latest: 1000000},
		})).To(Succeed())
		Expect(st.RightSizing().IncrementWrittenBatchCount(ctx, id)).To(Succeed())
		return id
	}

	Describe("GetVMUtilization", func() {
		It("should return utilization details for a VM with computed data", func() {
			seedVMs(struct{ id, name string }{"vm-100", "vm-a"})
			reportID := seedReportWithMetrics()

			Expect(st.RightSizing().ComputeAndStoreUtilization(ctx, reportID)).To(Succeed())

			details, err := svc.GetVMUtilization(ctx, "vm-100")
			Expect(err).NotTo(HaveOccurred())
			Expect(details.MOID).To(Equal("vm-100"))
			Expect(details.VMName).To(Equal("vm-a"))
			Expect(details.CpuAvg).To(BeNumerically(">", 0))
		})

		It("should return ResourceNotFoundError for unknown VM", func() {
			_, err := svc.GetVMUtilization(ctx, "does-not-exist")
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})
	})

	Describe("ListLatestClusterUtilization", func() {
		It("should return empty when no reports exist", func() {
			reportID, clusters, err := svc.ListLatestClusterUtilization(ctx, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(reportID).To(BeEmpty())
			Expect(clusters).To(BeEmpty())
		})
	})

	Describe("CreateReportFromInventory", func() {
		It("should create a report and return VMs from inventory", func() {
			seedVMs(
				struct{ id, name string }{"vm-100", "web-server"},
				struct{ id, name string }{"vm-200", "db-server"},
			)

			reportID, vms, start, end, err := svc.CreateReportFromInventory(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(reportID).NotTo(BeEmpty())
			Expect(vms).To(HaveLen(2))
			Expect(end).To(BeTemporally(">", start))
		})

		It("should return empty VM list when no inventory exists", func() {
			reportID, vms, _, _, err := svc.CreateReportFromInventory(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(reportID).NotTo(BeEmpty())
			Expect(vms).To(BeEmpty())
		})
	})

	Describe("PersistMetrics", func() {
		It("should persist metrics in batches", func() {
			vms := []v2.VMInfo{
				{Name: "vm-a", Ref: types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-100"}},
			}
			vmResults := map[string]v2.VMReport{
				"vm-100": {
					Name: "vm-a",
					MOID: "vm-100",
					Metrics: map[string]v2.MetricStats{
						"cpu.usagemhz.average": {SampleCount: 360, Average: 500, P95: 1200, P99: 1500, Max: 2000, Latest: 450},
					},
				},
			}

			report := models.RightSizingReport{
				VCenter:             "https://vcenter.example.com",
				ClusterID:           "domain-c123",
				IntervalID:          7200,
				WindowStart:         time.Now().Add(-720 * time.Hour).UTC(),
				WindowEnd:           time.Now().UTC(),
				ExpectedSampleCount: 360,
			}
			reportID, _, err := st.RightSizing().CreateReport(ctx, report, 1, 25)
			Expect(err).NotTo(HaveOccurred())

			Expect(svc.PersistMetrics(ctx, vms, vmResults, reportID)).To(Succeed())
		})
	})

	Describe("PersistVMWarnings", func() {
		It("should persist warnings for VMs with no metrics", func() {
			vms := []v2.VMInfo{
				{Name: "vm-a", Ref: types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-100"}},
				{Name: "vm-b", Ref: types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-200"}},
			}
			vmResults := map[string]v2.VMReport{
				"vm-100": {Name: "vm-a", MOID: "vm-100", Metrics: map[string]v2.MetricStats{}},
				"vm-200": {Name: "vm-b", MOID: "vm-200", Warnings: []string{"specific warning"}},
			}

			report := models.RightSizingReport{
				VCenter:             "https://vcenter.example.com",
				ClusterID:           "domain-c123",
				IntervalID:          7200,
				WindowStart:         time.Now().Add(-720 * time.Hour).UTC(),
				WindowEnd:           time.Now().UTC(),
				ExpectedSampleCount: 360,
			}
			reportID, _, err := st.RightSizing().CreateReport(ctx, report, 2, 25)
			Expect(err).NotTo(HaveOccurred())

			Expect(svc.PersistVMWarnings(ctx, vms, vmResults, reportID)).To(Succeed())
		})

		It("should be a no-op when all VMs have metrics", func() {
			vms := []v2.VMInfo{
				{Name: "vm-a", Ref: types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-100"}},
			}
			vmResults := map[string]v2.VMReport{
				"vm-100": {
					Name: "vm-a", MOID: "vm-100",
					Metrics: map[string]v2.MetricStats{
						"cpu.usagemhz.average": {SampleCount: 1, Average: 100},
					},
				},
			}

			report := models.RightSizingReport{
				VCenter:     "https://vcenter.example.com",
				IntervalID:  7200,
				WindowStart: time.Now().Add(-720 * time.Hour).UTC(),
				WindowEnd:   time.Now().UTC(),
			}
			reportID, _, err := st.RightSizing().CreateReport(ctx, report, 1, 25)
			Expect(err).NotTo(HaveOccurred())

			Expect(svc.PersistVMWarnings(ctx, vms, vmResults, reportID)).To(Succeed())
		})
	})

	Describe("ComputeUtilization", func() {
		It("should compute utilization from persisted metrics", func() {
			seedVMs(struct{ id, name string }{"vm-100", "vm-a"})
			reportID := seedReportWithMetrics()

			Expect(svc.ComputeUtilization(ctx, reportID)).To(Succeed())

			details, err := svc.GetVMUtilization(ctx, "vm-100")
			Expect(err).NotTo(HaveOccurred())
			Expect(details.MOID).To(Equal("vm-100"))
			Expect(details.CpuAvg).To(BeNumerically(">", 0))
		})
	})
})
