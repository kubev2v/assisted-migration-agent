package store_test

import (
	"context"
	"database/sql"
	"sort"

	sq "github.com/Masterminds/squirrel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("VMStore cross-table filters", func() {
	var (
		ctx context.Context
		s   *store.Store
		db  *sql.DB
	)

	vmIDs := func(vms []models.VirtualMachineSummary) []string {
		ids := make([]string, len(vms))
		for i, vm := range vms {
			ids[i] = vm.ID
		}
		sort.Strings(ids)
		return ids
	}

	BeforeEach(func() {
		ctx = context.Background()
		var err error

		db, err = store.NewDB(":memory:")
		Expect(err).NotTo(HaveOccurred())

		s = store.NewStore(db, test.NewMockValidator())

		err = s.Migrate(ctx)
		Expect(err).NotTo(HaveOccurred())

		err = test.InsertVMs(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		err = test.InsertVMMemory(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		err = test.InsertVMDatastores(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		err = test.InsertVMInspections(ctx, db)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	Context("vinfo columns", func() {
		It("should filter by firmware", func() {
			f := store.ByFilter("firmware = 'efi'")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-003", "vm-004"}))
		})

		It("should filter by host", func() {
			f := store.ByFilter("host = 'esxi-01.local'")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-001", "vm-002"}))
		})

		It("should filter by cpus with comparison", func() {
			f := store.ByFilter("cpus >= 4")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-003", "vm-004", "vm-005", "vm-006"}))
		})

		It("should filter by dns_name regex", func() {
			f := store.ByFilter("dns_name ~ /db/")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-003", "vm-004"}))
		})
	})

	Context("vdisk columns (disk.* prefix)", func() {
		It("should filter by individual disk capacity", func() {
			f := store.ByFilter("disk.capacity >= 500")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-003", "vm-004"}))
		})

		It("should filter by disk mode", func() {
			f := store.ByFilter("disk.mode = 'independent_persistent'")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-007"}))
		})

		It("should filter by RDM disk", func() {
			f := store.ByFilter("disk.raw = true")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-007"}))
		})

		It("should filter by disk controller", func() {
			f := store.ByFilter("disk.controller = 'NVME'")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-008", "vm-009"}))
		})
	})

	Context("concerns columns (concern.* prefix)", func() {
		It("should filter by critical category", func() {
			f := store.ByFilter("concern.category = 'Critical'")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-007"}))
		})

		It("should filter by concern label regex", func() {
			f := store.ByFilter("concern.label ~ /RDM/")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-007"}))
		})

		It("should filter by warning category", func() {
			f := store.ByFilter("concern.category = 'Warning'")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-003", "vm-004", "vm-007"}))
		})
	})

	Context("vcpu columns (cpu.* prefix)", func() {
		It("should filter by cores per socket", func() {
			f := store.ByFilter("cpu.cores_per_socket >= 4")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-003", "vm-004", "vm-005", "vm-006"}))
		})
	})

	Context("vmemory columns (mem.* prefix)", func() {
		It("should filter by hot add enabled", func() {
			f := store.ByFilter("mem.hot_add = true")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-003", "vm-005"}))
		})

		It("should filter by ballooned memory", func() {
			f := store.ByFilter("mem.ballooned > 0")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-003", "vm-008"}))
		})
	})

	Context("vnetwork columns (net.* prefix)", func() {
		It("should filter by production network", func() {
			f := store.ByFilter("net.network = 'Production'")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-003"}))
		})

		It("should filter by VM Network", func() {
			f := store.ByFilter("net.network = 'VM Network'")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-001", "vm-002"}))
		})
	})

	Context("vdatastore columns (datastore.* prefix)", func() {
		It("should filter by datastore type", func() {
			f := store.ByFilter("datastore.type = 'VMFS'")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(10))
		})

		It("should return empty for non-existent datastore type", func() {
			f := store.ByFilter("datastore.type = 'NFS'")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(BeEmpty())
		})

		It("should filter by datastore capacity", func() {
			f := store.ByFilter("datastore.capacity >= 1048576")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(10))
		})
	})

	Context("combined cross-table filters", func() {
		It("should combine vinfo and vdisk filters", func() {
			f := store.ByFilter("firmware = 'efi' and disk.capacity >= 500")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-003", "vm-004"}))
		})

		It("should combine concern and network filters", func() {
			f := store.ByFilter("concern.category = 'Critical' and net.network = 'Staging'")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-007"}))
		})

		It("should combine cpu and cluster filters", func() {
			f := store.ByFilter("cpu.cores_per_socket >= 4 and cluster = 'staging'")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-005", "vm-006"}))
		})

		It("should combine disk controller and powerstate filters", func() {
			f := store.ByFilter("disk.controller = 'NVME' and powerstate = 'poweredOn'")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-008"}))
		})

		It("should combine memory and vmemory filters", func() {
			f := store.ByFilter("mem.hot_add = true and memory >= 8192")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs(vms)).To(Equal([]string{"vm-003", "vm-005"}))
		})
	})

	Context("DISTINCT correctness", func() {
		It("should return each VM once even with multiple disks", func() {
			f := store.ByFilter("disk.capacity > 0")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			ids := vmIDs(vms)
			Expect(ids).To(HaveLen(10))

			unique := make(map[string]bool)
			for _, id := range ids {
				Expect(unique).NotTo(HaveKey(id), "duplicate VM ID: %s", id)
				unique[id] = true
			}
		})

		It("should return each VM once even with multiple concerns", func() {
			f := store.ByFilter("concern.category = 'Warning'")
			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			ids := vmIDs(vms)

			unique := make(map[string]bool)
			for _, id := range ids {
				Expect(unique).NotTo(HaveKey(id), "duplicate VM ID: %s", id)
				unique[id] = true
			}
		})

		It("should have List count match Count for multi-row JOINs", func() {
			f := store.ByFilter("disk.capacity > 0")

			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())
			Expect(err).NotTo(HaveOccurred())

			count, err := s.VM().Count(ctx, f)
			Expect(err).NotTo(HaveOccurred())

			Expect(len(vms)).To(Equal(count))
		})

		It("should have List count match Count for concern filters", func() {
			f := store.ByFilter("concern.category = 'Warning'")

			vms, err := s.VM().List(ctx, []sq.Sqlizer{f}, store.WithDefaultSort())
			Expect(err).NotTo(HaveOccurred())

			count, err := s.VM().Count(ctx, f)
			Expect(err).NotTo(HaveOccurred())

			Expect(len(vms)).To(Equal(count))
		})
	})

	Context("nil/empty filter edge cases", func() {
		It("should return all VMs with nil filters", func() {
			vms, err := s.VM().List(ctx, nil, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(10))
		})

		It("should return all VMs with empty filter slice", func() {
			vms, err := s.VM().List(ctx, []sq.Sqlizer{}, store.WithDefaultSort())

			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(10))
		})

		It("ByFilter with empty string should return nil", func() {
			f := store.ByFilter("")
			Expect(f).To(BeNil())
		})
	})

	Context("ByFilter with group service", func() {
		It("should filter VMs through group concern filter", func() {
			g, err := s.Group().Create(ctx, models.Group{
				Name:   "critical-vms",
				Filter: "concern.category = 'Critical'",
			})
			Expect(err).NotTo(HaveOccurred())

			svc := services.NewGroupService(s)
			vms, total, err := svc.ListVirtualMachines(ctx, g.ID, services.GroupGetParams{})
			Expect(err).NotTo(HaveOccurred())

			Expect(vmIDs(vms)).To(Equal([]string{"vm-007"}))
			Expect(total).To(Equal(1))
		})

		It("should filter VMs through group disk controller filter", func() {
			g, err := s.Group().Create(ctx, models.Group{
				Name:   "nvme-vms",
				Filter: "disk.controller = 'NVME'",
			})
			Expect(err).NotTo(HaveOccurred())

			svc := services.NewGroupService(s)
			vms, total, err := svc.ListVirtualMachines(ctx, g.ID, services.GroupGetParams{})
			Expect(err).NotTo(HaveOccurred())

			Expect(vmIDs(vms)).To(Equal([]string{"vm-008", "vm-009"}))
			Expect(total).To(Equal(2))

			for _, vm := range vms {
				Expect(vm.ID).NotTo(BeEmpty())
				Expect(vm.Name).NotTo(BeEmpty())
			}
		})

		It("should combine group filter with pagination", func() {
			g, err := s.Group().Create(ctx, models.Group{
				Name:   "staging-warnings",
				Filter: "concern.category = 'Warning'",
			})
			Expect(err).NotTo(HaveOccurred())

			svc := services.NewGroupService(s)
			vms, total, err := svc.ListVirtualMachines(ctx, g.ID, services.GroupGetParams{
				Limit: 1,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(vms).To(HaveLen(1))
			Expect(total).To(Equal(3))
		})
	})
})
