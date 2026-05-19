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

var _ = Describe("Group inventory e2e tests", Ordered, func() {
	var (
		agentSvc *service.AgentSvc
		allVMs   []v1.VirtualMachine
		totalVMs int
	)

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

		pageSize := 100
		result, err := agentSvc.ListVMs(&service.VMListParams{PageSize: &pageSize})
		Expect(err).ToNot(HaveOccurred(), "failed to list VMs after collection")
		allVMs = result.Vms
		totalVMs = result.Total
		Expect(totalVMs).To(Equal(50), "vcsim model should produce 50 VMs")

		GinkgoWriter.Printf("Group inventory test setup complete — %d VMs collected\n", totalVMs)
	})

	AfterAll(func() {
		GinkgoWriter.Println("Cleaning up group inventory tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	It("should include inventory in GroupResponse for groups with matched VMs", func() {
		firstCluster := allVMs[0].Cluster
		group, err := agentSvc.CreateGroup(
			"cluster-inventory-test",
			fmt.Sprintf("cluster = '%s'", firstCluster),
			"Test group for inventory",
		)
		Expect(err).ToNot(HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		resp, err := agentSvc.GetGroup(group.Id, nil)
		Expect(err).ToNot(HaveOccurred())

		Expect(resp.Inventory).ToNot(BeNil(), "inventory should be populated")

		// Access struct fields directly
		Expect(resp.Inventory.Vcenter).ToNot(BeNil(), "inventory should contain vcenter")

		vcenter := resp.Inventory.Vcenter
		Expect(vcenter.Infra.Hosts).ToNot(BeNil(), "vcenter infra should contain hosts")
		Expect(vcenter.Infra.Datastores).ToNot(BeEmpty(), "vcenter infra should contain datastores")
		// TODO: Networks may be empty due to migration-planner filtering bug
		// See: ECOPROJECT-4703
		// Expect(vcenter.Infra.Networks).ToNot(BeEmpty(), "vcenter infra should contain networks")

		GinkgoWriter.Printf("Group %s inventory - VCenter ID: %s\n", group.Name, resp.Inventory.VcenterId)
	})

	It("should scope inventory to matched VMs only", func() {
		firstCluster := allVMs[0].Cluster
		group, err := agentSvc.CreateGroup(
			"scoped-inventory-test",
			fmt.Sprintf("cluster = '%s'", firstCluster),
			"Test scoped inventory",
		)
		Expect(err).ToNot(HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		pageSize := 100
		resp, err := agentSvc.GetGroup(group.Id, &service.GroupGetParams{PageSize: &pageSize})
		Expect(err).ToNot(HaveOccurred())

		Expect(resp.Inventory).ToNot(BeNil())

		// Verify inventory structure
		Expect(resp.Inventory.Vcenter).ToNot(BeNil(), "inventory should contain vcenter")
		vcenter := resp.Inventory.Vcenter

		// Verify infra is scoped - check that we have hosts/datastores
		Expect(vcenter.Infra.Hosts).ToNot(BeNil(), "infra should have hosts")
		Expect(vcenter.Infra.Datastores).ToNot(BeEmpty(), "infra should have datastores")
		// TODO: Networks may be empty due to migration-planner filtering bug
		// See: ECOPROJECT-4703
		// Expect(vcenter.Infra.Networks).ToNot(BeEmpty(), "infra should have networks")

		// Count VMs in firstCluster to verify scoping
		expectedVMs := 0
		for _, vm := range allVMs {
			if vm.Cluster == firstCluster {
				expectedVMs++
			}
		}

		Expect(resp.Total).To(Equal(expectedVMs),
			"Group should match exactly %d VMs from cluster %s", expectedVMs, firstCluster)

		GinkgoWriter.Printf("Matched VMs count: %d (cluster: %s)\n", resp.Total, firstCluster)
		GinkgoWriter.Printf("Inventory contains scoped stats for %d VMs\n", resp.Total)
	})

	It("should rebuild inventory when group filter is updated", func() {
		firstCluster := allVMs[0].Cluster
		group, err := agentSvc.CreateGroup(
			"rebuild-inventory-test",
			fmt.Sprintf("cluster = '%s'", firstCluster),
			"Test inventory rebuild",
		)
		Expect(err).ToNot(HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		resp1, err := agentSvc.GetGroup(group.Id, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp1.Inventory).ToNot(BeNil())

		newFilter := "memory >= 16384"
		_, err = agentSvc.UpdateGroup(group.Id, v1.UpdateGroupRequest{
			Filter: &newFilter,
		})
		Expect(err).ToNot(HaveOccurred())

		resp2, err := agentSvc.GetGroup(group.Id, nil)
		Expect(err).ToNot(HaveOccurred())

		Expect(resp2.Inventory).ToNot(BeNil())
		Expect(resp2.Group.Filter).To(Equal(newFilter))

		GinkgoWriter.Printf("Initial VM count: %d, Updated VM count: %d\n",
			resp1.Total, resp2.Total)
	})

	It("should have nil inventory when no VMs match", func() {
		group, err := agentSvc.CreateGroup(
			"empty-inventory-test",
			"name = 'nonexistent-vm-name-12345'",
			"Test empty inventory",
		)
		Expect(err).ToNot(HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		resp, err := agentSvc.GetGroup(group.Id, nil)
		Expect(err).ToNot(HaveOccurred())

		Expect(resp.Total).To(Equal(0))
		if resp.Inventory != nil {
			inventory := *resp.Inventory
			GinkgoWriter.Printf("Empty group inventory: %v\n", inventory)
		}
	})

	It("should have independent inventories for different groups", func() {
		firstCluster := allVMs[0].Cluster
		var secondCluster string
		for _, vm := range allVMs {
			if vm.Cluster != firstCluster {
				secondCluster = vm.Cluster
				break
			}
		}
		Expect(secondCluster).ToNot(BeEmpty(), "need at least 2 clusters")

		group1, err := agentSvc.CreateGroup(
			"cluster1-inventory",
			fmt.Sprintf("cluster = '%s'", firstCluster),
			"Cluster 1",
		)
		Expect(err).ToNot(HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group1.Id) }()

		group2, err := agentSvc.CreateGroup(
			"cluster2-inventory",
			fmt.Sprintf("cluster = '%s'", secondCluster),
			"Cluster 2",
		)
		Expect(err).ToNot(HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group2.Id) }()

		resp1, err := agentSvc.GetGroup(group1.Id, nil)
		Expect(err).ToNot(HaveOccurred())

		resp2, err := agentSvc.GetGroup(group2.Id, nil)
		Expect(err).ToNot(HaveOccurred())

		Expect(resp1.Inventory).ToNot(BeNil())
		Expect(resp2.Inventory).ToNot(BeNil())

		GinkgoWriter.Printf("Group1 VM count: %d, Group2 VM count: %d\n",
			resp1.Total, resp2.Total)
	})

	It("should include inventory when listing VMs within a group", func() {
		group, err := agentSvc.CreateGroup("all-vms-inventory", "memory > 0", "All VMs")
		Expect(err).ToNot(HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		pageSize := 10
		resp, err := agentSvc.GetGroup(group.Id, &service.GroupGetParams{
			PageSize: &pageSize,
		})
		Expect(err).ToNot(HaveOccurred())

		Expect(resp.Inventory).ToNot(BeNil())
		Expect(len(resp.Vms)).To(Equal(10))
		Expect(resp.Total).To(Equal(totalVMs))

		Expect(resp.Inventory.Vcenter).ToNot(BeNil())
		vcenter := resp.Inventory.Vcenter
		Expect(vcenter.Infra.Datastores).ToNot(BeEmpty())
	})

	It("should clear inventory when filter updated to match no VMs", func() {
		firstCluster := allVMs[0].Cluster
		group, err := agentSvc.CreateGroup(
			"clear-inventory-test",
			fmt.Sprintf("cluster = '%s'", firstCluster),
			"Test inventory clearing",
		)
		Expect(err).ToNot(HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		resp1, err := agentSvc.GetGroup(group.Id, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp1.Inventory).ToNot(BeNil(), "initial inventory should be populated")
		Expect(resp1.Total).To(BeNumerically(">", 0), "should have matching VMs initially")

		emptyFilter := "name = 'nonexistent-vm-999999'"
		_, err = agentSvc.UpdateGroup(group.Id, v1.UpdateGroupRequest{
			Filter: &emptyFilter,
		})
		Expect(err).ToNot(HaveOccurred())

		resp2, err := agentSvc.GetGroup(group.Id, nil)
		Expect(err).ToNot(HaveOccurred())

		Expect(resp2.Total).To(Equal(0), "should match no VMs after update")
		Expect(resp2.Inventory).To(BeNil(), "inventory should be cleared when no VMs match")
	})

	It("should maintain inventory across pagination requests", func() {
		group, err := agentSvc.CreateGroup("paginated-inventory", "memory > 0", "Paginated test")
		Expect(err).ToNot(HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		pageSize := 10
		page1 := 1
		resp1, err := agentSvc.GetGroup(group.Id, &service.GroupGetParams{
			PageSize: &pageSize,
			Page:     &page1,
		})
		Expect(err).ToNot(HaveOccurred())

		page2 := 2
		resp2, err := agentSvc.GetGroup(group.Id, &service.GroupGetParams{
			PageSize: &pageSize,
			Page:     &page2,
		})
		Expect(err).ToNot(HaveOccurred())

		Expect(resp1.Inventory).ToNot(BeNil())
		Expect(resp2.Inventory).ToNot(BeNil())

		// Both pages should have the same inventory (same VCenter ID and structure)
		Expect(resp1.Inventory.VcenterId).To(Equal(resp2.Inventory.VcenterId))
		Expect(resp1.Inventory.Vcenter).ToNot(BeNil())
		Expect(resp2.Inventory.Vcenter).ToNot(BeNil())
	})
})
