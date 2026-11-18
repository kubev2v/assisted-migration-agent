package store_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("VMStore ListForApplianceDetection", Ordered, func() {
	var (
		ctx    context.Context
		s      *store.Store
		pool   *store.Pool
		tmpDir string
	)

	BeforeAll(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "vm-detection-store-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)
		collDB, dbErr := pool.NewDatabase("coll", filepath.Join(tmpDir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(dbErr).NotTo(HaveOccurred())

		Expect(collDB.Migrate(ctx, func(mCtx context.Context, sqlDB *sql.DB) error {
			st, stErr := collDB.Store()
			if stErr != nil {
				return stErr
			}
			if pErr := duckdb_parser.New(st.Querier(), test.NewMockValidator()).Init(); pErr != nil {
				return pErr
			}
			return migrations.RunCollection(mCtx, sqlDB, "collection")
		})).To(Succeed())

		pool.Add(collDB)

		s, err = collDB.Store()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		if pool != nil {
			pool.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	insertVM := func(id, name string) {
		_, err := s.Querier().ExecContext(ctx, `
			INSERT INTO vinfo ("VM ID", "VM") VALUES (?, ?)
		`, id, name)
		Expect(err).NotTo(HaveOccurred())
	}

	insertCPU := func(vmID string, sockets, coresPerSocket int) {
		_, err := s.Querier().ExecContext(ctx, `
			INSERT INTO vcpu ("VM ID", "Sockets", "Cores p/s") VALUES (?, ?, ?)
		`, vmID, sockets, coresPerSocket)
		Expect(err).NotTo(HaveOccurred())
	}

	insertDisk := func(vmID, key string, capacityMiB int) {
		_, err := s.Querier().ExecContext(ctx, `
			INSERT INTO vdisk ("VM ID", "Disk Key", "Capacity MiB") VALUES (?, ?, ?)
		`, vmID, key, capacityMiB)
		Expect(err).NotTo(HaveOccurred())
	}

	findVM := func(vms []models.ApplianceVirtualMachine, id string) models.ApplianceVirtualMachine {
		for _, vm := range vms {
			if vm.ID == id {
				return vm
			}
		}
		Fail("VM not found: " + id)
		return models.ApplianceVirtualMachine{}
	}

	Context("Given an empty collection database", func() {
		When("VMs are listed for appliance detection", func() {
			It("should return no VMs", func() {
				// Act
				vms, err := s.VM().ListForApplianceDetection(ctx, nil)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(BeEmpty())
			})
		})
	})

	Context("Given a collection seeded with VMs", func() {
		BeforeEach(func() {
			_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO vinfo (
					"VM ID", "VM", "Annotation", "Folder", "Host", "SMBIOS UUID", "Firmware",
					"Powerstate", "Connection state", "CPUs", "Memory",
					"DNS Name", "Primary IP Address", "Template", "Datacenter", "Cluster", "NICs"
				) VALUES
					('vm-1', 'pa-vm-01', 'PA-VM firewall', 'folder-1', 'host-1', 'uuid-1', 'efi',
					 'poweredOn', 'connected', 8, 16384, 'pa-vm-01.local', '10.0.0.5', true,
					 'dc-1', 'cluster-a', 3),
					('vm-2', 'app-02', '', 'folder-2', 'host-2', 'uuid-2', 'bios',
					 'poweredOff', 'disconnected', 2, 4096, '', '', false, 'dc-1', 'cluster-b', 1)
			`)
			Expect(err).NotTo(HaveOccurred())

			insertVM("vm-3", "bare-03")

			insertCPU("vm-1", 2, 4)
			insertCPU("vm-2", 1, 2)

			insertDisk("vm-1", "0", 51200)
			insertDisk("vm-1", "1", 102400)
			insertDisk("vm-2", "0", 20480)
		})

		AfterEach(func() {
			for _, table := range []string{"vdisk", "vcpu", "vinfo"} {
				_, err := s.Querier().ExecContext(ctx, `DELETE FROM `+table)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		When("the VMs are listed", func() {
			It("should return every VM ordered by id", func() {
				// Act
				vms, err := s.VM().ListForApplianceDetection(ctx, nil, store.WithDefaultSort())

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
				Expect([]string{vms[0].ID, vms[1].ID, vms[2].ID}).To(Equal([]string{"vm-1", "vm-2", "vm-3"}))
			})

			It("should carry the VM attributes and the summed disk capacity", func() {
				// Act
				vms, err := s.VM().ListForApplianceDetection(ctx, nil, store.WithDefaultSort())

				// Assert
				Expect(err).NotTo(HaveOccurred())

				vm := findVM(vms, "vm-1")
				Expect(vm.Name).To(Equal("pa-vm-01"))
				Expect(vm.Annotation).To(Equal("PA-VM firewall"))
				Expect(vm.Folder).To(Equal("folder-1"))
				Expect(vm.Host).To(Equal("host-1"))
				Expect(vm.UUID).To(Equal("uuid-1"))
				Expect(vm.Firmware).To(Equal("efi"))
				Expect(vm.PowerState).To(Equal("poweredOn"))
				Expect(vm.ConnectionState).To(Equal("connected"))
				Expect(vm.CpuCount).To(Equal(int32(8)))
				Expect(vm.CoresPerSocket).To(Equal(int32(4)))
				Expect(vm.MemoryMB).To(Equal(int32(16384)))
				Expect(vm.DiskSize).To(Equal(int64(153600)))
				Expect(vm.NICCount).To(Equal(int32(3)))
				Expect(vm.Datacenter).To(Equal("dc-1"))
				Expect(vm.Cluster).To(Equal("cluster-a"))
				Expect(vm.HostName).To(Equal("pa-vm-01.local"))
				Expect(vm.IPAddress).To(Equal("10.0.0.5"))
				Expect(vm.IsTemplate).To(BeTrue())
			})

			It("should default the missing columns of a bare VM", func() {
				// Act
				vms, err := s.VM().ListForApplianceDetection(ctx, nil, store.WithDefaultSort())

				// Assert
				Expect(err).NotTo(HaveOccurred())

				bare := findVM(vms, "vm-3")
				Expect(bare.Name).To(Equal("bare-03"))
				Expect(bare.Annotation).To(BeEmpty())
				Expect(bare.Folder).To(BeEmpty())
				Expect(bare.UUID).To(BeEmpty())
				Expect(bare.CpuCount).To(Equal(int32(0)))
				Expect(bare.CoresPerSocket).To(Equal(int32(0)))
				Expect(bare.MemoryMB).To(Equal(int32(0)))
				Expect(bare.DiskSize).To(Equal(int64(0)))
				Expect(bare.NICCount).To(Equal(int32(0)))
				Expect(bare.IsTemplate).To(BeFalse())
			})
		})

		When("the VMs are listed with a filter expression", func() {
			It("should return only the matching VM", func() {
				// Act
				vms, err := s.VM().ListForApplianceDetection(ctx, store.ByFilter("name = 'app-02'"), store.WithDefaultSort())

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(1))
				Expect(vms[0].ID).To(Equal("vm-2"))
			})
		})
	})
})
