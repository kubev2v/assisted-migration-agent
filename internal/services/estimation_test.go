package services_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kubev2v/migration-planner-common/estimations/complexity"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	"github.com/kubev2v/migration-planner/pkg/inventory"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const insertVinfoStm = `INSERT INTO vinfo ("VM ID", "VM", "Cluster", "Powerstate", "Template", "Memory", "CPUs") VALUES ('%s', '%s', '%s', '%s', %t, %d, %d)`

var _ = Describe("EstimationService", func() {
	var (
		ctx     context.Context
		pool    *store.Pool
		tmpDir  string
		st      *store.Store
		srv     *services.EstimationService
		builder *estimationInventoryBuilder
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "estimation-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)
		database, err := pool.NewDatabase("test", filepath.Join(tmpDir, "test.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		pool.Add(database)

		st, err = database.Store()
		Expect(err).NotTo(HaveOccurred())
		Expect(duckdb_parser.New(st.Querier(), nil).Init()).To(Succeed())
		Expect(database.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			return migrations.RunCollection(ctx, db, "test")
		})).To(Succeed())

		vms := []estimationVM{
			{id: "vm-1", name: "web-server", cluster: "production", memory: 4096, cpus: 2,
				osName: "Red Hat Enterprise Linux 8 (64-bit)", diskTier: "0-10TiB", diskSizeTB: 1.5},
			{id: "vm-2", name: "db-server", cluster: "development", memory: 8192, cpus: 4,
				osName: "Windows Server 2019", diskTier: "10-20TiB", diskSizeTB: 12.25},
			{id: "vm-3", name: "app-server", cluster: "production", memory: 4096, cpus: 2,
				osName: "CentOS 7 (64-bit)", diskTier: "0-10TiB", diskSizeTB: 3},
			{id: "vm-4", name: "test-server", cluster: "development", memory: 2048, cpus: 2,
				osName: "Red Hat Enterprise Linux 8 (64-bit)", diskTier: "0-10TiB", diskSizeTB: 2},
			{id: "vm-5", name: "legacy-server", cluster: "development", memory: 2048, cpus: 2,
				osName: "Ubuntu Linux 22.04", diskTier: "50+TiB", diskSizeTB: 60.5},
		}

		builder = &estimationInventoryBuilder{vms: make(map[string]estimationVM, len(vms))}

		for _, vm := range vms {
			_, err = st.Querier().ExecContext(ctx, fmt.Sprintf(insertVinfoStm,
				vm.id, vm.name, vm.cluster, "poweredOn", false, vm.memory, vm.cpus))
			Expect(err).NotTo(HaveOccurred())

			builder.vms[vm.id] = vm
		}

		srv = services.NewEstimationService(st, builder)
	})

	AfterEach(func() {
		if pool != nil {
			pool.Close()
		}

		if tmpDir != "" {
			Expect(os.RemoveAll(tmpDir)).To(Succeed())
		}
	})

	Describe("CalculateMigrationComplexity", func() {
		It("should return OS and disk breakdowns with counts, sizes, and ratings", func() {
			result, err := srv.CalculateMigrationComplexity(ctx, "")

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.ComplexityByDisk).To(ConsistOf(
				complexity.DiskComplexityEntry{Score: 1, VMCount: 3, TotalSizeTB: 6.5},
				complexity.DiskComplexityEntry{Score: 2, VMCount: 1, TotalSizeTB: 12.25},
				complexity.DiskComplexityEntry{Score: 3},
				complexity.DiskComplexityEntry{Score: 4, VMCount: 1, TotalSizeTB: 60.5},
			))
			Expect(result.ComplexityByOS).To(ConsistOf(
				complexity.OSDifficultyEntry{Score: 0},
				complexity.OSDifficultyEntry{Score: 1, VMCount: 3},
				complexity.OSDifficultyEntry{Score: 2, VMCount: 1},
				complexity.OSDifficultyEntry{Score: 3, VMCount: 1},
				complexity.OSDifficultyEntry{Score: 4},
			))
			Expect(result.ComplexityByOSName).To(ConsistOf(
				complexity.OSNameEntry{Name: "Red Hat Enterprise Linux 8 (64-bit)", Score: 1, VMCount: 2},
				complexity.OSNameEntry{Name: "CentOS 7 (64-bit)", Score: 1, VMCount: 1},
				complexity.OSNameEntry{Name: "Windows Server 2019", Score: 2, VMCount: 1},
				complexity.OSNameEntry{Name: "Ubuntu Linux 22.04", Score: 3, VMCount: 1},
			))
			Expect(result.DiskSizeRatings).To(Equal(map[string]complexity.Score{
				"0-10TiB": 1, "10-20TiB": 2, "20-50TiB": 3, "50+TiB": 4,
			}))
			Expect(result.OSRatings).To(Equal(map[string]complexity.Score{
				"Red Hat Enterprise Linux 8 (64-bit)": 1,
				"CentOS 7 (64-bit)":                   1,
				"Windows Server 2019":                 2,
				"Ubuntu Linux 22.04":                  3,
			}))
		})

		DescribeTable("should build inventory only for matching VM IDs",
			func(expression string, expectedIDs []string, expectedDiskTB float64, expectedOSRatings map[string]complexity.Score) {
				result, err := srv.CalculateMigrationComplexity(ctx, expression)

				Expect(err).NotTo(HaveOccurred())
				Expect(builder.calls).To(Equal(1))
				Expect(builder.vmIDs).To(ConsistOf(expectedIDs))
				Expect(result.OSRatings).To(Equal(expectedOSRatings))

				var osVMCount, diskVMCount int
				var totalDiskTB float64
				for _, entry := range result.ComplexityByOS {
					osVMCount += entry.VMCount
				}

				for _, entry := range result.ComplexityByDisk {
					diskVMCount += entry.VMCount
					totalDiskTB += entry.TotalSizeTB
				}

				Expect(osVMCount).To(Equal(len(expectedIDs)))
				Expect(diskVMCount).To(Equal(len(expectedIDs)))
				Expect(totalDiskTB).To(BeNumerically("~", expectedDiskTB))
			},
			Entry("all VMs when the expression is empty", "", []string{"vm-1", "vm-2", "vm-3", "vm-4", "vm-5"}, 79.25,
				map[string]complexity.Score{
					"Red Hat Enterprise Linux 8 (64-bit)": 1, "CentOS 7 (64-bit)": 1,
					"Windows Server 2019": 2, "Ubuntu Linux 22.04": 3,
				}),
			Entry("VMs in the selected cluster", "cluster = 'production'", []string{"vm-1", "vm-3"}, 4.5,
				map[string]complexity.Score{"Red Hat Enterprise Linux 8 (64-bit)": 1, "CentOS 7 (64-bit)": 1}),
			Entry("a VM selected by name", "name = 'db-server'", []string{"vm-2"}, 12.25,
				map[string]complexity.Score{"Windows Server 2019": 2}),
		)

		Context("when no VMs match", func() {
			It("should return a validation error without building inventory", func() {
				result, err := srv.CalculateMigrationComplexity(ctx, "name = 'missing'")

				Expect(err).To(HaveOccurred())
				Expect(srvErrors.IsValidationError(err)).To(BeTrue())
				Expect(result).To(BeNil())
				Expect(builder.calls).To(BeZero())
			})
		})

		Context("when inventory data is missing", func() {
			DescribeTable("should reject missing OS information or a nonpositive VM total",
				func(total int, osInfo map[string]inventory.OSInfo) {
					builder.totalOverride = &total
					builder.osInfoOverride = &osInfo

					result, err := srv.CalculateMigrationComplexity(ctx, "")

					Expect(err).To(MatchError("inventory has missing data"))
					Expect(result).To(BeNil())
				},
				Entry("nil OS information", 5, nil),
				Entry("zero VM total", 0, map[string]inventory.OSInfo{"CentOS 7": {Count: 1}}),
				Entry("negative VM total", -1, map[string]inventory.OSInfo{"CentOS 7": {Count: 1}}),
			)

			DescribeTable("should return a validation error when neither disk tier source has data",
				func(complexityTiers, sizeTiers map[string]inventory.DiskSizeTierSummary) {
					builder.diskComplexityTiersOverride = &complexityTiers
					builder.diskSizeTiersOverride = &sizeTiers

					result, err := srv.CalculateMigrationComplexity(ctx, "")

					Expect(err).To(HaveOccurred())
					Expect(srvErrors.IsValidationError(err)).To(BeTrue())
					Expect(result).To(BeNil())
				},
				Entry("nil disk tiers", nil, nil),
				Entry("empty disk tiers", map[string]inventory.DiskSizeTierSummary{}, map[string]inventory.DiskSizeTierSummary{}),
			)
		})

		Context("when a dependency fails", func() {
			It("should return a VM listing error without building inventory when the database is closed", func() {
				pool.Close()

				result, err := srv.CalculateMigrationComplexity(ctx, "")

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("listing VMs for estimation"))
				Expect(result).To(BeNil())
				Expect(builder.calls).To(BeZero())
			})
		})
	})
})

