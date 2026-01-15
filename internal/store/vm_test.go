package store_test

import (
	"context"
	"database/sql"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("VMStore", func() {
	var (
		ctx context.Context
		s   *store.Store
		db  *sql.DB
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(":memory:")
		Expect(err).NotTo(HaveOccurred())

		s = store.NewStore(db)
	})

	AfterEach(func() {
		if db != nil {
			db.Close()
		}
	})

	// Helper to insert test data into vinfo table
	insertVM := func(id, name, powerState, cluster string, memory int32) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory")
			VALUES (?, ?, ?, ?, ?)
		`, id, name, powerState, cluster, memory)
		Expect(err).NotTo(HaveOccurred())
	}

	// Helper to insert disk data into vdisk table
	insertDisk := func(vmID string, capacityMiB int64) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO vdisk ("VM ID", "Capacity MiB")
			VALUES (?, ?)
		`, vmID, capacityMiB)
		Expect(err).NotTo(HaveOccurred())
	}

	// Helper to insert concerns for a VM
	insertConcern := func(vmID, concernID, label string) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO concerns ("VM_ID", "Concern_ID", "Label", "Category", "Assessment")
			VALUES (?, ?, ?, 'Warning', 'Needs attention')
		`, vmID, concernID, label)
		Expect(err).NotTo(HaveOccurred())
	}

	Describe("List", func() {
		BeforeEach(func() {
			// Create schema first
			_, err := db.ExecContext(ctx, `
				CREATE TABLE IF NOT EXISTS vinfo (
					"VM ID" VARCHAR,
					"VM" VARCHAR,
					"Powerstate" VARCHAR,
					"Cluster" VARCHAR,
					"Datacenter" VARCHAR,
					"Memory" INTEGER DEFAULT 0
				);
				CREATE TABLE IF NOT EXISTS concerns (
					"VM_ID" VARCHAR,
					"Concern_ID" VARCHAR,
					"Label" VARCHAR,
					"Category" VARCHAR,
					"Assessment" VARCHAR
				);
				CREATE TABLE IF NOT EXISTS vdisk (
					"VM ID" VARCHAR,
					"Capacity MiB" BIGINT DEFAULT 0
				);
			`)
			Expect(err).NotTo(HaveOccurred())

			// Insert test VMs
			insertVM("vm-1", "web-server-1", "poweredOn", "cluster-a", 4096)
			insertVM("vm-2", "web-server-2", "poweredOn", "cluster-a", 8192)
			insertVM("vm-3", "db-server-1", "poweredOff", "cluster-b", 16384)
			insertVM("vm-4", "app-server-1", "poweredOn", "cluster-c", 8192)
			insertVM("vm-5", "app-server-2", "suspended", "cluster-c", 32768)

			// Insert disk data
			insertDisk("vm-1", 100)
			insertDisk("vm-2", 200)
			insertDisk("vm-3", 500)
			insertDisk("vm-4", 150)
			insertDisk("vm-5", 150)

			// Insert some concerns
			insertConcern("vm-3", "concern-1", "High CPU usage")
			insertConcern("vm-3", "concern-2", "Outdated OS")
			insertConcern("vm-5", "concern-3", "Network issue")
		})

		It("should return all VMs without filters", func() {
			vms, err := s.VM().List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(5))
		})

		Context("ByClusters", func() {
			It("should filter by single cluster", func() {
				vms, err := s.VM().List(ctx, store.ByClusters("cluster-a"))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
				for _, vm := range vms {
					Expect(vm.Cluster).To(Equal("cluster-a"))
				}
			})

			It("should filter by multiple clusters (OR)", func() {
				vms, err := s.VM().List(ctx, store.ByClusters("cluster-a", "cluster-b"))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
			})
		})

		Context("ByStatus", func() {
			It("should filter by single status", func() {
				vms, err := s.VM().List(ctx, store.ByStatus("poweredOn"))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
				for _, vm := range vms {
					Expect(vm.PowerState).To(Equal("poweredOn"))
				}
			})

			It("should filter by multiple statuses (OR)", func() {
				vms, err := s.VM().List(ctx, store.ByStatus("poweredOn", "poweredOff"))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(4))
			})
		})

		Context("ByIssues", func() {
			It("should filter VMs with at least N issues", func() {
				vms, err := s.VM().List(ctx, store.ByIssues(2))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(1))
				Expect(vms[0].ID).To(Equal("vm-3"))
				Expect(vms[0].IssueCount).To(Equal(2))
			})

			It("should filter VMs with at least 1 issue", func() {
				vms, err := s.VM().List(ctx, store.ByIssues(1))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2)) // vm-3 and vm-5
			})
		})

		Context("ByDiskSizeRange", func() {
			It("should filter by disk size range", func() {
				vms, err := s.VM().List(ctx, store.ByDiskSizeRange(100, 200))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
				for _, vm := range vms {
					Expect(vm.DiskSize).To(BeNumerically(">=", 100))
					Expect(vm.DiskSize).To(BeNumerically("<", 200))
				}
			})

			It("should return empty when no VMs in range", func() {
				vms, err := s.VM().List(ctx, store.ByDiskSizeRange(1000, 2000))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(BeEmpty())
			})
		})

		Context("ByMemorySizeRange", func() {
			It("should filter by memory size range", func() {
				vms, err := s.VM().List(ctx, store.ByMemorySizeRange(8000, 20000))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
				for _, vm := range vms {
					Expect(vm.Memory).To(BeNumerically(">=", 8000))
					Expect(vm.Memory).To(BeNumerically("<", 20000))
				}
			})
		})

		Context("WithLimit and WithOffset", func() {
			It("should limit results", func() {
				vms, err := s.VM().List(ctx, store.WithLimit(2))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
			})

			It("should offset results", func() {
				firstPage, err := s.VM().List(ctx, store.WithDefaultSort(), store.WithLimit(2))
				Expect(err).NotTo(HaveOccurred())
				Expect(firstPage).To(HaveLen(2))

				secondPage, err := s.VM().List(ctx, store.WithDefaultSort(), store.WithOffset(2), store.WithLimit(2))
				Expect(err).NotTo(HaveOccurred())
				Expect(secondPage).To(HaveLen(2))

				for _, vm := range secondPage {
					Expect(vm.ID).NotTo(Equal(firstPage[0].ID))
					Expect(vm.ID).NotTo(Equal(firstPage[1].ID))
				}
			})
		})

		Context("WithSort", func() {
			It("should sort by name ascending", func() {
				vms, err := s.VM().List(ctx, store.WithSort([]store.SortParam{{Field: "name", Desc: false}}))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(5))
				Expect(vms[0].Name).To(Equal("app-server-1"))
				Expect(vms[1].Name).To(Equal("app-server-2"))
			})

			It("should sort by memory descending", func() {
				vms, err := s.VM().List(ctx, store.WithSort([]store.SortParam{{Field: "memory", Desc: true}}))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(5))
				Expect(vms[0].Memory).To(Equal(int32(32768)))
			})

			It("should sort by issues descending", func() {
				vms, err := s.VM().List(ctx, store.WithSort([]store.SortParam{{Field: "issues", Desc: true}}))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(5))
				Expect(vms[0].IssueCount).To(Equal(2)) // vm-3 has 2 issues
			})
		})

		Context("combined filters", func() {
			It("should combine cluster and status filters (AND)", func() {
				vms, err := s.VM().List(ctx,
					store.ByClusters("cluster-a"),
					store.ByStatus("poweredOn"),
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
				for _, vm := range vms {
					Expect(vm.Cluster).To(Equal("cluster-a"))
					Expect(vm.PowerState).To(Equal("poweredOn"))
				}
			})

			It("should combine cluster and memory range filters", func() {
				vms, err := s.VM().List(ctx,
					store.ByClusters("cluster-a"),
					store.ByMemorySizeRange(4000, 10000),
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
			})

			It("should combine multiple filters with pagination", func() {
				vms, err := s.VM().List(ctx,
					store.ByStatus("poweredOn"),
					store.WithLimit(1),
					store.WithOffset(1),
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(1))
			})
		})
	})

	Describe("Count", func() {
		BeforeEach(func() {
			// Create schema first
			_, err := db.ExecContext(ctx, `
				CREATE TABLE IF NOT EXISTS vinfo (
					"VM ID" VARCHAR,
					"VM" VARCHAR,
					"Powerstate" VARCHAR,
					"Cluster" VARCHAR,
					"Datacenter" VARCHAR,
					"Memory" INTEGER DEFAULT 0
				);
				CREATE TABLE IF NOT EXISTS concerns (
					"VM_ID" VARCHAR,
					"Concern_ID" VARCHAR,
					"Label" VARCHAR,
					"Category" VARCHAR,
					"Assessment" VARCHAR
				);
				CREATE TABLE IF NOT EXISTS vdisk (
					"VM ID" VARCHAR,
					"Capacity MiB" BIGINT DEFAULT 0
				);
			`)
			Expect(err).NotTo(HaveOccurred())

			insertVM("vm-1", "vm1", "poweredOn", "cluster-a", 4096)
			insertVM("vm-2", "vm2", "poweredOn", "cluster-a", 8192)
			insertVM("vm-3", "vm3", "poweredOff", "cluster-b", 16384)

			insertDisk("vm-1", 100)
			insertDisk("vm-2", 200)
			insertDisk("vm-3", 500)
		})

		It("should count all VMs without filters", func() {
			count, err := s.VM().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(3))
		})

		It("should count VMs with filter", func() {
			count, err := s.VM().Count(ctx, store.ByStatus("poweredOn"))
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(2))
		})
	})
})
