package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2" // nolint:staticcheck
	. "github.com/onsi/gomega"    // nolint:staticcheck

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/test/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e/service"

	"github.com/google/uuid"
)

var _ = Describe("VM endpoint e2e tests", Ordered, func() {
	var agentSvc *service.AgentSvc

	BeforeAll(func() {
		GinkgoWriter.Println("Starting postgres...")
		err := infraManager.StartPostgres()
		Expect(err).ToNot(HaveOccurred(), "failed to start postgres")
		time.Sleep(2 * time.Second)

		GinkgoWriter.Println("Starting vcsim...")
		err = infraManager.StartVcsim()
		Expect(err).ToNot(HaveOccurred(), "failed to start vcsim")

		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
		Eventually(func() error {
			resp, err := client.Get(infra.VcsimURL)
			if err != nil {
				return err
			}
			_ = resp.Body.Close()
			return nil
		}, 30*time.Second, 1*time.Second).Should(BeNil(), "vcsim did not become ready")

		agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)

		agentID := uuid.NewString()
		GinkgoWriter.Printf("Starting agent %s in disconnected mode...\n", agentID)
		_, err = infraManager.StartAgent(infra.AgentConfig{
			AgentID:        agentID,
			SourceID:       uuid.NewString(),
			Mode:           "disconnected",
			ConsoleURL:     cfg.AgentProxyUrl,
			UpdateInterval: "1s",
		})
		Expect(err).ToNot(HaveOccurred(), "failed to start agent")

		Eventually(func() error {
			_, err := agentSvc.Status()
			return err
		}, 30*time.Second, 1*time.Second).Should(BeNil(), "agent did not become ready")

		GinkgoWriter.Println("Starting collector...")
		_, err = agentSvc.StartCollector(infra.VcsimURL, infra.VcsimUsername, infra.VcsimPassword)
		Expect(err).ToNot(HaveOccurred(), "failed to start collector")

		Eventually(func() string {
			status, err := agentSvc.GetCollectorStatus()
			if err != nil {
				return "error"
			}
			GinkgoWriter.Printf("Collector status: %s\n", status.Status)
			return status.Status
		}, 120*time.Second, 2*time.Second).Should(Equal("collected"), "collector did not reach collected state")

		GinkgoWriter.Println("VM endpoint test setup complete — 50 VMs collected")
	})

	AfterAll(func() {
		GinkgoWriter.Println("Cleaning up vm endpoint tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	listAllVMs := func() *v1.VirtualMachineListResponse {
		pageSize := 100
		result, err := agentSvc.ListVMs(&service.VMListParams{PageSize: &pageSize})
		Expect(err).ToNot(HaveOccurred(), "failed to list all VMs")
		return result
	}

	// -----------------------------------------------------------------
	// Basic tests
	// -----------------------------------------------------------------

	// Given an agent that has collected inventory from vcsim with 50 pre-loaded VMs
	// When listing all VMs without any filters
	// Then all 50 VMs should be returned
	It("should list all 50 VMs", func() {
		// Act
		result := listAllVMs()

		// Assert
		GinkgoWriter.Printf("Total VMs: %d, returned: %d\n", result.Total, len(result.Vms))
		Expect(result.Total).To(Equal(50), "expected 50 VMs total")
		Expect(len(result.Vms)).To(Equal(50), "expected 50 VMs in response body")
	})

	// Given an agent with collected inventory
	// When getting a specific VM by its ID
	// Then the VM detail should be returned with populated fields
	It("should get VM details by ID", func() {
		// Arrange
		all := listAllVMs()
		Expect(len(all.Vms)).To(BeNumerically(">", 0))
		vmID := all.Vms[0].Id

		// Act
		GinkgoWriter.Printf("Getting details for VM: %s\n", vmID)
		vm, err := agentSvc.GetVM(vmID)

		// Assert
		Expect(err).ToNot(HaveOccurred(), "failed to get VM details")
		GinkgoWriter.Printf("VM details: name=%s, memory=%d MB, cpus=%d\n",
			vm.Name, vm.MemoryMB, vm.CpuCount)
		Expect(vm.Id).To(Equal(vmID))
		Expect(vm.Name).ToNot(BeEmpty())
		Expect(vm.MemoryMB).To(BeNumerically(">", 0))
		Expect(vm.CpuCount).To(BeNumerically(">", 0))
	})

	// Given an agent with collected inventory
	// When requesting a VM with a non-existent ID
	// Then a not-found error should be returned
	It("should return error for non-existent VM", func() {
		// Act
		_, err := agentSvc.GetVM("non-existent-vm-id")

		// Assert
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not found"))
	})

	// -----------------------------------------------------------------
	// Memory filters
	// -----------------------------------------------------------------
	Context("memory filters", func() {
		// Given 50 VMs distributed across 6 memory tiers (4/8/16/32/64/128 GB)
		// When filtering by minimum memory of 32 GB (32768 MB)
		// Then only VMs with 32/64/128 GB memory should be returned (24 VMs)
		It("should filter by memory minimum", func() {
			// Arrange
			minMemory := int64(32768)

			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				MemorySizeMin: &minMemory,
				PageSize:      &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("VMs with >= 32GB memory: %d\n", result.Total)
			Expect(result.Total).To(Equal(24), "expected 24 VMs with >= 32GB (8+8+8)")
			for _, vm := range result.Vms {
				Expect(vm.Memory).To(BeNumerically(">=", minMemory),
					fmt.Sprintf("VM %s has memory %d MB, expected >= %d", vm.Name, vm.Memory, minMemory))
			}
		})

		// Given 50 VMs distributed across 6 memory tiers
		// When filtering by maximum memory below 16384 MB (exclusive upper bound)
		// Then only VMs with 4 GB and 8 GB memory should be returned (18 VMs)
		It("should filter by memory maximum", func() {
			// Arrange
			maxMemory := int64(16384)

			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				MemorySizeMax: &maxMemory,
				PageSize:      &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("VMs with memory < %d MB: %d\n", maxMemory, result.Total)
			Expect(result.Total).To(Equal(18), "expected 18 VMs with < 16384 MB (9+9)")
			for _, vm := range result.Vms {
				Expect(vm.Memory).To(BeNumerically("<", maxMemory),
					fmt.Sprintf("VM %s has memory %d MB, expected < %d", vm.Name, vm.Memory, maxMemory))
			}
		})

		// Given 50 VMs distributed across 6 memory tiers
		// When filtering by memory range [8192, 8193) to select exactly the 8 GB tier
		// Then only the 9 VMs with exactly 8192 MB should be returned
		It("should filter by exact memory tier", func() {
			// Arrange
			minMemory := int64(8192)
			maxMemory := int64(8193)

			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				MemorySizeMin: &minMemory,
				MemorySizeMax: &maxMemory,
				PageSize:      &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("VMs with exactly 8GB memory: %d\n", result.Total)
			Expect(result.Total).To(Equal(9), "expected 9 VMs with exactly 8192 MB")
			for _, vm := range result.Vms {
				Expect(vm.Memory).To(Equal(int64(8192)),
					fmt.Sprintf("VM %s has memory %d MB, expected exactly 8192", vm.Name, vm.Memory))
			}
		})

		// Given 50 VMs distributed across 6 memory tiers
		// When filtering by memory range [16384, 65537) to capture 16/32/64 GB tiers
		// Then 24 VMs should be returned (8+8+8)
		It("should filter by memory range spanning multiple tiers", func() {
			// Arrange
			minMemory := int64(16384)
			maxMemory := int64(65537)

			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				MemorySizeMin: &minMemory,
				MemorySizeMax: &maxMemory,
				PageSize:      &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("VMs with 16-64GB memory: %d\n", result.Total)
			Expect(result.Total).To(Equal(24), "expected 24 VMs with 16/32/64 GB (8+8+8)")
			for _, vm := range result.Vms {
				Expect(vm.Memory).To(BeNumerically(">=", minMemory))
				Expect(vm.Memory).To(BeNumerically("<", maxMemory))
			}
		})
	})

	// -----------------------------------------------------------------
	// Disk filters
	// -----------------------------------------------------------------
	Context("disk filters", func() {
		// Given 50 VMs with varied disk totals (1-3 disks, 100-825+ GB total)
		// When filtering by minimum disk size of 500 GB (512000 MB)
		// Then only VMs with large total disk should be returned
		It("should filter by disk minimum", func() {
			// Arrange
			minDisk := int64(512000)

			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				DiskSizeMin: &minDisk,
				PageSize:    &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("VMs with >= 500GB disk: %d\n", result.Total)
			Expect(result.Total).To(BeNumerically(">=", 10), "expected at least 10 VMs with >= 500GB total disk")
			for _, vm := range result.Vms {
				Expect(vm.DiskSize).To(BeNumerically(">=", minDisk),
					fmt.Sprintf("VM %s has disk %d MB, expected >= %d", vm.Name, vm.DiskSize, minDisk))
			}
		})

		// Given 50 VMs with varied disk totals
		// When filtering by maximum disk size of 200 GB (204800 MB, exclusive)
		// Then only VMs with small total disk should be returned
		It("should filter by disk maximum", func() {
			// Arrange
			maxDisk := int64(204800)

			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				DiskSizeMax: &maxDisk,
				PageSize:    &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("VMs with disk < 200GB: %d\n", result.Total)
			Expect(result.Total).To(BeNumerically(">=", 5), "expected at least 5 VMs with < 200GB disk")
			for _, vm := range result.Vms {
				Expect(vm.DiskSize).To(BeNumerically("<", maxDisk),
					fmt.Sprintf("VM %s has disk %d MB, expected < %d", vm.Name, vm.DiskSize, maxDisk))
			}
		})

		// Given 50 VMs with varied disk totals
		// When filtering by disk size range [200 GB, 500 GB)
		// Then only VMs within that band should be returned
		It("should filter by disk range", func() {
			// Arrange
			minDisk := int64(204800)
			maxDisk := int64(512001)

			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				DiskSizeMin: &minDisk,
				DiskSizeMax: &maxDisk,
				PageSize:    &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("VMs with 200-500GB disk: %d\n", result.Total)
			Expect(result.Total).To(BeNumerically(">=", 5))
			for _, vm := range result.Vms {
				Expect(vm.DiskSize).To(BeNumerically(">=", minDisk))
				Expect(vm.DiskSize).To(BeNumerically("<", maxDisk))
			}
		})
	})

	// -----------------------------------------------------------------
	// Cluster and status filters
	// -----------------------------------------------------------------
	Context("cluster and status filters", func() {
		// Given 50 VMs split across 2 clusters (25 each: DC0_H0 and DC0_C0)
		// When filtering by the cluster of the first VM
		// Then only the 25 VMs in that cluster should be returned
		It("should filter by cluster", func() {
			// Arrange
			all := listAllVMs()
			Expect(len(all.Vms)).To(BeNumerically(">", 0))
			clusterName := all.Vms[0].Cluster
			GinkgoWriter.Printf("Filtering by cluster: %s\n", clusterName)

			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				Clusters: []string{clusterName},
				PageSize: &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Total).To(Equal(25), "25 VMs per cluster (even/odd host split)")
			for _, vm := range result.Vms {
				Expect(vm.Cluster).To(Equal(clusterName))
			}
		})

		// Given 50 VMs all in poweredOn state
		// When filtering by status "poweredOn"
		// Then all 50 VMs should be returned
		It("should filter by status poweredOn", func() {
			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				Status:   []string{"poweredOn"},
				PageSize: &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("VMs with poweredOn status: %d\n", result.Total)
			Expect(result.Total).To(Equal(50), "all 50 VMs are poweredOn")
			for _, vm := range result.Vms {
				Expect(vm.VCenterState).To(Equal("poweredOn"))
			}
		})

		// Given 50 VMs all in poweredOn state
		// When filtering by status "poweredOff"
		// Then no VMs should be returned
		It("should return empty for non-matching status", func() {
			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				Status:   []string{"poweredOff"},
				PageSize: &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Total).To(Equal(0), "no VMs are poweredOff")
			Expect(result.Vms).To(BeEmpty())
		})
	})

	// -----------------------------------------------------------------
	// Combined filters
	// -----------------------------------------------------------------
	Context("combined filters", func() {
		// Given 50 VMs with varied memory and disk
		// When filtering by both memory min (32 GB) and disk min (300 GB)
		// Then only VMs satisfying both criteria should be returned
		It("should combine memory min and disk min", func() {
			// Arrange
			minMemory := int64(32768)
			minDisk := int64(307200)

			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				MemorySizeMin: &minMemory,
				DiskSizeMin:   &minDisk,
				PageSize:      &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("VMs with >= 32GB memory AND >= 300GB disk: %d\n", result.Total)
			Expect(result.Total).To(BeNumerically(">=", 5))
			for _, vm := range result.Vms {
				Expect(vm.Memory).To(BeNumerically(">=", minMemory),
					fmt.Sprintf("VM %s memory %d < %d", vm.Name, vm.Memory, minMemory))
				Expect(vm.DiskSize).To(BeNumerically(">=", minDisk),
					fmt.Sprintf("VM %s disk %d < %d", vm.Name, vm.DiskSize, minDisk))
			}
		})

		// Given 50 VMs with varied memory and disk
		// When filtering by memory range and disk range simultaneously
		// Then only VMs in the intersection should be returned
		It("should combine memory range and disk range", func() {
			// Arrange
			minMemory := int64(8192)
			maxMemory := int64(32769)
			minDisk := int64(204800)
			maxDisk := int64(614401)

			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				MemorySizeMin: &minMemory,
				MemorySizeMax: &maxMemory,
				DiskSizeMin:   &minDisk,
				DiskSizeMax:   &maxDisk,
				PageSize:      &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("VMs with 8-32GB memory AND 200-600GB disk: %d\n", result.Total)
			Expect(result.Total).To(BeNumerically(">=", 1), "expected at least 1 VM in the intersection")
			for _, vm := range result.Vms {
				Expect(vm.Memory).To(BeNumerically(">=", minMemory))
				Expect(vm.Memory).To(BeNumerically("<", maxMemory))
				Expect(vm.DiskSize).To(BeNumerically(">=", minDisk))
				Expect(vm.DiskSize).To(BeNumerically("<", maxDisk))
			}
		})

		// Given 50 VMs with varied memory and disk
		// When filtering by memory min, sorting by disk descending
		// Then results should satisfy the filter AND be in descending disk order
		It("should combine memory filter with disk sort", func() {
			// Arrange
			minMemory := int64(8192)

			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				MemorySizeMin: &minMemory,
				Sort:          []string{"diskSize:desc"},
				PageSize:      &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("Filtered VMs (memory >= 8GB) sorted by disk desc: %d\n", result.Total)
			Expect(result.Total).To(Equal(41), "expected 41 VMs with >= 8GB memory")
			for _, vm := range result.Vms {
				Expect(vm.Memory).To(BeNumerically(">=", minMemory))
			}
			for i := 1; i < len(result.Vms); i++ {
				Expect(result.Vms[i-1].DiskSize).To(BeNumerically(">=", result.Vms[i].DiskSize),
					"VMs should be sorted by disk size descending")
			}
		})

		// Given 50 VMs with varied memory and disk
		// When combining memory filter, disk filter, sort, and pagination
		// Then the page should contain correctly filtered, sorted results with accurate totals
		It("should combine memory filter, disk filter, sort, and pagination", func() {
			// Arrange
			minMemory := int64(8192)
			minDisk := int64(204800)
			page := 1
			pageSize := 5

			// Act
			result, err := agentSvc.ListVMs(&service.VMListParams{
				MemorySizeMin: &minMemory,
				DiskSizeMin:   &minDisk,
				Sort:          []string{"memory:desc"},
				Page:          &page,
				PageSize:      &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("Full combo: total=%d, page=%d, pageCount=%d, returned=%d\n",
				result.Total, result.Page, result.PageCount, len(result.Vms))
			Expect(result.Page).To(Equal(1))
			Expect(len(result.Vms)).To(Equal(pageSize))
			Expect(result.Total).To(BeNumerically(">=", pageSize), "total should exceed page size")
			Expect(result.PageCount).To(Equal((result.Total + pageSize - 1) / pageSize))
			for _, vm := range result.Vms {
				Expect(vm.Memory).To(BeNumerically(">=", minMemory))
				Expect(vm.DiskSize).To(BeNumerically(">=", minDisk))
			}
			for i := 1; i < len(result.Vms); i++ {
				Expect(result.Vms[i-1].Memory).To(BeNumerically(">=", result.Vms[i].Memory),
					"VMs should be sorted by memory descending")
			}
		})

		// Given 50 VMs
		// When filtering by memory, disk, cluster, and status simultaneously
		// Then all four filters should be applied as an AND
		It("should apply all filter dimensions together", func() {
			// Arrange
			all := listAllVMs()
			clusterName := all.Vms[0].Cluster
			minMemory := int64(16384)
			minDisk := int64(102400)

			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				MemorySizeMin: &minMemory,
				DiskSizeMin:   &minDisk,
				Status:        []string{"poweredOn"},
				Clusters:      []string{clusterName},
				PageSize:      &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("All dimensions combined: %d VMs\n", result.Total)
			Expect(result.Total).To(BeNumerically(">=", 10))
			for _, vm := range result.Vms {
				Expect(vm.Memory).To(BeNumerically(">=", minMemory))
				Expect(vm.DiskSize).To(BeNumerically(">=", minDisk))
				Expect(vm.VCenterState).To(Equal("poweredOn"))
				Expect(vm.Cluster).To(Equal(clusterName))
			}
		})
	})

	// -----------------------------------------------------------------
	// Sorting
	// -----------------------------------------------------------------
	Context("sorting", func() {
		// Given 50 VMs with names test-vm-01 through test-vm-50
		// When sorting by name ascending
		// Then VMs should be in alphabetical order
		It("should sort by name ascending", func() {
			// Act
			result, err := agentSvc.ListVMs(&service.VMListParams{
				Sort: []string{"name:asc"},
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			Expect(len(result.Vms)).To(BeNumerically(">", 1))
			for i := 1; i < len(result.Vms); i++ {
				Expect(result.Vms[i-1].Name <= result.Vms[i].Name).To(BeTrue(),
					fmt.Sprintf("expected %s <= %s", result.Vms[i-1].Name, result.Vms[i].Name))
			}
		})

		// Given 50 VMs with names test-vm-01 through test-vm-50
		// When sorting by name descending
		// Then VMs should be in reverse alphabetical order
		It("should sort by name descending", func() {
			// Act
			result, err := agentSvc.ListVMs(&service.VMListParams{
				Sort: []string{"name:desc"},
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			Expect(len(result.Vms)).To(BeNumerically(">", 1))
			for i := 1; i < len(result.Vms); i++ {
				Expect(result.Vms[i-1].Name >= result.Vms[i].Name).To(BeTrue(),
					fmt.Sprintf("expected %s >= %s", result.Vms[i-1].Name, result.Vms[i].Name))
			}
		})

		// Given 50 VMs with varied memory sizes
		// When sorting by memory descending
		// Then VMs should be in decreasing memory order
		It("should sort by memory descending", func() {
			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				Sort:     []string{"memory:desc"},
				PageSize: &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			Expect(len(result.Vms)).To(Equal(50))
			for i := 1; i < len(result.Vms); i++ {
				Expect(result.Vms[i-1].Memory).To(BeNumerically(">=", result.Vms[i].Memory),
					"VMs should be sorted by memory descending")
			}
		})

		// Given 50 VMs with varied disk sizes
		// When sorting by disk size ascending
		// Then VMs should be in increasing disk size order
		It("should sort by diskSize ascending", func() {
			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				Sort:     []string{"diskSize:asc"},
				PageSize: &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			Expect(len(result.Vms)).To(Equal(50))
			for i := 1; i < len(result.Vms); i++ {
				Expect(result.Vms[i-1].DiskSize).To(BeNumerically("<=", result.Vms[i].DiskSize),
					"VMs should be sorted by disk size ascending")
			}
		})

		// Given 50 VMs with varied memory and names
		// When sorting by memory descending then name ascending (multi-sort)
		// Then VMs with equal memory should be sub-sorted by name
		It("should apply multi-sort memory desc then name asc", func() {
			// Act
			pageSize := 100
			result, err := agentSvc.ListVMs(&service.VMListParams{
				Sort:     []string{"memory:desc", "name:asc"},
				PageSize: &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			Expect(len(result.Vms)).To(Equal(50))
			for i := 1; i < len(result.Vms); i++ {
				prev := result.Vms[i-1]
				curr := result.Vms[i]
				if prev.Memory == curr.Memory {
					Expect(prev.Name <= curr.Name).To(BeTrue(),
						fmt.Sprintf("within same memory tier, expected %s <= %s", prev.Name, curr.Name))
				} else {
					Expect(prev.Memory).To(BeNumerically(">", curr.Memory),
						"memory should be descending across tiers")
				}
			}
		})
	})

	// -----------------------------------------------------------------
	// Pagination
	// -----------------------------------------------------------------
	Context("pagination", func() {
		// Given 50 VMs
		// When requesting page 1 with page size 3
		// Then 3 VMs should be returned with correct pagination metadata
		It("should paginate with correct metadata", func() {
			// Arrange
			page := 1
			pageSize := 3

			// Act
			result, err := agentSvc.ListVMs(&service.VMListParams{
				Page:     &page,
				PageSize: &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("Page %d: %d VMs (total: %d, pages: %d)\n",
				result.Page, len(result.Vms), result.Total, result.PageCount)
			Expect(len(result.Vms)).To(Equal(3))
			Expect(result.Page).To(Equal(1))
			Expect(result.Total).To(Equal(50))
			Expect(result.PageCount).To(Equal(17))
		})

		// Given 50 VMs
		// When requesting pages 1 and 2 with the same page size
		// Then the two pages should contain different VMs
		It("should return different VMs on different pages", func() {
			// Arrange
			page1 := 1
			page2 := 2
			pageSize := 3

			// Act
			result1, err := agentSvc.ListVMs(&service.VMListParams{Page: &page1, PageSize: &pageSize})
			Expect(err).ToNot(HaveOccurred())
			result2, err := agentSvc.ListVMs(&service.VMListParams{Page: &page2, PageSize: &pageSize})
			Expect(err).ToNot(HaveOccurred())

			// Assert
			for _, vm1 := range result1.Vms {
				for _, vm2 := range result2.Vms {
					Expect(vm1.Id).ToNot(Equal(vm2.Id),
						fmt.Sprintf("VM %s appeared on both page 1 and 2", vm1.Name))
				}
			}
		})

		// Given 50 VMs and page size 3 (17 pages, last page has 2 items)
		// When requesting the last page
		// Then only the remainder (2 VMs) should be returned
		It("should return correct remainder on last page", func() {
			// Arrange
			page := 17
			pageSize := 3

			// Act
			result, err := agentSvc.ListVMs(&service.VMListParams{
				Page:     &page,
				PageSize: &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("Last page (%d): %d VMs\n", page, len(result.Vms))
			Expect(len(result.Vms)).To(Equal(2), "last page should have 50 - 16*3 = 2 VMs")
			Expect(result.Page).To(Equal(17))
		})

		// Given 50 VMs
		// When listing without specifying a page size
		// Then the default page size (20) should be applied
		It("should use default page size when not specified", func() {
			// Act
			result, err := agentSvc.ListVMs(nil)

			// Assert
			Expect(err).ToNot(HaveOccurred())
			Expect(len(result.Vms)).To(Equal(20), "default page size is 20")
			Expect(result.Total).To(Equal(50))
			Expect(result.PageCount).To(Equal(3))
		})

		// Given 50 VMs with a filter that matches fewer VMs
		// When paginating the filtered results
		// Then pagination should reflect the filtered total, not all 50
		It("should paginate filtered results correctly", func() {
			// Arrange
			minMemory := int64(131072)
			maxMemory := int64(131073)
			page := 1
			pageSize := 3

			// Act
			result, err := agentSvc.ListVMs(&service.VMListParams{
				MemorySizeMin: &minMemory,
				MemorySizeMax: &maxMemory,
				Page:          &page,
				PageSize:      &pageSize,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("Filtered pagination: total=%d, pageCount=%d, returned=%d\n",
				result.Total, result.PageCount, len(result.Vms))
			Expect(result.Total).To(Equal(8), "exactly 8 VMs have 128GB memory")
			Expect(result.PageCount).To(Equal(3), "ceil(8/3) = 3 pages")
			Expect(len(result.Vms)).To(Equal(3))
		})
	})

	// -----------------------------------------------------------------
	// Edge cases
	// -----------------------------------------------------------------
	Context("edge cases", func() {
		// Given 50 VMs with maximum memory of 128 GB
		// When filtering by minimum memory of 200 GB
		// Then no VMs should be returned
		It("should return empty result for unreachable filter", func() {
			// Arrange
			minMemory := int64(204800)

			// Act
			result, err := agentSvc.ListVMs(&service.VMListParams{
				MemorySizeMin: &minMemory,
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Total).To(Equal(0))
			Expect(result.Vms).To(BeEmpty())
		})

		// Given the API validates that memorySizeMin <= memorySizeMax
		// When sending memorySizeMin > memorySizeMax
		// Then the API should return an error
		It("should reject memory min greater than max", func() {
			// Arrange
			minMemory := int64(65536)
			maxMemory := int64(4096)

			// Act
			_, err := agentSvc.ListVMs(&service.VMListParams{
				MemorySizeMin: &minMemory,
				MemorySizeMax: &maxMemory,
			})

			// Assert
			Expect(err).To(HaveOccurred(), "expected API to reject min > max")
			Expect(err.Error()).To(ContainSubstring("400"))
		})

		// Given the API validates that diskSizeMin <= diskSizeMax
		// When sending diskSizeMin > diskSizeMax
		// Then the API should return an error
		It("should reject disk min greater than max", func() {
			// Arrange
			minDisk := int64(512000)
			maxDisk := int64(102400)

			// Act
			_, err := agentSvc.ListVMs(&service.VMListParams{
				DiskSizeMin: &minDisk,
				DiskSizeMax: &maxDisk,
			})

			// Assert
			Expect(err).To(HaveOccurred(), "expected API to reject min > max")
			Expect(err.Error()).To(ContainSubstring("400"))
		})
	})
})
