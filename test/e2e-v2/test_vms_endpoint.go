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

		gm.Eventually(func() string {
			status, err := agentSvc.GetCollectorStatus(agentSvc.CollectorID)
			if err != nil {
				return "error"
			}
			ginkgo.GinkgoWriter.Printf("Collector status: %s\n", status.Status)
			return string(status.Status)
		}, 120*time.Second, 2*time.Second).Should(gm.Equal("collected"), "collector did not reach collected state")

		ginkgo.GinkgoWriter.Println("Discovering collections...")
		collections, err := agentSvc.ListCollections()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to list collections")
		gm.Expect(len(collections.Collections)).To(gm.BeNumerically(">", 0), "expected at least one collection")
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
		result, err := agentSvc.ListVMs(collectionID, &service.VMListParams{PageSize: &pageSize})
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
})
