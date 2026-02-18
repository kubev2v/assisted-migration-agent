package store_test

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
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

		s = store.NewStore(db, test.NewMockValidator())

		err = s.Migrate(ctx)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if db != nil {
			db.Close()
		}
	})

	// Helper to insert test data into vinfo table
	insertVM := func(id, name, powerState, cluster string, memory int32) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template")
			VALUES (?, ?, ?, ?, ?, false)
		`, id, name, powerState, cluster, memory)
		Expect(err).NotTo(HaveOccurred())
	}

	// Helper to insert VM with template flag
	insertVMWithTemplate := func(id, name, powerState, cluster string, memory int32, isTemplate bool) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template")
			VALUES (?, ?, ?, ?, ?, ?)
		`, id, name, powerState, cluster, memory, isTemplate)
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
	insertConcern := func(vmID, concernID, label, category string) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO concerns ("VM_ID", "Concern_ID", "Label", "Category", "Assessment")
			VALUES (?, ?, ?, ?, 'Needs attention')
		`, vmID, concernID, label, category)
		Expect(err).NotTo(HaveOccurred())
	}

	Context("List", func() {
		BeforeEach(func() {
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
			insertConcern("vm-3", "concern-1", "High CPU usage", "Warning")
			insertConcern("vm-3", "concern-2", "Outdated OS", "Warning")
			insertConcern("vm-5", "concern-3", "Network issue", "Warning")
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

		Context("IsMigratable", func() {
			BeforeEach(func() {
				// Add critical concern to vm-3, making it non-migratable
				insertConcern("vm-3", "concern-critical", "RDM disk detected", "Critical")
			})

			// Given VMs with different concern categories
			// When we list VMs
			// Then VMs with critical concerns should have IsMigratable=false
			It("should set IsMigratable=false for VMs with critical concerns", func() {
				// Act
				vms, err := s.VM().List(ctx, store.WithDefaultSort())

				// Assert
				Expect(err).NotTo(HaveOccurred())

				// Find vm-3 which has a critical concern
				var vm3Found bool
				for _, vm := range vms {
					if vm.ID == "vm-3" {
						vm3Found = true
						Expect(vm.IsMigratable).To(BeFalse(), "vm-3 should not be migratable due to critical concern")
					}
				}
				Expect(vm3Found).To(BeTrue(), "vm-3 should be in the list")
			})

			// Given VMs with only warning concerns
			// When we list VMs
			// Then VMs with only warning concerns should have IsMigratable=true
			It("should set IsMigratable=true for VMs with only warning concerns", func() {
				// Act
				vms, err := s.VM().List(ctx, store.WithDefaultSort())

				// Assert
				Expect(err).NotTo(HaveOccurred())

				// Find vm-5 which has only warning concerns
				var vm5Found bool
				for _, vm := range vms {
					if vm.ID == "vm-5" {
						vm5Found = true
						Expect(vm.IsMigratable).To(BeTrue(), "vm-5 should be migratable (only warning concerns)")
					}
				}
				Expect(vm5Found).To(BeTrue(), "vm-5 should be in the list")
			})

			// Given VMs with no concerns
			// When we list VMs
			// Then VMs with no concerns should have IsMigratable=true
			It("should set IsMigratable=true for VMs with no concerns", func() {
				// Act
				vms, err := s.VM().List(ctx, store.WithDefaultSort())

				// Assert
				Expect(err).NotTo(HaveOccurred())

				// Find vm-1 which has no concerns
				var vm1Found bool
				for _, vm := range vms {
					if vm.ID == "vm-1" {
						vm1Found = true
						Expect(vm.IsMigratable).To(BeTrue(), "vm-1 should be migratable (no concerns)")
					}
				}
				Expect(vm1Found).To(BeTrue(), "vm-1 should be in the list")
			})
		})

		Context("IsTemplate", func() {
			BeforeEach(func() {
				// Insert a template VM
				insertVMWithTemplate("vm-template", "template-server", "poweredOff", "cluster-a", 2048, true)
			})

			// Given VMs including templates
			// When we list VMs
			// Then template VMs should have IsTemplate=true
			It("should set IsTemplate=true for template VMs", func() {
				// Act
				vms, err := s.VM().List(ctx, store.WithDefaultSort())

				// Assert
				Expect(err).NotTo(HaveOccurred())

				// Find the template VM
				var templateFound bool
				for _, vm := range vms {
					if vm.ID == "vm-template" {
						templateFound = true
						Expect(vm.IsTemplate).To(BeTrue(), "vm-template should be marked as template")
					}
				}
				Expect(templateFound).To(BeTrue(), "vm-template should be in the list")
			})

			// Given regular VMs
			// When we list VMs
			// Then regular VMs should have IsTemplate=false
			It("should set IsTemplate=false for regular VMs", func() {
				// Act
				vms, err := s.VM().List(ctx, store.WithDefaultSort())

				// Assert
				Expect(err).NotTo(HaveOccurred())

				// Check that regular VMs are not templates
				for _, vm := range vms {
					if vm.ID != "vm-template" {
						Expect(vm.IsTemplate).To(BeFalse(), "%s should not be marked as template", vm.ID)
					}
				}
			})
		})
	})

	Context("Count", func() {
		BeforeEach(func() {
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

	Context("Get", func() {
		BeforeEach(func() {
			err := test.InsertVMs(ctx, db)
			Expect(err).NotTo(HaveOccurred())
		})

		// Given a VM exists in the database
		// When we get it by ID
		// Then it should return full VM details
		It("should return full VM details by ID", func() {
			// Act
			vm, err := s.VM().Get(ctx, "vm-003")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm).NotTo(BeNil())
			Expect(vm.ID).To(Equal("vm-003"))
			Expect(vm.Name).To(Equal("db-server-1"))
			Expect(vm.PowerState).To(Equal("poweredOn"))
			Expect(vm.Cluster).To(Equal("production"))
			Expect(vm.MemoryMB).To(Equal(int32(16384)))
			Expect(vm.Firmware).To(Equal("efi"))
		})

		// Given a VM ID that does not exist
		// When we get it by ID
		// Then it should return ResourceNotFoundError
		It("should return ResourceNotFoundError for non-existent ID", func() {
			// Act
			_, err := s.VM().Get(ctx, "non-existent")

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		// Given a VM with disks, NICs, and concerns
		// When we get it by ID
		// Then it should return correct disks, NICs, and issues
		It("should return correct disks, NICs, and issues from parser", func() {
			// Act - vm-003 has 2 disks, 2 NICs, and 2 concerns
			vm, err := s.VM().Get(ctx, "vm-003")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.Disks).To(HaveLen(2))
			Expect(vm.DiskSize).To(Equal(int64(500 + 500)))
			Expect(vm.NICs).To(HaveLen(2))
			Expect(vm.Issues).To(HaveLen(2))
			Expect(vm.Issues).To(ContainElement("High memory usage"))
			Expect(vm.Issues).To(ContainElement("Outdated VMware Tools"))
		})

		// Given a VM with only warning concerns
		// When we get it by ID
		// Then it should have IsMigratable=true
		It("should set IsMigratable=true for VMs with only warning concerns", func() {
			// Act - vm-003 has only warning concerns
			vm, err := s.VM().Get(ctx, "vm-003")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.IsMigratable).To(BeTrue(), "vm-003 should be migratable (only warning concerns)")
		})

		// Given a VM with critical concerns
		// When we get it by ID
		// Then it should have IsMigratable=false
		It("should set IsMigratable=false for VMs with critical concerns", func() {
			// Act - vm-007 has a critical concern (RDM disk detected)
			vm, err := s.VM().Get(ctx, "vm-007")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.IsMigratable).To(BeFalse(), "vm-007 should not be migratable (has critical concern)")
		})

		// Given a VM with no concerns
		// When we get it by ID
		// Then it should have IsMigratable=true
		It("should set IsMigratable=true for VMs with no concerns", func() {
			// Act - vm-001 has no concerns
			vm, err := s.VM().Get(ctx, "vm-001")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.IsMigratable).To(BeTrue(), "vm-001 should be migratable (no concerns)")
		})

		// Given a template VM
		// When we get it by ID
		// Then it should have IsTemplate=true
		It("should return IsTemplate=true for template VMs", func() {
			// Act - vm-010 is a template
			vm, err := s.VM().Get(ctx, "vm-010")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.IsTemplate).To(BeTrue(), "vm-010 should be marked as template")
		})
	})
})
