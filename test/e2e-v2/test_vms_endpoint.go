package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gm "github.com/onsi/gomega"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e-v2/service"

	"github.com/google/uuid"
)

var _ = ginkgo.Describe("VM endpoint v2 e2e tests", ginkgo.Ordered, func() {
	var (
		agentSvc     *service.AgentSvc
		collectionID string
	)

	ginkgo.BeforeAll(func() {
		ginkgo.GinkgoWriter.Println("Starting postgres...")
		err := infraManager.StartPostgres()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start postgres")
		time.Sleep(2 * time.Second)

		ginkgo.GinkgoWriter.Println("Starting vcsim...")
		err = infraManager.StartVcsim()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start vcsim")

		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
		gm.Eventually(func() error {
			resp, err := client.Get(infra.VcsimURL)
			if err != nil {
				return err
			}
			_ = resp.Body.Close()
			return nil
		}, 30*time.Second, 1*time.Second).Should(gm.BeNil(), "vcsim did not become ready")

		agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)

		agentID := uuid.NewString()
		ginkgo.GinkgoWriter.Printf("Starting agent %s in disconnected mode (v2)...\n", agentID)
		_, err = infraManager.StartAgent(infra.AgentConfig{
			AgentID:        agentID,
			SourceID:       uuid.NewString(),
			Mode:           "disconnected",
			ConsoleURL:     cfg.AgentProxyUrl,
			UpdateInterval: "1s",
			APIVersion:     "v2",
		})
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")

		gm.Eventually(func() error {
			_, err := agentSvc.Status()
			return err
		}, 30*time.Second, 1*time.Second).Should(gm.BeNil(), "agent did not become ready")

		ginkgo.GinkgoWriter.Println("Storing credentials...")
		_, err = agentSvc.StoreCredentials(infra.VcsimURL, infra.VcsimUsername, infra.VcsimPassword)
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to store credentials")

		ginkgo.GinkgoWriter.Println("Starting collector...")
		_, err = agentSvc.StartCollector()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

		// Wait for a collection to appear in the pool
		gm.Eventually(func() int {
			collections, err := agentSvc.ListCollections()
			if err != nil {
				return 0
			}
			ginkgo.GinkgoWriter.Printf("Collections: %d\n", len(collections.Collections))
			return len(collections.Collections)
		}, 120*time.Second, 2*time.Second).Should(gm.BeNumerically(">", 0), "expected at least 1 collection")

		collections, err := agentSvc.ListCollections()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to list collections")
		collectionID = collections.Collections[0].Id
		ginkgo.GinkgoWriter.Printf("Using collection: %s\n", collectionID)

		ginkgo.GinkgoWriter.Println("VM endpoint v2 test setup complete")
	})

	ginkgo.AfterAll(func() {
		ginkgo.GinkgoWriter.Println("Cleaning up vm endpoint v2 tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	listAllVMs := func() *v2.VirtualMachineListResponse {
		pageSize := 100
		result, err := agentSvc.ListLatestVMs(&service.VMListParams{PageSize: &pageSize})
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to list all VMs")
		return result
	}

	// Given an agent that has collected inventory from vcsim with 50 pre-loaded VMs
	// When listing all VMs without any filters
	// Then all 50 VMs should be returned
	ginkgo.It("should list all 50 VMs", func() {
		result := listAllVMs()

		ginkgo.GinkgoWriter.Printf("Total VMs: %d, returned: %d\n", result.Total, len(result.VirtualMachines))
		gm.Expect(result.Total).To(gm.Equal(50), "expected 50 VMs total")
		gm.Expect(len(result.VirtualMachines)).To(gm.Equal(50), "expected 50 VMs in response body")
	})

	// Given an agent with collected inventory
	// When getting a specific VM by its ID
	// Then the VM detail should be returned with populated fields
	ginkgo.It("should get VM details by ID", func() {
		all := listAllVMs()
		gm.Expect(len(all.VirtualMachines)).To(gm.BeNumerically(">", 0))
		vmID := all.VirtualMachines[0].Id

		ginkgo.GinkgoWriter.Printf("Getting details for VM: %s\n", vmID)
		vm, err := agentSvc.GetVM(collectionID, vmID)

		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get VM details")
		ginkgo.GinkgoWriter.Printf("VM details: name=%s, memory=%d MB, cpus=%d\n",
			vm.Name, vm.MemoryMB, vm.CpuCount)
		gm.Expect(vm.Id).To(gm.Equal(vmID))
		gm.Expect(vm.Name).ToNot(gm.BeEmpty())
		gm.Expect(vm.MemoryMB).To(gm.BeNumerically(">", 0))
		gm.Expect(vm.CpuCount).To(gm.BeNumerically(">", 0))
	})

	// Given an agent with collected inventory
	// When filtering VMs by memory >= 32GB using byExpression
	// Then only VMs with at least 32768 MB of memory should be returned
	ginkgo.It("should filter by memory", func() {
		expr := "memory >= 32GB"

		pageSize := 100
		result, err := agentSvc.ListVMs(collectionID, &service.VMListParams{
			ByExpression: &expr,
			PageSize:     &pageSize,
		})

		gm.Expect(err).ToNot(gm.HaveOccurred())
		ginkgo.GinkgoWriter.Printf("VMs with >= 32GB memory: %d\n", result.Total)
		gm.Expect(result.Total).To(gm.BeNumerically(">", 0), "expected at least some VMs with >= 32GB memory")
		for _, vm := range result.VirtualMachines {
			gm.Expect(vm.Memory).To(gm.BeNumerically(">=", 32768),
				fmt.Sprintf("VM %s has memory %d MB, expected >= 32768", vm.Name, vm.Memory))
		}
	})

	// Given an agent with collected inventory
	// When sorting VMs by name ascending
	// Then VMs should be returned in alphabetical order
	ginkgo.It("should sort by name ascending", func() {
		pageSize := 100
		result, err := agentSvc.ListVMs(collectionID, &service.VMListParams{
			Sort:     []string{"name:asc"},
			PageSize: &pageSize,
		})

		gm.Expect(err).ToNot(gm.HaveOccurred())
		gm.Expect(len(result.VirtualMachines)).To(gm.BeNumerically(">", 1))
		for i := 1; i < len(result.VirtualMachines); i++ {
			gm.Expect(result.VirtualMachines[i-1].Name <= result.VirtualMachines[i].Name).To(gm.BeTrue(),
				fmt.Sprintf("expected %s <= %s", result.VirtualMachines[i-1].Name, result.VirtualMachines[i].Name))
		}
	})

	// Given an agent with collected inventory
	// When requesting page 1 with page size 3
	// Then 3 VMs should be returned with correct pagination metadata
	ginkgo.It("should paginate correctly", func() {
		page := 1
		pageSize := 3

		result, err := agentSvc.ListVMs(collectionID, &service.VMListParams{
			Page:     &page,
			PageSize: &pageSize,
		})

		gm.Expect(err).ToNot(gm.HaveOccurred())
		ginkgo.GinkgoWriter.Printf("Page %d: %d VMs (total: %d, pages: %d)\n",
			result.Page, len(result.VirtualMachines), result.Total, result.PageCount)
		gm.Expect(len(result.VirtualMachines)).To(gm.Equal(3))
		gm.Expect(result.Page).To(gm.Equal(1))
		gm.Expect(result.Total).To(gm.Equal(50))
		gm.Expect(result.PageCount).To(gm.Equal(17))
	})

	// Given an agent with collected inventory
	// When requesting a VM with a non-existent ID
	// Then a not-found error should be returned
	ginkgo.It("should return empty for non-existent VM", func() {
		_, err := agentSvc.GetVM(collectionID, "non-existent-vm-id")

		gm.Expect(err).To(gm.HaveOccurred())
		gm.Expect(err.Error()).To(gm.ContainSubstring("not found"))
	})

	// -----------------------------------------------------------------
	// Memory filters (using byExpression)
	// -----------------------------------------------------------------
	ginkgo.Context("memory filters", func() {
		// Given 50 VMs distributed across 6 memory tiers (4/8/16/32/64/128 GB)
		// When filtering by minimum memory of 32 GB (32768 MB)
		// Then only VMs with 32/64/128 GB memory should be returned (24 VMs)
		ginkgo.It("should filter by memory minimum using byExpression", func() {
			expr := "memory >= 32GB"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with >= 32GB memory: %d\n", result.Total)
			gm.Expect(result.Total).To(gm.Equal(24), "expected 24 VMs with >= 32GB (8+8+8)")
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Memory).To(gm.BeNumerically(">=", 32768),
					fmt.Sprintf("VM %s has memory %d MB, expected >= 32768", vm.Name, vm.Memory))
			}
		})

		// Given 50 VMs distributed across 6 memory tiers
		// When filtering by maximum memory of 16384 MB (inclusive upper bound)
		// Then VMs with 4 GB, 8 GB, and 16 GB memory should be returned (26 VMs)
		ginkgo.It("should filter by memory maximum using byExpression", func() {
			expr := "memory <= 16GB"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with memory <= 16GB MB: %d\n", result.Total)
			gm.Expect(result.Total).To(gm.Equal(26), "expected 26 VMs with <= 16384 MB (9+9+8)")
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Memory).To(gm.BeNumerically("<=", 16384),
					fmt.Sprintf("VM %s has memory %d MB, expected <= 16384", vm.Name, vm.Memory))
			}
		})

		// Given 50 VMs distributed across 6 memory tiers
		// When filtering by memory range [8192, 8192] to select exactly the 8 GB tier
		// Then only the 9 VMs with exactly 8192 MB should be returned
		ginkgo.It("should filter by exact memory tier using byExpression", func() {
			expr := "memory = 8GB"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with exactly 8GB memory: %d\n", result.Total)
			gm.Expect(result.Total).To(gm.Equal(9), "expected 9 VMs with exactly 8192 MB")
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Memory).To(gm.Equal(int64(8192)),
					fmt.Sprintf("VM %s has memory %d MB, expected exactly 8192", vm.Name, vm.Memory))
			}
		})

		// Given 50 VMs distributed across 6 memory tiers
		// When filtering by memory range [16384, 65536] to capture 16/32/64 GB tiers
		// Then 24 VMs should be returned (8+8+8)
		ginkgo.It("should filter by memory range spanning multiple tiers using byExpression", func() {
			expr := "memory >= 16GB and memory <= 64GB"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with 16-64GB memory: %d\n", result.Total)
			gm.Expect(result.Total).To(gm.Equal(24), "expected 24 VMs with 16/32/64 GB (8+8+8)")
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Memory).To(gm.BeNumerically(">=", 16384))
				gm.Expect(vm.Memory).To(gm.BeNumerically("<=", 65536))
			}
		})
	})

	// -----------------------------------------------------------------
	// Disk filters (using byExpression)
	// -----------------------------------------------------------------
	ginkgo.Context("disk filters", func() {
		// Given 50 VMs with varied disk totals (1-3 disks, 100-825+ GB total)
		// When filtering by minimum total disk size of 500 GB (512000 MiB)
		// Then only VMs with large total disk should be returned
		ginkgo.It("should filter by total disk minimum using byExpression", func() {
			expr := "total_disk_capacity >= 500GB"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with >= 500GB total disk: %d\n", result.Total)
			gm.Expect(result.Total).To(gm.BeNumerically(">=", 10), "expected at least 10 VMs with >= 500GB total disk")
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.DiskSize).To(gm.BeNumerically(">=", 512000),
					fmt.Sprintf("VM %s has disk %d MiB, expected >= 512000", vm.Name, vm.DiskSize))
			}
		})

		// Given 50 VMs with varied disk totals
		// When filtering by maximum total disk size of 200 GB (204800 MiB, exclusive)
		// Then only VMs with small total disk should be returned
		ginkgo.It("should filter by total disk maximum using byExpression", func() {
			expr := "total_disk_capacity < 200GB"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with total disk < 200GB: %d\n", result.Total)
			gm.Expect(result.Total).To(gm.BeNumerically(">=", 5), "expected at least 5 VMs with < 200GB total disk")
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.DiskSize).To(gm.BeNumerically("<", 204800),
					fmt.Sprintf("VM %s has disk %d MiB, expected < 204800", vm.Name, vm.DiskSize))
			}
		})

		// Given 50 VMs with varied disk totals
		// When filtering by total disk size range [200 GB, 500 GB)
		// Then only VMs within that band should be returned
		ginkgo.It("should filter by total disk range using byExpression", func() {
			expr := "total_disk_capacity >= 200GB and total_disk_capacity < 500GB"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with 200-500GB total disk: %d\n", result.Total)
			gm.Expect(result.Total).To(gm.BeNumerically(">=", 5))
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.DiskSize).To(gm.BeNumerically(">=", 204800))
				gm.Expect(vm.DiskSize).To(gm.BeNumerically("<", 512001))
			}
		})
	})

	// -----------------------------------------------------------------
	// Individual disk capacity filters (using byExpression)
	// -----------------------------------------------------------------
	ginkgo.Context("individual disk capacity filters", func() {
		// Given VMs with multiple disks of varying sizes
		// When filtering by individual disk capacity >= 100GB
		// Then VMs with at least one disk meeting the criteria should be returned
		ginkgo.It("should filter by individual disk capacity minimum using byExpression", func() {
			expr := "disk.capacity >= 100GB"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with at least one disk >= 100GB: %d\n", result.Total)
			gm.Expect(result.Total).To(gm.BeNumerically(">=", 1), "expected at least 1 VM with a disk >= 100GB")
		})

		// Given VMs with multiple disks of varying sizes
		// When filtering by individual disk capacity < 150GB
		// Then VMs with at least one disk below the threshold should be returned
		ginkgo.It("should filter by individual disk capacity maximum using byExpression", func() {
			expr := "disk.capacity < 150GB"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with at least one disk < 150GB: %d\n", result.Total)
			gm.Expect(result.Total).To(gm.BeNumerically(">=", 1), "expected at least 1 VM with a disk < 150GB")
		})

		// Given VMs with multiple disks of varying sizes
		// When filtering by individual disk capacity in a range
		// Then VMs with at least one disk in that range should be returned
		ginkgo.It("should filter by individual disk capacity range using byExpression", func() {
			expr := "disk.capacity >= 100GB and disk.capacity <= 200GB"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with at least one disk in 100-200GB range: %d\n", result.Total)
			gm.Expect(result.Total).To(gm.BeNumerically(">=", 1), "expected at least 1 VM with a disk in 100-200GB range")
		})

		// Given VMs with multiple disks
		// When combining individual disk filter with other filters
		// Then all filters should be applied together
		ginkgo.It("should combine individual disk capacity with memory filter using byExpression", func() {
			expr := "disk.capacity >= 100GB and memory >= 16GB"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with disk >= 100GB AND memory >= 16GB: %d\n", result.Total)
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Memory).To(gm.BeNumerically(">=", 16384),
					fmt.Sprintf("VM %s has %d MB memory, expected >= 16384", vm.Name, vm.Memory))
			}
		})
	})

	// -----------------------------------------------------------------
	// Issues count filters (using byExpression)
	// -----------------------------------------------------------------
	ginkgo.Context("issues count filters", func() {
		// Given VMs with varying numbers of issues/concerns
		// When filtering by minimum issues count
		// Then only VMs with at least that many issues should be returned
		ginkgo.It("should filter by minimum issues count using byExpression", func() {
			expr := "issues_count >= 1"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with >= 1 issues: %d\n", result.Total)
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.IssueCount).To(gm.BeNumerically(">=", 1),
					fmt.Sprintf("VM %s has %d issues, expected >= 1", vm.Name, vm.IssueCount))
			}
		})

		// Given VMs with varying numbers of issues/concerns
		// When filtering by issues count greater than a threshold
		// Then only VMs exceeding that threshold should be returned
		ginkgo.It("should filter by issues count greater than threshold using byExpression", func() {
			expr := "issues_count > 2"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with > 2 issues: %d\n", result.Total)
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.IssueCount).To(gm.BeNumerically(">", 2),
					fmt.Sprintf("VM %s has %d issues, expected > 2", vm.Name, vm.IssueCount))
			}
		})

		// Given VMs with varying numbers of issues/concerns
		// When filtering by exact issues count
		// Then only VMs with exactly that count should be returned
		ginkgo.It("should filter by exact issues count using byExpression", func() {
			expr := "issues_count = 0"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with exactly 0 issues: %d\n", result.Total)
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.IssueCount).To(gm.Equal(0),
					fmt.Sprintf("VM %s has %d issues, expected exactly 0", vm.Name, vm.IssueCount))
			}
		})

		// Given VMs with varying numbers of issues/concerns
		// When combining issues count filter with other filters
		// Then all filters should be applied together
		ginkgo.It("should combine issues count with memory filter using byExpression", func() {
			expr := "issues_count >= 1 and memory >= 8GB"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with >= 1 issues AND >= 8GB memory: %d\n", result.Total)
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.IssueCount).To(gm.BeNumerically(">=", 1),
					fmt.Sprintf("VM %s has %d issues, expected >= 1", vm.Name, vm.IssueCount))
				gm.Expect(vm.Memory).To(gm.BeNumerically(">=", 8192),
					fmt.Sprintf("VM %s has %d MB memory, expected >= 8192", vm.Name, vm.Memory))
			}
		})
	})

	// -----------------------------------------------------------------
	// Cluster and status filters (using byExpression)
	// -----------------------------------------------------------------
	ginkgo.Context("cluster and status filters", func() {
		// Given 50 VMs split across 2 clusters (25 each: DC0_H0 and DC0_C0)
		// When filtering by the cluster of the first VM
		// Then only the 25 VMs in that cluster should be returned
		ginkgo.It("should filter by cluster using byExpression", func() {
			all := listAllVMs()
			gm.Expect(len(all.VirtualMachines)).To(gm.BeNumerically(">", 0))
			clusterName := all.VirtualMachines[0].Cluster
			ginkgo.GinkgoWriter.Printf("Filtering by cluster: %s\n", clusterName)
			expr := fmt.Sprintf("cluster = '%s'", clusterName)

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.Total).To(gm.Equal(25), "25 VMs per cluster (even/odd host split)")
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Cluster).To(gm.Equal(clusterName))
			}
		})

		// Given 50 VMs all in poweredOn state
		// When filtering by status "poweredOn"
		// Then all 50 VMs should be returned
		ginkgo.It("should filter by status poweredOn using byExpression", func() {
			expr := "powerstate = 'poweredOn'"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with poweredOn status: %d\n", result.Total)
			gm.Expect(result.Total).To(gm.Equal(50), "all 50 VMs are poweredOn")
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.VCenterState).To(gm.Equal("poweredOn"))
			}
		})

		// Given 50 VMs all in poweredOn state
		// When filtering by status "poweredOff"
		// Then no VMs should be returned
		ginkgo.It("should return empty for non-matching status using byExpression", func() {
			expr := "powerstate = 'poweredOff'"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.Total).To(gm.Equal(0), "no VMs are poweredOff")
			gm.Expect(result.VirtualMachines).To(gm.BeEmpty())
		})
	})

	// -----------------------------------------------------------------
	// Combined filters (using byExpression)
	// -----------------------------------------------------------------
	ginkgo.Context("combined filters", func() {
		// Given 50 VMs with varied memory and disk
		// When filtering by both memory min (32 GB) and total disk min (300 GB)
		// Then only VMs satisfying both criteria should be returned
		ginkgo.It("should combine memory min and total disk min using byExpression", func() {
			expr := "memory >= 32GB and total_disk_capacity >= 300GB"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with >= 32GB memory AND >= 300GB total disk: %d\n", result.Total)
			gm.Expect(result.Total).To(gm.BeNumerically(">=", 5))
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Memory).To(gm.BeNumerically(">=", 32768),
					fmt.Sprintf("VM %s memory %d < 32768", vm.Name, vm.Memory))
				gm.Expect(vm.DiskSize).To(gm.BeNumerically(">=", 307200),
					fmt.Sprintf("VM %s disk %d < 307200", vm.Name, vm.DiskSize))
			}
		})

		// Given 50 VMs with varied memory and disk
		// When filtering by memory range and total disk range simultaneously
		// Then only VMs in the intersection should be returned
		ginkgo.It("should combine memory range and total disk range using byExpression", func() {
			expr := "memory >= 8GB and memory <= 32GB and total_disk_capacity >= 200GB and total_disk_capacity <= 600GB"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs with 8-32GB memory AND 200-600GB total disk: %d\n", result.Total)
			gm.Expect(result.Total).To(gm.BeNumerically(">=", 1), "expected at least 1 VM in the intersection")
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Memory).To(gm.BeNumerically(">=", 8192))
				gm.Expect(vm.Memory).To(gm.BeNumerically("<=", 32768))
				gm.Expect(vm.DiskSize).To(gm.BeNumerically(">=", 204800))
				gm.Expect(vm.DiskSize).To(gm.BeNumerically("<=", 614400))
			}
		})

		// Given 50 VMs with varied memory and disk
		// When filtering by memory min, sorting by disk descending
		// Then results should satisfy the filter AND be in descending disk order
		ginkgo.It("should combine memory filter with disk sort using byExpression", func() {
			expr := "memory >= 8GB"

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				Sort:         []string{"diskSize:desc"},
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("Filtered VMs (memory >= 8GB) sorted by disk desc: %d\n", result.Total)
			gm.Expect(result.Total).To(gm.Equal(41), "expected 41 VMs with >= 8GB memory")
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Memory).To(gm.BeNumerically(">=", 8192))
			}
			for i := 1; i < len(result.VirtualMachines); i++ {
				gm.Expect(result.VirtualMachines[i-1].DiskSize).To(gm.BeNumerically(">=", result.VirtualMachines[i].DiskSize),
					"VMs should be sorted by disk size descending")
			}
		})

		// Given 50 VMs with varied memory and disk
		// When combining memory filter, total disk filter, sort, and pagination
		// Then the page should contain correctly filtered, sorted results with accurate totals
		ginkgo.It("should combine byExpression filter, sort, and pagination", func() {
			expr := "memory >= 8GB and total_disk_capacity >= 200GB"
			page := 1
			pageSize := 5

			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				Sort:         []string{"memory:desc"},
				Page:         &page,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("Full combo: total=%d, page=%d, pageCount=%d, returned=%d\n",
				result.Total, result.Page, result.PageCount, len(result.VirtualMachines))
			gm.Expect(result.Page).To(gm.Equal(1))
			gm.Expect(len(result.VirtualMachines)).To(gm.Equal(pageSize))
			gm.Expect(result.Total).To(gm.BeNumerically(">=", pageSize), "total should exceed page size")
			gm.Expect(result.PageCount).To(gm.Equal((result.Total + pageSize - 1) / pageSize))
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Memory).To(gm.BeNumerically(">=", 8192))
				gm.Expect(vm.DiskSize).To(gm.BeNumerically(">=", 204800))
			}
			for i := 1; i < len(result.VirtualMachines); i++ {
				gm.Expect(result.VirtualMachines[i-1].Memory).To(gm.BeNumerically(">=", result.VirtualMachines[i].Memory),
					"VMs should be sorted by memory descending")
			}
		})

		// Given 50 VMs
		// When filtering by memory, total disk, cluster, and status simultaneously
		// Then all four filters should be applied as an AND
		ginkgo.It("should apply all filter dimensions together using byExpression", func() {
			all := listAllVMs()
			clusterName := all.VirtualMachines[0].Cluster
			expr := fmt.Sprintf("memory >= 16GB and total_disk_capacity >= 100GB and powerstate = 'poweredOn' and cluster = '%s'", clusterName)

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("All dimensions combined: %d VMs\n", result.Total)
			gm.Expect(result.Total).To(gm.BeNumerically(">=", 10))
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Memory).To(gm.BeNumerically(">=", 16384))
				gm.Expect(vm.DiskSize).To(gm.BeNumerically(">=", 102400))
				gm.Expect(vm.VCenterState).To(gm.Equal("poweredOn"))
				gm.Expect(vm.Cluster).To(gm.Equal(clusterName))
			}
		})
	})

	// -----------------------------------------------------------------
	// Sorting
	// -----------------------------------------------------------------
	ginkgo.Context("sorting", func() {
		// Given 50 VMs with names test-vm-01 through test-vm-50
		// When sorting by name ascending
		// Then VMs should be in alphabetical order
		ginkgo.It("should sort by name ascending", func() {
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				Sort: []string{"name:asc"},
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(result.VirtualMachines)).To(gm.BeNumerically(">", 1))
			for i := 1; i < len(result.VirtualMachines); i++ {
				gm.Expect(result.VirtualMachines[i-1].Name <= result.VirtualMachines[i].Name).To(gm.BeTrue(),
					fmt.Sprintf("expected %s <= %s", result.VirtualMachines[i-1].Name, result.VirtualMachines[i].Name))
			}
		})

		// Given 50 VMs with names test-vm-01 through test-vm-50
		// When sorting by name descending
		// Then VMs should be in reverse alphabetical order
		ginkgo.It("should sort by name descending", func() {
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				Sort: []string{"name:desc"},
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(result.VirtualMachines)).To(gm.BeNumerically(">", 1))
			for i := 1; i < len(result.VirtualMachines); i++ {
				gm.Expect(result.VirtualMachines[i-1].Name >= result.VirtualMachines[i].Name).To(gm.BeTrue(),
					fmt.Sprintf("expected %s >= %s", result.VirtualMachines[i-1].Name, result.VirtualMachines[i].Name))
			}
		})

		// Given 50 VMs with varied memory sizes
		// When sorting by memory descending
		// Then VMs should be in decreasing memory order
		ginkgo.It("should sort by memory descending", func() {
			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				Sort:     []string{"memory:desc"},
				PageSize: &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(result.VirtualMachines)).To(gm.Equal(50))
			for i := 1; i < len(result.VirtualMachines); i++ {
				gm.Expect(result.VirtualMachines[i-1].Memory).To(gm.BeNumerically(">=", result.VirtualMachines[i].Memory),
					"VMs should be sorted by memory descending")
			}
		})

		// Given 50 VMs with varied disk sizes
		// When sorting by disk size ascending
		// Then VMs should be in increasing disk size order
		ginkgo.It("should sort by diskSize ascending", func() {
			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				Sort:     []string{"diskSize:asc"},
				PageSize: &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(result.VirtualMachines)).To(gm.Equal(50))
			for i := 1; i < len(result.VirtualMachines); i++ {
				gm.Expect(result.VirtualMachines[i-1].DiskSize).To(gm.BeNumerically("<=", result.VirtualMachines[i].DiskSize),
					"VMs should be sorted by disk size ascending")
			}
		})

		// Given 50 VMs with varied memory and names
		// When sorting by memory descending then name ascending (multi-sort)
		// Then VMs with equal memory should be sub-sorted by name
		ginkgo.It("should apply multi-sort memory desc then name asc", func() {
			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				Sort:     []string{"memory:desc", "name:asc"},
				PageSize: &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(result.VirtualMachines)).To(gm.Equal(50))
			for i := 1; i < len(result.VirtualMachines); i++ {
				prev := result.VirtualMachines[i-1]
				curr := result.VirtualMachines[i]
				if prev.Memory == curr.Memory {
					gm.Expect(prev.Name <= curr.Name).To(gm.BeTrue(),
						fmt.Sprintf("within same memory tier, expected %s <= %s", prev.Name, curr.Name))
				} else {
					gm.Expect(prev.Memory).To(gm.BeNumerically(">", curr.Memory),
						"memory should be descending across tiers")
				}
			}
		})
	})

	// -----------------------------------------------------------------
	// Pagination
	// -----------------------------------------------------------------
	ginkgo.Context("pagination", func() {
		// Given 50 VMs
		// When requesting page 1 with page size 3
		// Then 3 VMs should be returned with correct pagination metadata
		ginkgo.It("should paginate with correct metadata", func() {
			page := 1
			pageSize := 3

			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				Page:     &page,
				PageSize: &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("Page %d: %d VMs (total: %d, pages: %d)\n",
				result.Page, len(result.VirtualMachines), result.Total, result.PageCount)
			gm.Expect(len(result.VirtualMachines)).To(gm.Equal(3))
			gm.Expect(result.Page).To(gm.Equal(1))
			gm.Expect(result.Total).To(gm.Equal(50))
			gm.Expect(result.PageCount).To(gm.Equal(17))
		})

		// Given 50 VMs
		// When requesting pages 1 and 2 with the same page size
		// Then the two pages should contain different VMs
		ginkgo.It("should return different VMs on different pages", func() {
			page1 := 1
			page2 := 2
			pageSize := 3

			result1, err := agentSvc.ListLatestVMs(&service.VMListParams{Page: &page1, PageSize: &pageSize})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			result2, err := agentSvc.ListLatestVMs(&service.VMListParams{Page: &page2, PageSize: &pageSize})
			gm.Expect(err).ToNot(gm.HaveOccurred())

			for _, vm1 := range result1.VirtualMachines {
				for _, vm2 := range result2.VirtualMachines {
					gm.Expect(vm1.Id).ToNot(gm.Equal(vm2.Id),
						fmt.Sprintf("VM %s appeared on both page 1 and 2", vm1.Name))
				}
			}
		})

		// Given 50 VMs and page size 3 (17 pages, last page has 2 items)
		// When requesting the last page
		// Then only the remainder (2 VMs) should be returned
		ginkgo.It("should return correct remainder on last page", func() {
			page := 17
			pageSize := 3

			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				Page:     &page,
				PageSize: &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("Last page (%d): %d VMs\n", page, len(result.VirtualMachines))
			gm.Expect(len(result.VirtualMachines)).To(gm.Equal(2), "last page should have 50 - 16*3 = 2 VMs")
			gm.Expect(result.Page).To(gm.Equal(17))
		})

		// Given 50 VMs
		// When listing without specifying a page size
		// Then the default page size (20) should be applied
		ginkgo.It("should use default page size when not specified", func() {
			result, err := agentSvc.ListLatestVMs(nil)

			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(result.VirtualMachines)).To(gm.Equal(20), "default page size is 20")
			gm.Expect(result.Total).To(gm.Equal(50))
			gm.Expect(result.PageCount).To(gm.Equal(3))
		})

		// Given 50 VMs with a filter that matches fewer VMs
		// When paginating the filtered results
		// Then pagination should reflect the filtered total, not all 50
		ginkgo.It("should paginate filtered results correctly using byExpression", func() {
			expr := "memory = 128GB"
			page := 1
			pageSize := 3

			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				Page:         &page,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("Filtered pagination: total=%d, pageCount=%d, returned=%d\n",
				result.Total, result.PageCount, len(result.VirtualMachines))
			gm.Expect(result.Total).To(gm.Equal(8), "exactly 8 VMs have 128GB memory")
			gm.Expect(result.PageCount).To(gm.Equal(3), "ceil(8/3) = 3 pages")
			gm.Expect(len(result.VirtualMachines)).To(gm.Equal(3))
		})
	})

	// -----------------------------------------------------------------
	// LIKE operator (substring match via byExpression)
	// -----------------------------------------------------------------
	ginkgo.Context("LIKE operator filters", func() {
		ginkgo.It("should filter VMs by name substring using like", func() {
			all := listAllVMs()
			gm.Expect(len(all.VirtualMachines)).To(gm.BeNumerically(">", 0))

			firstName := all.VirtualMachines[0].Name
			gm.Expect(len(firstName)).To(gm.BeNumerically(">=", 3))
			substr := firstName[:3]
			ginkgo.GinkgoWriter.Printf("First VM name: %s, using '%s' as like substring\n", firstName, substr)
			expr := fmt.Sprintf("name like '%s'", substr)

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("VMs matching like '%s': %d\n", substr, result.Total)
			gm.Expect(result.Total).To(gm.BeNumerically(">", 0))
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Name).To(gm.ContainSubstring(substr))
			}
		})

		ginkgo.It("should return empty for non-matching like substring", func() {
			expr := "name like 'ZZZZNOTEXIST'"

			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.Total).To(gm.Equal(0))
			gm.Expect(result.VirtualMachines).To(gm.BeEmpty())
		})

		ginkgo.It("should combine like with memory filter", func() {
			all := listAllVMs()
			gm.Expect(len(all.VirtualMachines)).To(gm.BeNumerically(">", 0))
			firstName := all.VirtualMachines[0].Name
			gm.Expect(len(firstName)).To(gm.BeNumerically(">=", 3))
			substr := firstName[:3]
			expr := fmt.Sprintf("name like '%s' and memory >= 32GB", substr)

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Name).To(gm.ContainSubstring(substr))
				gm.Expect(vm.Memory).To(gm.BeNumerically(">=", 32768))
			}
		})

		ginkgo.It("should combine like with regex in same expression", func() {
			all := listAllVMs()
			gm.Expect(len(all.VirtualMachines)).To(gm.BeNumerically(">", 0))
			firstName := all.VirtualMachines[0].Name
			gm.Expect(len(firstName)).To(gm.BeNumerically(">=", 3))
			substr := firstName[:3]
			expr := fmt.Sprintf("name like '%s' and name ~ /%s/", substr, substr)

			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.Total).To(gm.BeNumerically(">", 0))
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Name).To(gm.ContainSubstring(substr))
			}
		})
	})

	// -----------------------------------------------------------------
	// Regex operator enforcement (~ requires /regex/, not 'string')
	// -----------------------------------------------------------------
	ginkgo.Context("regex operator validation", func() {
		ginkgo.It("should reject ~ with string literal instead of regex", func() {
			expr := "name ~ 'test'"

			_, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
			})

			gm.Expect(err).To(gm.HaveOccurred())
		})

		ginkgo.It("should reject !~ with string literal instead of regex", func() {
			expr := "name !~ 'test'"

			_, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
			})

			gm.Expect(err).To(gm.HaveOccurred())
		})

		ginkgo.It("should reject like with regex literal instead of string", func() {
			expr := "name like /pattern/"

			_, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
			})

			gm.Expect(err).To(gm.HaveOccurred())
		})
	})

	// -----------------------------------------------------------------
	// Edge cases
	// -----------------------------------------------------------------
	ginkgo.Context("edge cases", func() {
		// Given 50 VMs with maximum memory of 128 GB
		// When filtering by minimum memory of 200 GB
		// Then no VMs should be returned
		ginkgo.It("should return empty result for unreachable filter using byExpression", func() {
			expr := "memory >= 200GB"

			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expr,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.Total).To(gm.Equal(0))
			gm.Expect(result.VirtualMachines).To(gm.BeEmpty())
		})
	})
})