type estimationVM struct {
	id, name, cluster, osName, diskTier string
	memory, cpus                        int
	diskSizeTB                          float64
}

type estimationInventoryBuilder struct {
	vms   map[string]estimationVM
	vmIDs []string
	calls int

	totalOverride               *int
	osInfoOverride              *map[string]inventory.OSInfo
	diskComplexityTiersOverride *map[string]inventory.DiskSizeTierSummary
	diskSizeTiersOverride       *map[string]inventory.DiskSizeTierSummary
}

func (b *estimationInventoryBuilder) BuildInventory(_ context.Context, vmIDs []string) (*inventory.Inventory, error) {
	b.vmIDs = append([]string(nil), vmIDs...)
	b.calls++

	vmData := inventory.VMsData{
		OSInfo:              make(map[string]inventory.OSInfo),
		DiskComplexityTiers: make(map[string]inventory.DiskSizeTierSummary),
	}

	for _, id := range vmIDs {
		vm, ok := b.vms[id]
		if !ok {
			return nil, fmt.Errorf("unknown test VM: %s", id)
		}

		vmData.Total++

		osInfo := vmData.OSInfo[vm.osName]
		osInfo.Count++
		vmData.OSInfo[vm.osName] = osInfo

		tier := vmData.DiskComplexityTiers[vm.diskTier]
		tier.VMCount++
		tier.TotalSizeTB += vm.diskSizeTB
		vmData.DiskComplexityTiers[vm.diskTier] = tier
	}

	if b.totalOverride != nil {
		vmData.Total = *b.totalOverride
	}

	if b.osInfoOverride != nil {
		vmData.OSInfo = *b.osInfoOverride
	}

	if b.diskComplexityTiersOverride != nil {
		vmData.DiskComplexityTiers = *b.diskComplexityTiersOverride
	}

	if b.diskSizeTiersOverride != nil {
		vmData.DiskSizeTiers = *b.diskSizeTiersOverride
	}

	return &inventory.Inventory{VCenter: &inventory.InventoryData{VMs: vmData}}, nil
}
