package store_test

import (
	"context"
	"database/sql"

	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"

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

		err = migrations.Run(ctx, db)
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
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())
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

		// Given VMs in the database
		// When we list without filters
		// Then it should return all VMs
		It("should return all VMs without filters", func() {
			// Act
			vms, err := s.VM().List(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(5))
		})

		Context("ByClusters", func() {
			// Given VMs in different clusters
			// When we filter by a single cluster
			// Then it should return only VMs in that cluster
			It("should filter by single cluster", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByClusters("cluster-a"))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
				for _, vm := range vms {
					Expect(vm.Cluster).To(Equal("cluster-a"))
				}
			})

			// Given VMs in different clusters
			// When we filter by multiple clusters
			// Then it should return VMs in any of those clusters (OR)
			It("should filter by multiple clusters (OR)", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByClusters("cluster-a", "cluster-b"))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
			})
		})

		Context("ByStatus", func() {
			// Given VMs with different power states
			// When we filter by a single status
			// Then it should return only VMs with that status
			It("should filter by single status", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByStatus("poweredOn"))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
				for _, vm := range vms {
					Expect(vm.PowerState).To(Equal("poweredOn"))
				}
			})

			// Given VMs with different power states
			// When we filter by multiple statuses
			// Then it should return VMs with any of those statuses (OR)
			It("should filter by multiple statuses (OR)", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByStatus("poweredOn", "poweredOff"))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(4))
			})
		})

		Context("ByIssues", func() {
			// Given VMs with different issue counts
			// When we filter by minimum issue count of 2
			// Then it should return only VMs with at least 2 issues
			It("should filter VMs with at least N issues", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByIssues(2))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(1))
				Expect(vms[0].ID).To(Equal("vm-3"))
				Expect(vms[0].IssueCount).To(Equal(2))
			})

			// Given VMs with different issue counts
			// When we filter by minimum issue count of 1
			// Then it should return VMs with at least 1 issue
			It("should filter VMs with at least 1 issue", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByIssues(1))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2)) // vm-3 and vm-5
			})
		})

		Context("ByDiskSizeRange", func() {
			// Given VMs with different disk sizes
			// When we filter by disk size range
			// Then it should return only VMs within that range
			It("should filter by disk size range", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByDiskSizeRange(100, 200))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
				for _, vm := range vms {
					Expect(vm.DiskSize).To(BeNumerically(">=", 100))
					Expect(vm.DiskSize).To(BeNumerically("<", 200))
				}
			})

			// Given VMs with specific disk sizes
			// When we filter by a range that matches no VMs
			// Then it should return empty result
			It("should return empty when no VMs in range", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByDiskSizeRange(1000, 2000))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(BeEmpty())
			})
		})

		Context("ByMemorySizeRange", func() {
			// Given VMs with different memory sizes
			// When we filter by memory size range
			// Then it should return only VMs within that range
			It("should filter by memory size range", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByMemorySizeRange(8000, 20000))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
				for _, vm := range vms {
					Expect(vm.Memory).To(BeNumerically(">=", 8000))
					Expect(vm.Memory).To(BeNumerically("<", 20000))
				}
			})
		})

		Context("WithLimit and WithOffset", func() {
			// Given multiple VMs in the database
			// When we list with a limit
			// Then it should return only that many results
			It("should limit results", func() {
				// Act
				vms, err := s.VM().List(ctx, store.WithLimit(2))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
			})

			// Given multiple VMs in the database
			// When we list with offset and limit
			// Then it should return paginated results
			It("should offset results", func() {
				// Arrange
				firstPage, err := s.VM().List(ctx, store.WithDefaultSort(), store.WithLimit(2))
				Expect(err).NotTo(HaveOccurred())
				Expect(firstPage).To(HaveLen(2))

				// Act
				secondPage, err := s.VM().List(ctx, store.WithDefaultSort(), store.WithOffset(2), store.WithLimit(2))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(secondPage).To(HaveLen(2))
				for _, vm := range secondPage {
					Expect(vm.ID).NotTo(Equal(firstPage[0].ID))
					Expect(vm.ID).NotTo(Equal(firstPage[1].ID))
				}
			})
		})

		Context("WithSort", func() {
			// Given VMs with different names
			// When we sort by name ascending
			// Then results should be ordered alphabetically
			It("should sort by name ascending", func() {
				// Act
				vms, err := s.VM().List(ctx, store.WithSort([]store.SortParam{{Field: "name", Desc: false}}))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(5))
				Expect(vms[0].Name).To(Equal("app-server-1"))
				Expect(vms[1].Name).To(Equal("app-server-2"))
			})

			// Given VMs with different memory sizes
			// When we sort by memory descending
			// Then results should be ordered from highest to lowest memory
			It("should sort by memory descending", func() {
				// Act
				vms, err := s.VM().List(ctx, store.WithSort([]store.SortParam{{Field: "memory", Desc: true}}))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(5))
				Expect(vms[0].Memory).To(Equal(int32(32768)))
			})

			// Given VMs with different issue counts
			// When we sort by issues descending
			// Then results should be ordered from most to least issues
			It("should sort by issues descending", func() {
				// Act
				vms, err := s.VM().List(ctx, store.WithSort([]store.SortParam{{Field: "issues", Desc: true}}))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(5))
				Expect(vms[0].IssueCount).To(Equal(2)) // vm-3 has 2 issues
			})
		})

		Context("combined filters", func() {
			// Given VMs in different clusters with different statuses
			// When we combine cluster and status filters
			// Then it should return VMs matching both conditions (AND)
			It("should combine cluster and status filters (AND)", func() {
				// Act
				vms, err := s.VM().List(ctx,
					store.ByClusters("cluster-a"),
					store.ByStatus("poweredOn"),
				)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
				for _, vm := range vms {
					Expect(vm.Cluster).To(Equal("cluster-a"))
					Expect(vm.PowerState).To(Equal("poweredOn"))
				}
			})

			// Given VMs in different clusters with different memory sizes
			// When we combine cluster and memory range filters
			// Then it should return VMs matching both conditions
			It("should combine cluster and memory range filters", func() {
				// Act
				vms, err := s.VM().List(ctx,
					store.ByClusters("cluster-a"),
					store.ByMemorySizeRange(4000, 10000),
				)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
			})

			// Given VMs with different statuses
			// When we combine status filter with pagination
			// Then it should return paginated filtered results
			It("should combine multiple filters with pagination", func() {
				// Act
				vms, err := s.VM().List(ctx,
					store.ByStatus("poweredOn"),
					store.WithLimit(1),
					store.WithOffset(1),
				)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(1))
			})
		})
	})

	Describe("Count", func() {
		BeforeEach(func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			insertVM("vm-1", "vm1", "poweredOn", "cluster-a", 4096)
			insertVM("vm-2", "vm2", "poweredOn", "cluster-a", 8192)
			insertVM("vm-3", "vm3", "poweredOff", "cluster-b", 16384)

			insertDisk("vm-1", 100)
			insertDisk("vm-2", 200)
			insertDisk("vm-3", 500)
		})

		// Given VMs in the database
		// When we count without filters
		// Then it should return the total count
		It("should count all VMs without filters", func() {
			// Act
			count, err := s.VM().Count(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(3))
		})

		// Given VMs with different statuses
		// When we count with a status filter
		// Then it should return only the count of matching VMs
		It("should count VMs with filter", func() {
			// Act
			count, err := s.VM().Count(ctx, store.ByStatus("poweredOn"))

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(2))
		})
	})
})
