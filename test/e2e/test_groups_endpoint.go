package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" // nolint:staticcheck
	. "github.com/onsi/gomega"    // nolint:staticcheck

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/test/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e/service"

	"github.com/google/uuid"
)

var _ = Describe("Group endpoint e2e tests", Ordered, func() {
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

		states := map[string]int{}
		for _, vm := range allVMs {
			states[vm.VCenterState]++
		}
		GinkgoWriter.Printf("Group endpoint test setup complete — %d VMs collected, states: %v\n", totalVMs, states)
	})

	AfterAll(func() {
		GinkgoWriter.Println("Cleaning up group endpoint tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	countVMs := func(pred func(v1.VirtualMachine) bool) int {
		n := 0
		for _, vm := range allVMs {
			if pred(vm) {
				n++
			}
		}
		return n
	}

	firstCluster := func() string {
		Expect(len(allVMs)).To(BeNumerically(">", 0))
		return allVMs[0].Cluster
	}

	// -----------------------------------------------------------------
	// CRUD lifecycle
	// -----------------------------------------------------------------
	Context("CRUD lifecycle", func() {
		var groupID string

		AfterEach(func() {
			if groupID != "" {
				_, _ = agentSvc.DeleteGroup(groupID)
				groupID = ""
			}
		})

		It("should create a group and return it with id and timestamps", func() {
			group, err := agentSvc.CreateGroup("test-group", "memory > 0", "e2e test group")
			Expect(err).ToNot(HaveOccurred())
			groupID = group.Id

			Expect(group.Id).ToNot(BeEmpty())
			Expect(group.Name).To(Equal("test-group"))
			Expect(group.Filter).To(Equal("memory > 0"))
			Expect(group.Description).ToNot(BeNil())
			Expect(*group.Description).To(Equal("e2e test group"))
			Expect(group.CreatedAt).ToNot(BeNil())
			Expect(group.UpdatedAt).ToNot(BeNil())
		})

		It("should list created groups", func() {
			g1, err := agentSvc.CreateGroup("group-a", "memory > 0", "")
			Expect(err).ToNot(HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(g1.Id) }()

			g2, err := agentSvc.CreateGroup("group-b", "memory >= 8192", "")
			Expect(err).ToNot(HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(g2.Id) }()

			list, err := agentSvc.ListGroups()
			Expect(err).ToNot(HaveOccurred())
			Expect(len(list.Groups)).To(BeNumerically(">=", 2))

			names := make([]string, len(list.Groups))
			for i, g := range list.Groups {
				names[i] = g.Name
			}
			Expect(names).To(ContainElement("group-a"))
			Expect(names).To(ContainElement("group-b"))
		})

		It("should get a group by ID with its VMs", func() {
			group, err := agentSvc.CreateGroup("all-vms", "memory > 0", "")
			Expect(err).ToNot(HaveOccurred())
			groupID = group.Id

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, &service.GroupGetParams{PageSize: &pageSize})
			Expect(err).ToNot(HaveOccurred())

			Expect(resp.Group.Id).To(Equal(group.Id))
			Expect(resp.Group.Name).To(Equal("all-vms"))
			Expect(resp.Total).To(Equal(totalVMs))
			Expect(len(resp.Vms)).To(Equal(totalVMs))
		})

		It("should update group name via PATCH", func() {
			group, err := agentSvc.CreateGroup("original-name", "memory > 0", "")
			Expect(err).ToNot(HaveOccurred())
			groupID = group.Id

			newName := "updated-name"
			updated, err := agentSvc.UpdateGroup(group.Id, v1.UpdateGroupRequest{Name: &newName})
			Expect(err).ToNot(HaveOccurred())

			Expect(updated.Name).To(Equal("updated-name"))
			Expect(updated.Filter).To(Equal("memory > 0"))
		})

		It("should update group filter via PATCH and change VM list", func() {
			group, err := agentSvc.CreateGroup("filter-test", "memory > 0", "")
			Expect(err).ToNot(HaveOccurred())
			groupID = group.Id

			pageSize := 100
			before, err := agentSvc.GetGroup(group.Id, &service.GroupGetParams{PageSize: &pageSize})
			Expect(err).ToNot(HaveOccurred())
			Expect(before.Total).To(Equal(totalVMs))

			newFilter := "memory >= 32768"
			_, err = agentSvc.UpdateGroup(group.Id, v1.UpdateGroupRequest{Filter: &newFilter})
			Expect(err).ToNot(HaveOccurred())

			expectedAfter := countVMs(func(vm v1.VirtualMachine) bool { return vm.Memory >= 32768 })
			after, err := agentSvc.GetGroup(group.Id, &service.GroupGetParams{PageSize: &pageSize})
			Expect(err).ToNot(HaveOccurred())
			Expect(after.Total).To(Equal(expectedAfter))
			Expect(after.Total).To(BeNumerically("<", totalVMs))
		})

		It("should delete a group", func() {
			group, err := agentSvc.CreateGroup("to-delete", "memory > 0", "")
			Expect(err).ToNot(HaveOccurred())

			status, err := agentSvc.DeleteGroup(group.Id)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusNoContent))

			getStatus, err := agentSvc.GetGroupStatus(group.Id)
			Expect(err).ToNot(HaveOccurred())
			Expect(getStatus).To(Equal(http.StatusNotFound))
		})

		It("should return 204 when deleting non-existent group", func() {
			status, err := agentSvc.DeleteGroup("999999")
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusNoContent))
		})
	})

	// -----------------------------------------------------------------
	// Validation
	// -----------------------------------------------------------------
	Context("validation", func() {
		It("should reject empty name", func() {
			body, _ := json.Marshal(v1.CreateGroupRequest{Name: "", Filter: "memory > 0"})
			status, err := agentSvc.CreateGroupRaw(body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should reject name longer than 100 characters", func() {
			longName := strings.Repeat("a", 101)
			body, _ := json.Marshal(v1.CreateGroupRequest{Name: longName, Filter: "memory > 0"})
			status, err := agentSvc.CreateGroupRaw(body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should reject empty filter", func() {
			body, _ := json.Marshal(v1.CreateGroupRequest{Name: "valid-name", Filter: ""})
			status, err := agentSvc.CreateGroupRaw(body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should reject invalid filter syntax", func() {
			body, _ := json.Marshal(v1.CreateGroupRequest{Name: "valid-name", Filter: "invalid %%% filter"})
			status, err := agentSvc.CreateGroupRaw(body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should reject PATCH with no fields", func() {
			group, err := agentSvc.CreateGroup("patch-test", "memory > 0", "")
			Expect(err).ToNot(HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

			body, _ := json.Marshal(v1.UpdateGroupRequest{})
			status, err := agentSvc.UpdateGroupRaw(group.Id, body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should reject duplicate group name on create", func() {
			group, err := agentSvc.CreateGroup("unique-name", "memory > 0", "")
			Expect(err).ToNot(HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

			// Try to create another group with the same name
			body, _ := json.Marshal(v1.CreateGroupRequest{Name: "unique-name", Filter: "memory > 0"})
			status, err := agentSvc.CreateGroupRaw(body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should reject duplicate group name on update", func() {
			group1, err := agentSvc.CreateGroup("first-name", "memory > 0", "")
			Expect(err).ToNot(HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(group1.Id) }()

			group2, err := agentSvc.CreateGroup("second-name", "memory > 0", "")
			Expect(err).ToNot(HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(group2.Id) }()

			// Try to update first group to have the same name as second
			newName := "second-name"
			body, _ := json.Marshal(v1.UpdateGroupRequest{Name: &newName})
			status, err := agentSvc.UpdateGroupRaw(group1.Id, body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})
	})

	// -----------------------------------------------------------------
	// Filter DSL via groups
	// -----------------------------------------------------------------
	Context("filter DSL via groups", func() {
		AfterEach(func() {
			list, err := agentSvc.ListGroups()
			if err == nil {
				for _, g := range list.Groups {
					_, _ = agentSvc.DeleteGroup(g.Id)
				}
			}
		})

		It("should match all VMs with memory > 0", func() {
			group, err := agentSvc.CreateGroup("all", "memory > 0", "")
			Expect(err).ToNot(HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, &service.GroupGetParams{PageSize: &pageSize})
			Expect(err).ToNot(HaveOccurred())

			GinkgoWriter.Printf("Filter 'memory > 0': total=%d\n", resp.Total)
			Expect(resp.Total).To(Equal(totalVMs))
			Expect(len(resp.Vms)).To(Equal(totalVMs))
		})

		It("should filter by powerstate", func() {
			poweredOn := countVMs(func(vm v1.VirtualMachine) bool { return vm.VCenterState == "poweredOn" })
			GinkgoWriter.Printf("VMs with poweredOn: %d / %d\n", poweredOn, totalVMs)

			group, err := agentSvc.CreateGroup("powered-on", "powerstate = 'poweredOn'", "")
			Expect(err).ToNot(HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, &service.GroupGetParams{PageSize: &pageSize})
			Expect(err).ToNot(HaveOccurred())

			GinkgoWriter.Printf("Filter 'poweredOn': total=%d\n", resp.Total)
			Expect(resp.Total).To(Equal(poweredOn))
			for _, vm := range resp.Vms {
				Expect(vm.VCenterState).To(Equal("poweredOn"))
			}
		})

		It("should filter by cluster", func() {
			clusterName := firstCluster()
			expected := countVMs(func(vm v1.VirtualMachine) bool { return vm.Cluster == clusterName })
			GinkgoWriter.Printf("Using cluster: %s (expected %d)\n", clusterName, expected)

			group, err := agentSvc.CreateGroup("one-cluster",
				fmt.Sprintf("cluster = '%s'", clusterName), "")
			Expect(err).ToNot(HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, &service.GroupGetParams{PageSize: &pageSize})
			Expect(err).ToNot(HaveOccurred())

			GinkgoWriter.Printf("Filter cluster='%s': total=%d\n", clusterName, resp.Total)
			Expect(resp.Total).To(Equal(expected))
			for _, vm := range resp.Vms {
				Expect(vm.Cluster).To(Equal(clusterName))
			}
		})

		It("should filter by memory >= 32768", func() {
			expected := countVMs(func(vm v1.VirtualMachine) bool { return vm.Memory >= 32768 })

			group, err := agentSvc.CreateGroup("big-memory", "memory >= 32768", "")
			Expect(err).ToNot(HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, &service.GroupGetParams{PageSize: &pageSize})
			Expect(err).ToNot(HaveOccurred())

			GinkgoWriter.Printf("Filter memory>=32768: total=%d (expected %d)\n", resp.Total, expected)
			Expect(resp.Total).To(Equal(expected))
			for _, vm := range resp.Vms {
				Expect(vm.Memory).To(BeNumerically(">=", 32768))
			}
		})

		It("should filter by exact memory tier", func() {
			expected := countVMs(func(vm v1.VirtualMachine) bool { return vm.Memory == 131072 })

			group, err := agentSvc.CreateGroup("mem-128g", "memory = 131072", "")
			Expect(err).ToNot(HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, &service.GroupGetParams{PageSize: &pageSize})
			Expect(err).ToNot(HaveOccurred())

			GinkgoWriter.Printf("Filter memory=131072: total=%d (expected %d)\n", resp.Total, expected)
			Expect(resp.Total).To(Equal(expected))
			for _, vm := range resp.Vms {
				Expect(vm.Memory).To(Equal(int64(131072)))
			}
		})

		It("should filter by name", func() {
			vmName := allVMs[0].Name

			group, err := agentSvc.CreateGroup("single-vm",
				fmt.Sprintf("name = '%s'", vmName), "")
			Expect(err).ToNot(HaveOccurred())

			resp, err := agentSvc.GetGroup(group.Id, nil)
			Expect(err).ToNot(HaveOccurred())

			GinkgoWriter.Printf("Filter name='%s': total=%d\n", vmName, resp.Total)
			Expect(resp.Total).To(Equal(1))
			Expect(resp.Vms[0].Name).To(Equal(vmName))
		})

		It("should filter by firmware", func() {
			group, err := agentSvc.CreateGroup("bios-only", "firmware = 'bios'", "")
			Expect(err).ToNot(HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, &service.GroupGetParams{PageSize: &pageSize})
			Expect(err).ToNot(HaveOccurred())

			GinkgoWriter.Printf("Filter firmware='bios': total=%d\n", resp.Total)
			Expect(resp.Total).To(BeNumerically(">", 0))
		})

		It("should filter by cpus", func() {
			group, err := agentSvc.CreateGroup("cpu-4", "cpus = 4", "")
			Expect(err).ToNot(HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, &service.GroupGetParams{PageSize: &pageSize})
			Expect(err).ToNot(HaveOccurred())

			GinkgoWriter.Printf("Filter cpus=4: total=%d\n", resp.Total)
			Expect(resp.Total).To(BeNumerically(">", 0))
			Expect(resp.Total).To(BeNumerically("<", totalVMs))
		})

		It("should apply combined cross-table filter", func() {
			clusterName := firstCluster()
			expected := countVMs(func(vm v1.VirtualMachine) bool {
				return vm.Memory >= 32768 && vm.Cluster == clusterName
			})
			GinkgoWriter.Printf("Expect %d VMs with memory>=32768 in cluster %s\n", expected, clusterName)

			group, err := agentSvc.CreateGroup("combined-filter",
				fmt.Sprintf("memory >= 32768 and cluster = '%s'", clusterName), "")
			Expect(err).ToNot(HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, &service.GroupGetParams{PageSize: &pageSize})
			Expect(err).ToNot(HaveOccurred())

			GinkgoWriter.Printf("Combined filter: total=%d\n", resp.Total)
			Expect(resp.Total).To(Equal(expected))
			for _, vm := range resp.Vms {
				Expect(vm.Memory).To(BeNumerically(">=", 32768))
				Expect(vm.Cluster).To(Equal(clusterName))
			}
		})

		It("should return 0 VMs for non-matching filter", func() {
			group, err := agentSvc.CreateGroup("no-match", "memory > 999999999", "")
			Expect(err).ToNot(HaveOccurred())

			resp, err := agentSvc.GetGroup(group.Id, nil)
			Expect(err).ToNot(HaveOccurred())

			Expect(resp.Total).To(Equal(0))
			Expect(resp.Vms).To(BeEmpty())
		})
	})

	// -----------------------------------------------------------------
	// Pagination on group VMs
	// -----------------------------------------------------------------
	Context("pagination", func() {
		var groupID string

		BeforeAll(func() {
			group, err := agentSvc.CreateGroup("paginate-group", "memory > 0", "")
			Expect(err).ToNot(HaveOccurred())
			groupID = group.Id
		})

		AfterAll(func() {
			if groupID != "" {
				_, _ = agentSvc.DeleteGroup(groupID)
			}
		})

		It("should return correct pagination metadata", func() {
			page := 1
			pageSize := 5
			resp, err := agentSvc.GetGroup(groupID, &service.GroupGetParams{
				Page: &page, PageSize: &pageSize,
			})
			Expect(err).ToNot(HaveOccurred())

			expectedPages := (totalVMs + pageSize - 1) / pageSize
			GinkgoWriter.Printf("Pagination: page=%d, pageCount=%d, total=%d, returned=%d\n",
				resp.Page, resp.PageCount, resp.Total, len(resp.Vms))
			Expect(resp.Page).To(Equal(1))
			Expect(resp.Total).To(Equal(totalVMs))
			Expect(resp.PageCount).To(Equal(expectedPages))
			Expect(len(resp.Vms)).To(Equal(pageSize))
		})

		It("should return different VMs on page 2", func() {
			page1 := 1
			page2 := 2
			pageSize := 5

			resp1, err := agentSvc.GetGroup(groupID, &service.GroupGetParams{
				Page: &page1, PageSize: &pageSize,
			})
			Expect(err).ToNot(HaveOccurred())

			resp2, err := agentSvc.GetGroup(groupID, &service.GroupGetParams{
				Page: &page2, PageSize: &pageSize,
			})
			Expect(err).ToNot(HaveOccurred())

			for _, vm1 := range resp1.Vms {
				for _, vm2 := range resp2.Vms {
					Expect(vm1.Id).ToNot(Equal(vm2.Id),
						fmt.Sprintf("VM %s appeared on both page 1 and 2", vm1.Name))
				}
			}
		})
	})

	// -----------------------------------------------------------------
	// Sort on group VMs
	// -----------------------------------------------------------------
	Context("sorting", func() {
		var groupID string

		BeforeAll(func() {
			group, err := agentSvc.CreateGroup("sort-group", "memory > 0", "")
			Expect(err).ToNot(HaveOccurred())
			groupID = group.Id
		})

		AfterAll(func() {
			if groupID != "" {
				_, _ = agentSvc.DeleteGroup(groupID)
			}
		})

		It("should sort by name ascending", func() {
			pageSize := 100
			resp, err := agentSvc.GetGroup(groupID, &service.GroupGetParams{
				Sort: []string{"name:asc"}, PageSize: &pageSize,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(resp.Vms)).To(BeNumerically(">", 1))

			for i := 1; i < len(resp.Vms); i++ {
				Expect(resp.Vms[i-1].Name <= resp.Vms[i].Name).To(BeTrue(),
					fmt.Sprintf("expected %s <= %s", resp.Vms[i-1].Name, resp.Vms[i].Name))
			}
		})

		It("should sort by memory descending", func() {
			pageSize := 100
			resp, err := agentSvc.GetGroup(groupID, &service.GroupGetParams{
				Sort: []string{"memory:desc"}, PageSize: &pageSize,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(resp.Vms)).To(Equal(totalVMs))

			for i := 1; i < len(resp.Vms); i++ {
				Expect(resp.Vms[i-1].Memory).To(BeNumerically(">=", resp.Vms[i].Memory),
					"VMs should be sorted by memory descending")
			}
		})
	})

})

var _ = Describe("Auto-created folder groups e2e tests", Ordered, func() {
	// vcsim model has 3 folders by MoRef ID:
	// - group-60 (databases): 17 VMs (index 0-16)
	// - group-61 (workload): 17 VMs (index 17-33)
	// - group-62 (sap): 16 VMs (index 34-49)
	// The Folder column in vinfo stores the folder ID, not the name.

	var (
		agentSvc *service.AgentSvc
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

		GinkgoWriter.Println("Auto-created folder groups test setup complete")
	})

	AfterAll(func() {
		GinkgoWriter.Println("Cleaning up auto-created folder groups tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	// Given an agent that has collected inventory from vcsim with 3 folders
	// When listing all groups
	// Then groups for each folder plus "No Folder" should exist
	It("should have auto-created groups for each folder after collection", func() {
		// Act
		list, err := agentSvc.ListGroups()
		Expect(err).ToNot(HaveOccurred())

		// Assert
		groupNames := make([]string, len(list.Groups))
		for i, g := range list.Groups {
			groupNames[i] = g.Name
		}
		GinkgoWriter.Printf("Auto-created groups: %v\n", groupNames)

		Expect(groupNames).To(ContainElement("group-60"))
		Expect(groupNames).To(ContainElement("group-61"))
		Expect(groupNames).To(ContainElement("group-62"))
		Expect(groupNames).To(ContainElement("No Folder"))
	})

	// Given auto-created folder groups exist
	// When inspecting the filter field of each group
	// Then each group should have the correct folder filter syntax
	It("should have correct filter for folder groups", func() {
		// Act
		list, err := agentSvc.ListGroups()
		Expect(err).ToNot(HaveOccurred())

		// Assert
		for _, g := range list.Groups {
			switch g.Name {
			case "group-60":
				Expect(g.Filter).To(Equal("folder = 'group-60'"))
			case "group-61":
				Expect(g.Filter).To(Equal("folder = 'group-61'"))
			case "group-62":
				Expect(g.Filter).To(Equal("folder = 'group-62'"))
			case "No Folder":
				Expect(g.Filter).To(Equal("folder = ''"))
			}
		}
	})

	// Given the group-60 folder contains 17 VMs (index 0-16)
	// When getting the group-60 folder group
	// Then it should return exactly 17 VMs
	It("should return correct VMs for group-60 folder group", func() {
		// Arrange
		list, err := agentSvc.ListGroups()
		Expect(err).ToNot(HaveOccurred())

		var folderGroupID string
		for _, g := range list.Groups {
			if g.Name == "group-60" {
				folderGroupID = g.Id
				break
			}
		}
		Expect(folderGroupID).ToNot(BeEmpty(), "group-60 group should exist")

		// Act
		pageSize := 100
		resp, err := agentSvc.GetGroup(folderGroupID, &service.GroupGetParams{PageSize: &pageSize})
		Expect(err).ToNot(HaveOccurred())

		// Assert
		GinkgoWriter.Printf("group-60 folder group: total=%d\n", resp.Total)
		Expect(resp.Total).To(Equal(17))
	})

	// Given the group-61 folder contains 17 VMs (index 17-33)
	// When getting the group-61 folder group
	// Then it should return exactly 17 VMs
	It("should return correct VMs for group-61 folder group", func() {
		// Arrange
		list, err := agentSvc.ListGroups()
		Expect(err).ToNot(HaveOccurred())

		var folderGroupID string
		for _, g := range list.Groups {
			if g.Name == "group-61" {
				folderGroupID = g.Id
				break
			}
		}
		Expect(folderGroupID).ToNot(BeEmpty(), "group-61 group should exist")

		// Act
		pageSize := 100
		resp, err := agentSvc.GetGroup(folderGroupID, &service.GroupGetParams{PageSize: &pageSize})
		Expect(err).ToNot(HaveOccurred())

		// Assert
		GinkgoWriter.Printf("group-61 folder group: total=%d\n", resp.Total)
		Expect(resp.Total).To(Equal(17))
	})

	// Given the group-62 folder contains 16 VMs (index 34-49)
	// When getting the group-62 folder group
	// Then it should return exactly 16 VMs
	It("should return correct VMs for group-62 folder group", func() {
		// Arrange
		list, err := agentSvc.ListGroups()
		Expect(err).ToNot(HaveOccurred())

		var folderGroupID string
		for _, g := range list.Groups {
			if g.Name == "group-62" {
				folderGroupID = g.Id
				break
			}
		}
		Expect(folderGroupID).ToNot(BeEmpty(), "group-62 group should exist")

		// Act
		pageSize := 100
		resp, err := agentSvc.GetGroup(folderGroupID, &service.GroupGetParams{PageSize: &pageSize})
		Expect(err).ToNot(HaveOccurred())

		// Assert
		GinkgoWriter.Printf("group-62 folder group: total=%d\n", resp.Total)
		Expect(resp.Total).To(Equal(16))
	})

	// Given all VMs in vcsim model are organized in folders
	// When getting the "No Folder" group
	// Then it should return 0 VMs
	It("should return 0 VMs for No Folder group in vcsim model", func() {
		// Arrange
		list, err := agentSvc.ListGroups()
		Expect(err).ToNot(HaveOccurred())

		var noFolderGroupID string
		for _, g := range list.Groups {
			if g.Name == "No Folder" {
				noFolderGroupID = g.Id
				break
			}
		}
		Expect(noFolderGroupID).ToNot(BeEmpty(), "No Folder group should exist")

		// Act
		resp, err := agentSvc.GetGroup(noFolderGroupID, nil)
		Expect(err).ToNot(HaveOccurred())

		// Assert
		GinkgoWriter.Printf("No Folder group: total=%d\n", resp.Total)
		Expect(resp.Total).To(Equal(0))
	})

	// Given 50 VMs distributed across 3 folders (group-60: 17, group-61: 17, group-62: 16)
	// When summing VMs across all folder groups
	// Then the total should equal 50
	It("should have all 50 VMs distributed across folder groups", func() {
		// Arrange
		list, err := agentSvc.ListGroups()
		Expect(err).ToNot(HaveOccurred())
		pageSize := 100

		// Act
		totalInFolders := 0
		for _, g := range list.Groups {
			if g.Name == "group-60" || g.Name == "group-61" || g.Name == "group-62" {
				resp, err := agentSvc.GetGroup(g.Id, &service.GroupGetParams{PageSize: &pageSize})
				Expect(err).ToNot(HaveOccurred())
				totalInFolders += resp.Total
				GinkgoWriter.Printf("Folder %s: %d VMs\n", g.Name, resp.Total)
			}
		}

		// Assert
		Expect(totalInFolders).To(Equal(50), "all 50 VMs should be in folder groups")
	})
})
