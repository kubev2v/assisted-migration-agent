package services_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const insertSizerVinfoStm = `INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Template", "Memory", "CPUs", "migration_excluded") VALUES ('%s', '%s', 'poweredOn', '%s', false, %d, %d, %t)`

var _ = Describe("SizerService", func() {
	var (
		ctx    context.Context
		pool   *store.Pool
		tmpDir string
		st     *store.Store
		srv    *services.SizerService
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "sizer-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)
		database, err := pool.NewDatabase("test", filepath.Join(tmpDir, "test.duckdb"), time.Now(), store.EagerConnectionInitilization, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())

		st, err = database.Store()
		Expect(err).NotTo(HaveOccurred())
		Expect(duckdb_parser.New(st.Querier(), nil).Init()).To(Succeed())
		Expect(database.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			return migrations.RunCollection(ctx, db, "test")
		})).To(Succeed())

		srv = services.NewSizerService(st)
	})

	AfterEach(func() {
		if pool != nil {
			pool.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	insertVM := func(id, cluster string, cpus, memoryMB int, excluded bool) {
		_, err := st.Querier().ExecContext(ctx, fmt.Sprintf(insertSizerVinfoStm, id, id, cluster, memoryMB, cpus, excluded))
		Expect(err).NotTo(HaveOccurred())
	}

	// baseForm is a valid HA request (control plane defaults to 3 nodes, non-schedulable).
	baseForm := func() models.ClusterRequirementsRequestForm {
		return models.ClusterRequirementsRequestForm{
			CPUOverCommitMultiplier:    1,
			MemoryOverCommitMultiplier: 1,
			WorkerNodeCPU:              16,
			WorkerNodeMemory:           64,
		}
	}

	Describe("CalculateClusterRequirements - topology", func() {
		It("sizes a default HA cluster with 3 control-plane nodes, workers, and failover", func() {
			insertVM("vm-1", "prod", 2, 4096, false)
			insertVM("vm-2", "prod", 2, 4096, false)

			result, err := srv.CalculateClusterRequirements(ctx, baseForm())

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.InventoryTotals.TotalVMs).To(Equal(2))
			Expect(result.InventoryTotals.TotalCPU).To(Equal(4))
			Expect(result.InventoryTotals.TotalMemory).To(Equal(8))

			Expect(result.ClusterSizing.ControlPlaneNodes).To(Equal(3))
			Expect(result.ClusterSizing.WorkerNodes - result.ClusterSizing.FailoverNodes).To(BeNumerically(">=", 1))
			Expect(result.ClusterSizing.FailoverNodes).To(BeNumerically(">=", 2))
		})

		It("sizes a single-node cluster as exactly one control-plane node", func() {
			insertVM("vm-1", "prod", 2, 4096, false)

			form := baseForm()
			form.ControlPlaneNodeCount = intPtr(1)
			form.ControlPlaneSchedulable = boolPtr(true)
			form.ControlPlaneCPU = intPtr(32)
			form.ControlPlaneMemory = intPtr(64)

			result, err := srv.CalculateClusterRequirements(ctx, form)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.ClusterSizing.TotalNodes).To(Equal(1))
			Expect(result.ClusterSizing.ControlPlaneNodes).To(Equal(1))
			Expect(result.ClusterSizing.WorkerNodes).To(Equal(0))
			Expect(result.ClusterSizing.FailoverNodes).To(Equal(0))
		})

		It("sizes a hosted control plane with worker nodes only", func() {
			insertVM("vm-1", "prod", 2, 4096, false)
			insertVM("vm-2", "prod", 2, 4096, false)

			form := baseForm()
			form.HostedControlPlane = boolPtr(true)

			result, err := srv.CalculateClusterRequirements(ctx, form)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.ClusterSizing.ControlPlaneNodes).To(Equal(0))
			Expect(result.ClusterSizing.WorkerNodes - result.ClusterSizing.FailoverNodes).To(BeNumerically(">=", 1))
		})

		It("sizes a compact cluster with control-plane nodes only and no failover", func() {
			insertVM("vm-1", "prod", 2, 4096, false)
			insertVM("vm-2", "prod", 2, 4096, false)

			form := baseForm()
			form.ControlPlaneNodeCount = intPtr(3)
			form.ControlPlaneSchedulable = boolPtr(true)
			form.ControlPlaneCPU = intPtr(32)
			form.ControlPlaneMemory = intPtr(64)
			form.CompactMode = boolPtr(true)

			result, err := srv.CalculateClusterRequirements(ctx, form)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.ClusterSizing.TotalNodes).To(Equal(3))
			Expect(result.ClusterSizing.ControlPlaneNodes).To(Equal(3))
			Expect(result.ClusterSizing.WorkerNodes).To(Equal(0))
			Expect(result.ClusterSizing.FailoverNodes).To(Equal(0))
		})

		It("keeps 3 control-plane nodes when the control plane is schedulable", func() {
			insertVM("vm-1", "prod", 2, 4096, false)
			insertVM("vm-2", "prod", 2, 4096, false)

			form := baseForm()
			form.ControlPlaneSchedulable = boolPtr(true)
			form.ControlPlaneCPU = intPtr(32)
			form.ControlPlaneMemory = intPtr(64)

			result, err := srv.CalculateClusterRequirements(ctx, form)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.ClusterSizing.ControlPlaneNodes).To(Equal(3))
		})
	})

	Describe("CalculateClusterRequirements - request validation", func() {
		BeforeEach(func() {
			insertVM("vm-1", "prod", 2, 4096, false)
		})

		It("rejects a single-node cluster with a non-schedulable control plane", func() {
			form := baseForm()
			form.ControlPlaneNodeCount = intPtr(1)
			form.ControlPlaneSchedulable = boolPtr(false)

			_, err := srv.CalculateClusterRequirements(ctx, form)

			Expect(err).To(MatchError(ContainSubstring("single-node clusters require controlPlaneSchedulable to be true")))
		})

		It("rejects compact mode without a schedulable control plane", func() {
			form := baseForm()
			form.ControlPlaneNodeCount = intPtr(3)
			form.ControlPlaneSchedulable = boolPtr(false)
			form.CompactMode = boolPtr(true)

			_, err := srv.CalculateClusterRequirements(ctx, form)

			Expect(err).To(MatchError(ContainSubstring("compactMode requires controlPlaneSchedulable to be true")))
		})

		DescribeTable("rejects hosted control plane combined with control-plane settings",
			func(mutate func(*models.ClusterRequirementsRequestForm), wantSubstr string) {
				form := baseForm()
				form.HostedControlPlane = boolPtr(true)
				mutate(&form)

				_, err := srv.CalculateClusterRequirements(ctx, form)

				Expect(err).To(MatchError(ContainSubstring(wantSubstr)))
			},
			Entry("control-plane node count", func(f *models.ClusterRequirementsRequestForm) {
				f.ControlPlaneNodeCount = intPtr(3)
			}, "controlPlaneNodeCount cannot be specified when hostedControlPlane is true"),
			Entry("control-plane CPU", func(f *models.ClusterRequirementsRequestForm) {
				f.ControlPlaneCPU = intPtr(8)
			}, "controlPlaneCPU cannot be specified when hostedControlPlane is true"),
			Entry("control-plane memory", func(f *models.ClusterRequirementsRequestForm) {
				f.ControlPlaneMemory = intPtr(16)
			}, "controlPlaneMemory cannot be specified when hostedControlPlane is true"),
			Entry("control-plane schedulable", func(f *models.ClusterRequirementsRequestForm) {
				f.ControlPlaneSchedulable = boolPtr(true)
			}, "controlPlaneSchedulable cannot be specified when hostedControlPlane is true"),
			Entry("compact mode", func(f *models.ClusterRequirementsRequestForm) {
				f.CompactMode = boolPtr(true)
			}, "compactMode cannot be true when hostedControlPlane is true"),
		)

		DescribeTable("rejects out-of-range node sizing",
			func(mutate func(*models.ClusterRequirementsRequestForm), wantSubstr string) {
				form := baseForm()
				mutate(&form)

				_, err := srv.CalculateClusterRequirements(ctx, form)

				Expect(err).To(MatchError(ContainSubstring(wantSubstr)))
			},
			Entry("worker CPU too low", func(f *models.ClusterRequirementsRequestForm) {
				f.WorkerNodeCPU = 1
			}, "workerNodeCPU must be between 2 and 384"),
			Entry("worker memory too low", func(f *models.ClusterRequirementsRequestForm) {
				f.WorkerNodeMemory = 2
			}, "workerNodeMemory must be between 4 and 4096 GB"),
			Entry("worker threads below CPU", func(f *models.ClusterRequirementsRequestForm) {
				f.WorkerNodeThreads = intPtr(4)
			}, "workerNodeThreads must be greater than or equal to workerNodeCPU"),
			Entry("control-plane CPU too low", func(f *models.ClusterRequirementsRequestForm) {
				f.ControlPlaneCPU = intPtr(1)
			}, "controlPlaneCPU must be between 2 and 384"),
			Entry("control-plane node count not 1 or 3", func(f *models.ClusterRequirementsRequestForm) {
				f.ControlPlaneNodeCount = intPtr(2)
			}, "controlPlaneNodeCount must be 1 or 3"),
		)
	})

	Describe("CalculateClusterRequirements - inventory selection", func() {
		It("returns a validation error when no VMs match", func() {
			_, err := srv.CalculateClusterRequirements(ctx, baseForm())

			Expect(srvErrors.IsValidationError(err)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring("no VMs available for sizing")))
		})

		It("sizes only the VMs selected by the filter expression", func() {
			insertVM("vm-1", "prod", 2, 4096, false)
			insertVM("vm-2", "prod", 2, 4096, false)
			insertVM("vm-3", "dev", 4, 8192, false)

			form := baseForm()
			form.Expression = "cluster = 'prod'"

			result, err := srv.CalculateClusterRequirements(ctx, form)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.InventoryTotals.TotalVMs).To(Equal(2))
			Expect(result.InventoryTotals.TotalCPU).To(Equal(4))
		})

		It("rounds aggregate memory once so sub-GB VMs are not each rounded up", func() {
			insertVM("vm-1", "prod", 2, 512, false)
			insertVM("vm-2", "prod", 2, 512, false)

			result, err := srv.CalculateClusterRequirements(ctx, baseForm())

			Expect(err).NotTo(HaveOccurred())
			Expect(result.InventoryTotals.TotalMemory).To(Equal(1))
		})

		It("skips VMs excluded from migration", func() {
			insertVM("vm-1", "prod", 2, 4096, false)
			insertVM("vm-2", "prod", 2, 4096, true)

			result, err := srv.CalculateClusterRequirements(ctx, baseForm())

			Expect(err).NotTo(HaveOccurred())
			Expect(result.InventoryTotals.TotalVMs).To(Equal(1))
			Expect(result.InventoryTotals.TotalCPU).To(Equal(2))
		})

		It("rejects a VM with negative CPU or memory", func() {
			insertVM("vm-1", "prod", -1, 4096, false)

			_, err := srv.CalculateClusterRequirements(ctx, baseForm())

			Expect(srvErrors.IsValidationError(err)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring("non negative CPU and memory")))
		})
	})
})

func boolPtr(b bool) *bool {
	return &b
}

func intPtr(i int) *int {
	return &i
}
