package store_test

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
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

		db, err = store.NewConnection(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		s = store.NewStore(db, test.NewMockValidator())

		Expect(s.InitCollection(ctx)).To(Succeed())
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
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

	// Helper to insert VM with folder information
	insertVMWithFolder := func(id, name, folderID, folderName string) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template", "Folder ID", "Folder")
			VALUES (?, ?, 'poweredOn', 'cluster-a', 4096, false, ?, ?)
		`, id, name, folderID, folderName)
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
			vms, err := s.VM().List(ctx, nil)

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
				vms, err := s.VM().List(ctx, store.ByFilter("cluster = 'cluster-a'"))

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
				vms, err := s.VM().List(ctx, store.ByFilter("cluster in ['cluster-a', 'cluster-b']"))

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
				vms, err := s.VM().List(ctx, store.ByFilter("powerstate = 'poweredOn'"))

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
				vms, err := s.VM().List(ctx, store.ByFilter("powerstate in ['poweredOn', 'poweredOff']"))

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
				vms, err := s.VM().List(ctx, store.ByFilter("issues_count >= 2"))

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
				vms, err := s.VM().List(ctx, store.ByFilter("issues_count >= 1"))

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
				vms, err := s.VM().List(ctx, store.ByFilter("total_disk_capacity >= 100 and total_disk_capacity < 200"))

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
				vms, err := s.VM().List(ctx, store.ByFilter("total_disk_capacity >= 1000 and total_disk_capacity < 2000"))

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
				vms, err := s.VM().List(ctx, store.ByFilter("memory >= 8000 and memory < 20000"))

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
				vms, err := s.VM().List(ctx, nil, store.WithLimit(2))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
			})

			// Given multiple VMs in the database
			// When we list with offset and limit
			// Then it should return paginated results
			It("should offset results", func() {
				// Arrange
				firstPage, err := s.VM().List(ctx, nil, store.WithDefaultSort(), store.WithLimit(2))
				Expect(err).NotTo(HaveOccurred())
				Expect(firstPage).To(HaveLen(2))

				// Act
				secondPage, err := s.VM().List(ctx, nil, store.WithDefaultSort(), store.WithOffset(2), store.WithLimit(2))

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
				vms, err := s.VM().List(ctx, nil, store.WithSort([]store.SortParam{{Field: "name", Desc: false}}))

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
				vms, err := s.VM().List(ctx, nil, store.WithSort([]store.SortParam{{Field: "memory", Desc: true}}))

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
				vms, err := s.VM().List(ctx, nil, store.WithSort([]store.SortParam{{Field: "issues", Desc: true}}))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(5))
				Expect(vms[0].IssueCount).To(Equal(2)) // vm-3 has 2 issues
			})

			// Given VMs with known CPU utilization values
			// When we sort by cpuUsage descending
			// Then VMs with highest CPU come first and VMs without data come last
			It("should sort by cpuUsage descending (NULLs last)", func() {
				// IDs are chosen so alphabetical order conflicts with the expected utilization order,
				// ensuring the test fails without implementation (not just coincidentally passes).
				// Alphabetical: vm-cpu-a < vm-cpu-b < vm-cpu-c
				// Expected CPU DESC order: vm-cpu-c (80%) < vm-cpu-b (30%) < vm-cpu-a (NULL)
				insertVM("vm-cpu-a", "cpu-none", "poweredOn", "cluster-a", 2048)
				insertVM("vm-cpu-b", "cpu-low", "poweredOn", "cluster-a", 2048)
				insertVM("vm-cpu-c", "cpu-high", "poweredOn", "cluster-a", 2048)

				_, err := db.ExecContext(ctx, `
					INSERT INTO rightsizing_reports
						(id, vcenter, cluster_id, interval_id, window_start, window_end,
						 expected_sample_count, expected_batch_count, written_batch_count)
					VALUES ('r-sort-cpu', 'vc', '', 1,
						'2024-01-01 00:00:00+00', '2024-01-02 00:00:00+00', 288, 1, 1)
				`)
				Expect(err).NotTo(HaveOccurred())

				// vm-cpu-c: 80%, vm-cpu-b: 30%, vm-cpu-a: no data
				_, err = db.ExecContext(ctx, `
					INSERT INTO rightsizing_vm_utilization (report_id, moid, vm_name, cpu_max_pct)
					VALUES ('r-sort-cpu', 'vm-cpu-c', 'cpu-high', 80.0),
					       ('r-sort-cpu', 'vm-cpu-b', 'cpu-low', 30.0)
				`)
				Expect(err).NotTo(HaveOccurred())

				// Act
				vms, err := s.VM().List(ctx, nil, store.WithSort([]store.SortParam{{Field: "cpuUsage", Desc: true}}))
				Expect(err).NotTo(HaveOccurred())

				// Find positions of the three test VMs
				posHigh, posLow, posNone := -1, -1, -1
				for i, vm := range vms {
					switch vm.ID {
					case "vm-cpu-c":
						posHigh = i
					case "vm-cpu-b":
						posLow = i
					case "vm-cpu-a":
						posNone = i
					}
				}
				Expect(posHigh).NotTo(Equal(-1), "vm-cpu-c not found")
				Expect(posLow).NotTo(Equal(-1), "vm-cpu-b not found")
				Expect(posNone).NotTo(Equal(-1), "vm-cpu-a not found")

				// 80% before 30% before NULL
				Expect(posHigh).To(BeNumerically("<", posLow), "vm-cpu-c (80%%) should come before vm-cpu-b (30%%)")
				Expect(posLow).To(BeNumerically("<", posNone), "vm-cpu-b (30%%) should come before vm-cpu-a (NULL)")
			})

			// Given VMs with known disk utilization values
			// When we sort by diskUsage ascending
			// Then VMs are ordered from lowest to highest disk usage, NULLs last
			It("should sort by diskUsage ascending (NULLs last)", func() {
				// Alphabetical: vm-disk-a < vm-disk-b < vm-disk-c
				// Expected disk ASC order: vm-disk-c (10%) < vm-disk-b (90%) < vm-disk-a (NULL)
				// Conflicts with alphabetical, so test fails without implementation.
				insertVM("vm-disk-a", "disk-none", "poweredOn", "cluster-a", 2048)
				insertVM("vm-disk-b", "disk-high", "poweredOn", "cluster-a", 2048)
				insertVM("vm-disk-c", "disk-low", "poweredOn", "cluster-a", 2048)

				_, err := db.ExecContext(ctx, `
					INSERT INTO rightsizing_reports
						(id, vcenter, cluster_id, interval_id, window_start, window_end,
						 expected_sample_count, expected_batch_count, written_batch_count)
					VALUES ('r-sort-disk', 'vc', '', 1,
						'2024-01-01 00:00:00+00', '2024-01-02 00:00:00+00', 288, 1, 1)
				`)
				Expect(err).NotTo(HaveOccurred())

				// vm-disk-c: 10%, vm-disk-b: 90%, vm-disk-a: no data
				_, err = db.ExecContext(ctx, `
					INSERT INTO rightsizing_vm_utilization (report_id, moid, vm_name, disk_pct)
					VALUES ('r-sort-disk', 'vm-disk-c', 'disk-low', 10.0),
					       ('r-sort-disk', 'vm-disk-b', 'disk-high', 90.0)
				`)
				Expect(err).NotTo(HaveOccurred())

				// Act
				vms, err := s.VM().List(ctx, nil, store.WithSort([]store.SortParam{{Field: "diskUsage", Desc: false}}))
				Expect(err).NotTo(HaveOccurred())

				posLow, posHigh, posNone := -1, -1, -1
				for i, vm := range vms {
					switch vm.ID {
					case "vm-disk-c":
						posLow = i
					case "vm-disk-b":
						posHigh = i
					case "vm-disk-a":
						posNone = i
					}
				}
				Expect(posLow).NotTo(Equal(-1))
				Expect(posHigh).NotTo(Equal(-1))
				Expect(posNone).NotTo(Equal(-1))

				// 10% before 90% before NULL
				Expect(posLow).To(BeNumerically("<", posHigh), "low disk (10%%) should come before high disk (90%%)")
				Expect(posHigh).To(BeNumerically("<", posNone), "high disk (90%%) should come before NULL disk")
			})

			// Given VMs with known memory utilization values
			// When we sort by ramUsage descending
			// Then VMs are ordered from highest to lowest mem usage, NULLs last
			It("should sort by ramUsage descending (NULLs last)", func() {
				// Alphabetical: vm-ram-a < vm-ram-b < vm-ram-c
				// Expected RAM DESC order: vm-ram-c (85%) < vm-ram-b (25%) < vm-ram-a (NULL)
				// Conflicts with alphabetical, so test fails without implementation.
				insertVM("vm-ram-a", "ram-none", "poweredOn", "cluster-a", 2048)
				insertVM("vm-ram-b", "ram-low", "poweredOn", "cluster-a", 2048)
				insertVM("vm-ram-c", "ram-high", "poweredOn", "cluster-a", 2048)

				_, err := db.ExecContext(ctx, `
					INSERT INTO rightsizing_reports
						(id, vcenter, cluster_id, interval_id, window_start, window_end,
						 expected_sample_count, expected_batch_count, written_batch_count)
					VALUES ('r-sort-ram', 'vc', '', 1,
						'2024-01-01 00:00:00+00', '2024-01-02 00:00:00+00', 288, 1, 1)
				`)
				Expect(err).NotTo(HaveOccurred())

				// vm-ram-c: 85%, vm-ram-b: 25%, vm-ram-a: no data
				_, err = db.ExecContext(ctx, `
					INSERT INTO rightsizing_vm_utilization (report_id, moid, vm_name, mem_max_pct)
					VALUES ('r-sort-ram', 'vm-ram-c', 'ram-high', 85.0),
					       ('r-sort-ram', 'vm-ram-b', 'ram-low', 25.0)
				`)
				Expect(err).NotTo(HaveOccurred())

				// Act
				vms, err := s.VM().List(ctx, nil, store.WithSort([]store.SortParam{{Field: "ramUsage", Desc: true}}))
				Expect(err).NotTo(HaveOccurred())

				posHigh, posLow, posNone := -1, -1, -1
				for i, vm := range vms {
					switch vm.ID {
					case "vm-ram-c":
						posHigh = i
					case "vm-ram-b":
						posLow = i
					case "vm-ram-a":
						posNone = i
					}
				}
				Expect(posHigh).NotTo(Equal(-1))
				Expect(posLow).NotTo(Equal(-1))
				Expect(posNone).NotTo(Equal(-1))

				// 85% before 25% before NULL
				Expect(posHigh).To(BeNumerically("<", posLow), "vm-ram-c (85%%) should come before vm-ram-b (25%%)")
				Expect(posLow).To(BeNumerically("<", posNone), "vm-ram-b (25%%) should come before vm-ram-a (NULL)")
			})

			// Given VMs with known cpu_avg_pct values
			// When we sort by cpuAvg descending
			// Then VMs with highest average CPU come first and VMs without data come last
			It("should sort by cpuAvg descending (NULLs last)", func() {
				// Alphabetical: vm-cavg-a < vm-cavg-b < vm-cavg-c
				// Expected cpuAvg DESC: vm-cavg-c (75%) then vm-cavg-b (25%) then vm-cavg-a (NULL)
				// Conflicts with alphabetical — test cannot pass accidentally.
				insertVM("vm-cavg-a", "cavg-none", "poweredOn", "cluster-a", 2048)
				insertVM("vm-cavg-b", "cavg-low", "poweredOn", "cluster-a", 2048)
				insertVM("vm-cavg-c", "cavg-high", "poweredOn", "cluster-a", 2048)

				_, err := db.ExecContext(ctx, `
					INSERT INTO rightsizing_reports
						(id, vcenter, cluster_id, interval_id, window_start, window_end,
						 expected_sample_count, expected_batch_count, written_batch_count)
					VALUES ('r-sort-cavg', 'vc', '', 1,
						'2024-01-01 00:00:00+00', '2024-01-02 00:00:00+00', 288, 1, 1)
				`)
				Expect(err).NotTo(HaveOccurred())

				_, err = db.ExecContext(ctx, `
					INSERT INTO rightsizing_vm_utilization (report_id, moid, vm_name, cpu_avg_pct)
					VALUES ('r-sort-cavg', 'vm-cavg-c', 'cavg-high', 75.0),
					       ('r-sort-cavg', 'vm-cavg-b', 'cavg-low',  25.0)
				`)
				Expect(err).NotTo(HaveOccurred())

				vms, err := s.VM().List(ctx, nil, store.WithSort([]store.SortParam{{Field: "cpuAvg", Desc: true}}))
				Expect(err).NotTo(HaveOccurred())

				posHigh, posLow, posNone := -1, -1, -1
				for i, vm := range vms {
					switch vm.ID {
					case "vm-cavg-c":
						posHigh = i
					case "vm-cavg-b":
						posLow = i
					case "vm-cavg-a":
						posNone = i
					}
				}
				Expect(posHigh).NotTo(Equal(-1), "vm-cavg-c not found")
				Expect(posLow).NotTo(Equal(-1), "vm-cavg-b not found")
				Expect(posNone).NotTo(Equal(-1), "vm-cavg-a not found")

				Expect(posHigh).To(BeNumerically("<", posLow), "75%% should come before 25%%")
				Expect(posLow).To(BeNumerically("<", posNone), "25%% should come before NULL")
			})

			// Given VMs with known mem_avg_pct values
			// When we sort by memAvg ascending
			// Then VMs are ordered from lowest to highest average memory, NULLs last
			It("should sort by memAvg ascending (NULLs last)", func() {
				// Alphabetical: vm-mavg-a < vm-mavg-b < vm-mavg-c
				// Expected memAvg ASC: vm-mavg-c (8%) then vm-mavg-b (92%) then vm-mavg-a (NULL)
				// Conflicts with alphabetical — test cannot pass accidentally.
				insertVM("vm-mavg-a", "mavg-none", "poweredOn", "cluster-a", 2048)
				insertVM("vm-mavg-b", "mavg-high", "poweredOn", "cluster-a", 2048)
				insertVM("vm-mavg-c", "mavg-low", "poweredOn", "cluster-a", 2048)

				_, err := db.ExecContext(ctx, `
					INSERT INTO rightsizing_reports
						(id, vcenter, cluster_id, interval_id, window_start, window_end,
						 expected_sample_count, expected_batch_count, written_batch_count)
					VALUES ('r-sort-mavg', 'vc', '', 1,
						'2024-01-01 00:00:00+00', '2024-01-02 00:00:00+00', 288, 1, 1)
				`)
				Expect(err).NotTo(HaveOccurred())

				_, err = db.ExecContext(ctx, `
					INSERT INTO rightsizing_vm_utilization (report_id, moid, vm_name, mem_avg_pct)
					VALUES ('r-sort-mavg', 'vm-mavg-c', 'mavg-low',  8.0),
					       ('r-sort-mavg', 'vm-mavg-b', 'mavg-high', 92.0)
				`)
				Expect(err).NotTo(HaveOccurred())

				vms, err := s.VM().List(ctx, nil, store.WithSort([]store.SortParam{{Field: "memAvg", Desc: false}}))
				Expect(err).NotTo(HaveOccurred())

				posLow, posHigh, posNone := -1, -1, -1
				for i, vm := range vms {
					switch vm.ID {
					case "vm-mavg-c":
						posLow = i
					case "vm-mavg-b":
						posHigh = i
					case "vm-mavg-a":
						posNone = i
					}
				}
				Expect(posLow).NotTo(Equal(-1), "vm-mavg-c not found")
				Expect(posHigh).NotTo(Equal(-1), "vm-mavg-b not found")
				Expect(posNone).NotTo(Equal(-1), "vm-mavg-a not found")

				Expect(posLow).To(BeNumerically("<", posHigh), "8%% should come before 92%%")
				Expect(posHigh).To(BeNumerically("<", posNone), "92%% should come before NULL")
			})
		})

		Context("combined filters", func() {
			// Given VMs in different clusters with different statuses
			// When we combine cluster and status filters
			// Then it should return VMs matching both conditions (AND)
			It("should combine cluster and status filters (AND)", func() {
				// Act
				vms, err := s.VM().List(ctx,
					store.ByFilter("cluster = 'cluster-a' and powerstate = 'poweredOn'"),
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
					store.ByFilter("cluster = 'cluster-a' and memory >= 4000 and memory < 10000"),
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
					store.ByFilter("powerstate = 'poweredOn'"),
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

			// Given VMs with different issue categories
			// When we list VMs
			// Then VMs with critical concerns should have IsMigratable=false
			It("should set IsMigratable=false for VMs with critical concerns", func() {
				// Act
				vms, err := s.VM().List(ctx, nil, store.WithDefaultSort())

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
				vms, err := s.VM().List(ctx, nil, store.WithDefaultSort())

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
				vms, err := s.VM().List(ctx, nil, store.WithDefaultSort())

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
				vms, err := s.VM().List(ctx, nil, store.WithDefaultSort())

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
				vms, err := s.VM().List(ctx, nil, store.WithDefaultSort())

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
			count, err := s.VM().Count(ctx, nil)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(3))
		})

		// Given VMs with different statuses
		// When we count with a status filter
		// Then it should return only the count of matching VMs
		It("should count VMs with filter", func() {
			// Act
			count, err := s.VM().Count(ctx, store.ByFilter("powerstate = 'poweredOn'"))

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
			Expect(vm.Issues[0].Label).To(Equal("High memory usage"))
			Expect(vm.Issues[1].Label).To(Equal("Outdated VMware Tools"))
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

		// Given a VM with an unknown category
		// When we get it by ID
		// Then the category should be normalized to "Other"
		It("should normalize unknown categories to 'Other'", func() {
			// Arrange - Insert a concern with an unknown category
			_, err := db.ExecContext(ctx, `
				INSERT INTO concerns ("VM_ID", "Concern_ID", "Label", "Category", "Assessment")
				VALUES ('vm-001', 'test.unknown', 'Unknown Category Test', 'UnknownCategory', 'This is a test')
			`)
			Expect(err).NotTo(HaveOccurred())

			// Act
			vm, err := s.VM().Get(ctx, "vm-001")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.Issues).To(HaveLen(1))
			Expect(vm.Issues[0].Category).To(Equal("Other"), "Unknown category should be normalized to 'Other'")
			Expect(vm.Issues[0].Label).To(Equal("Unknown Category Test"))
		})

		// Given a VM with case-variant category names
		// When we get it by ID
		// Then categories should be normalized to proper case
		It("should normalize category case variants", func() {
			// Arrange - Insert concerns with different case variants
			_, err := db.ExecContext(ctx, `
				INSERT INTO concerns ("VM_ID", "Concern_ID", "Label", "Category", "Assessment")
				VALUES
					('vm-002', 'test.lowercase', 'Lowercase Test', 'critical', 'Test'),
					('vm-002', 'test.uppercase', 'Uppercase Test', 'WARNING', 'Test'),
					('vm-002', 'test.mixedcase', 'Mixed Test', 'InFoRmAtIoN', 'Test')
			`)
			Expect(err).NotTo(HaveOccurred())

			// Act
			vm, err := s.VM().Get(ctx, "vm-002")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.Issues).To(HaveLen(3))

			// Find each issue and check its normalized category
			for _, issue := range vm.Issues {
				switch issue.ID {
				case "test.lowercase":
					Expect(issue.Category).To(Equal("Critical"))
				case "test.uppercase":
					Expect(issue.Category).To(Equal("Warning"))
				case "test.mixedcase":
					Expect(issue.Category).To(Equal("Information"))
				}
			}
		})
	})

	Context("GetFolders", func() {
		// Given VMs with different folders
		// When we call GetFolders
		// Then it should return distinct folders ordered by name
		It("should return distinct folders ordered by name", func() {
			// Arrange
			insertVMWithFolder("vm-1", "vm1", "folder-1", "Production")
			insertVMWithFolder("vm-2", "vm2", "folder-2", "Development")
			insertVMWithFolder("vm-3", "vm3", "folder-1", "Production") // duplicate folder
			insertVMWithFolder("vm-4", "vm4", "folder-3", "Testing")

			// Act
			folders, err := s.VM().GetFolders(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(folders).To(HaveLen(3))
			Expect(folders[0].Name).To(Equal("Development"))
			Expect(folders[0].ID).To(Equal("folder-2"))
			Expect(folders[1].Name).To(Equal("Production"))
			Expect(folders[1].ID).To(Equal("folder-1"))
			Expect(folders[2].Name).To(Equal("Testing"))
			Expect(folders[2].ID).To(Equal("folder-3"))
		})

		// Given no VMs in the database
		// When we call GetFolders
		// Then it should return an empty list
		It("should return empty list when no VMs exist", func() {
			// Act
			folders, err := s.VM().GetFolders(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(folders).To(BeEmpty())
		})

		// Given VMs with empty folder values
		// When we call GetFolders
		// Then it should exclude empty folders
		It("should exclude VMs with empty folders", func() {
			// Arrange
			insertVMWithFolder("vm-1", "vm1", "folder-1", "Production")
			insertVMWithFolder("vm-2", "vm2", "", "") // empty folder

			// Act
			folders, err := s.VM().GetFolders(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(folders).To(HaveLen(1))
			Expect(folders[0].Name).To(Equal("Production"))
		})

		// Given VMs with only Folder ID set (no Folder name)
		// When we call GetFolders
		// Then it should return the folder with ID and empty name
		It("should handle VMs with only Folder ID", func() {
			// Arrange
			insertVMWithFolder("vm-1", "vm1", "folder-123", "")

			// Act
			folders, err := s.VM().GetFolders(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(folders).To(HaveLen(1))
			Expect(folders[0].ID).To(Equal("folder-123"))
			Expect(folders[0].Name).To(Equal(""))
		})
	})

	Context("Tags in List output", func() {
		BeforeEach(func() {
			insertVM("vm-1", "web-server", "poweredOn", "cluster-a", 4096)
			insertVM("vm-2", "db-server", "poweredOn", "cluster-a", 8192)
			insertVM("vm-3", "app-server", "poweredOff", "cluster-b", 16384)
		})

		It("should return empty groups when no groups match", func() {
			vms, err := s.VM().List(ctx, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(3))
			for _, vm := range vms {
				Expect(vm.Groups).To(BeEmpty())
			}
		})

		It("should derive groups from group_matches", func() {
			g, err := s.Group().Create(ctx, models.Group{
				Name:   "cluster-a-group",
				Filter: "cluster = 'cluster-a'",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, g.ID)
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(3))

			groupsByID := make(map[string][]string)
			for _, vm := range vms {
				groupsByID[vm.ID] = vm.Groups
			}

			Expect(groupsByID["vm-1"]).To(ConsistOf("cluster-a-group"))
			Expect(groupsByID["vm-2"]).To(ConsistOf("cluster-a-group"))
			Expect(groupsByID["vm-3"]).To(BeEmpty())
		})

		It("should include VM in multiple groups", func() {
			g1, err := s.Group().Create(ctx, models.Group{
				Name:   "cluster-a-group",
				Filter: "cluster = 'cluster-a'",
			})
			Expect(err).NotTo(HaveOccurred())

			g2, err := s.Group().Create(ctx, models.Group{
				Name:   "all-group",
				Filter: "memory > 0",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, g1.ID, g2.ID)
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, nil)
			Expect(err).NotTo(HaveOccurred())

			groupsByID := make(map[string][]string)
			for _, vm := range vms {
				groupsByID[vm.ID] = vm.Groups
			}

			Expect(groupsByID["vm-1"]).To(ConsistOf("cluster-a-group", "all-group"))
			Expect(groupsByID["vm-2"]).To(ConsistOf("cluster-a-group", "all-group"))
			Expect(groupsByID["vm-3"]).To(ConsistOf("all-group"))
		})

		It("should return groups when filter is applied", func() {
			g, err := s.Group().Create(ctx, models.Group{
				Name:   "cluster-a-group",
				Filter: "cluster = 'cluster-a'",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, g.ID)
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, store.ByFilter("cluster = 'cluster-a'"))
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(2))

			for _, vm := range vms {
				Expect(vm.Groups).To(ConsistOf("cluster-a-group"))
			}
		})
	})

	Context("Inspection concerns in List output", func() {
		Context("single inspection result", func() {
			BeforeEach(func() {
				insertVM("vm-insp", "insp-vm", "poweredOn", "cluster-a", 4096)
				concerns := []models.VmInspectionConcern{
					{Category: "disk", Label: "Disk", Msg: "ok"},
					{Category: "network", Label: "Net", Msg: "review"},
				}
				err := s.WithTx(ctx, func(txCtx context.Context) error {
					return s.Inspection().InsertResult(txCtx, "vm-insp", concerns)
				})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should return the concern count for the latest inspection result", func() {
				vms, err := s.VM().List(ctx, nil, store.WithDefaultSort())
				Expect(err).NotTo(HaveOccurred())

				var insp *models.VirtualMachineSummary
				for i := range vms {
					if vms[i].ID == "vm-insp" {
						insp = &vms[i]
						break
					}
				}
				Expect(insp).NotTo(BeNil())
				Expect(insp.InspectionConcernCount).To(Equal(2))
			})
		})

		Context("multiple inspection results", func() {
			It("should use concerns only from the newest result by max inspection_id", func() {
				insertVM("vm-multi", "multi-vm", "poweredOn", "cluster-a", 4096)
				const oldID, newID = 1, 2
				_, err := db.ExecContext(ctx, `
					INSERT INTO vm_inspection_concerns ("VM ID", inspection_id, category, label, msg) VALUES
						('vm-multi', ?, 'stale', 'x', 'from-old'),
						('vm-multi', ?, 'fresh', 'y', 'from-new')
				`, oldID, newID)
				Expect(err).NotTo(HaveOccurred())

				vms, err := s.VM().List(ctx, nil, store.WithDefaultSort())
				Expect(err).NotTo(HaveOccurred())

				var vm *models.VirtualMachineSummary
				for i := range vms {
					if vms[i].ID == "vm-multi" {
						vm = &vms[i]
						break
					}
				}
				Expect(vm).NotTo(BeNil())
				Expect(vm.InspectionConcernCount).To(Equal(1))
			})
		})
	})

	Context("NIC IP addresses", func() {
		BeforeEach(func() {
			_, err := db.ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template")
				VALUES ('vm-ips', 'IP Test VM', 'poweredOn', 'cluster1', 4096, false)
			`)
			Expect(err).NotTo(HaveOccurred())

			_, err = db.ExecContext(ctx, `
				INSERT INTO vnetwork ("VM ID", "Network", "Mac Address", "IPv4 Address", "IPv6 Address")
				VALUES ('vm-ips', 'VM Network', '00:50:56:aa:bb:cc', '10.0.0.5', 'fe80::1')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return IPv4 and IPv6 addresses on each NIC", func() {
			vm, err := s.VM().Get(ctx, "vm-ips")

			Expect(err).NotTo(HaveOccurred())
			Expect(vm.NICs).To(HaveLen(1))
			Expect(vm.NICs[0].IPv4Address).To(Equal("10.0.0.5"))
			Expect(vm.NICs[0].IPv6Address).To(Equal("fe80::1"))
		})

		It("should return empty string for NICs with no IP", func() {
			_, err := db.ExecContext(ctx, `
				INSERT INTO vnetwork ("VM ID", "Network", "Mac Address")
				VALUES ('vm-ips', 'Management', '00:50:56:ff:ff:ff')
			`)
			Expect(err).NotTo(HaveOccurred())

			vm, err := s.VM().Get(ctx, "vm-ips")

			Expect(err).NotTo(HaveOccurred())
			Expect(vm.NICs).To(HaveLen(2))
			noIpNic := vm.NICs[1]
			Expect(noIpNic.IPv4Address).To(Equal(""))
			Expect(noIpNic.IPv6Address).To(Equal(""))
		})
	})
})
