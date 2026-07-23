package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gm "github.com/onsi/gomega"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e-v2/service"

	"github.com/google/uuid"
)

var _ = ginkgo.Describe("Group endpoint v2 e2e tests", ginkgo.Ordered, func() {
	var (
		agentSvc *service.AgentSvc
		allVMs   []v2.VirtualMachine
		totalVMs int
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
			defer func() {
				_ = resp.Body.Close()
			}()
			if resp.StatusCode >= 500 {
				return fmt.Errorf("server error: %d", resp.StatusCode)
			}
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
		ginkgo.GinkgoWriter.Printf("Using collection: %s\n", collections.Collections[0].Id)

		pageSize := 100
		result, err := agentSvc.ListLatestVMs(&service.VMListParams{PageSize: &pageSize})
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to list VMs after collection")
		allVMs = result.VirtualMachines
		totalVMs = result.Total
		gm.Expect(totalVMs).To(gm.Equal(50), "vcsim model should produce 50 VMs")

		ginkgo.GinkgoWriter.Println("Group endpoint v2 test setup complete")
	})

	ginkgo.AfterAll(func() {
		ginkgo.GinkgoWriter.Println("Cleaning up group endpoint v2 tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	countVMs := func(pred func(v2.VirtualMachine) bool) int {
		n := 0
		for _, vm := range allVMs {
			if pred(vm) {
				n++
			}
		}
		return n
	}

	firstCluster := func() string {
		gm.Expect(len(allVMs)).To(gm.BeNumerically(">", 0))
		return allVMs[0].Cluster
	}

	// Given a collected inventory
	// When creating a group with a name and filter
	// Then the returned group should have matching Name and Filter
	ginkgo.It("should create a group", func() {
		group, err := agentSvc.CreateGroup("test-group-v2", "memory > 0", "e2e v2 test group")
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		ginkgo.GinkgoWriter.Printf("Created group: id=%s, name=%s\n", group.Id, group.Name)
		gm.Expect(group.Id).ToNot(gm.BeEmpty())
		gm.Expect(group.Name).To(gm.Equal("test-group-v2"))
		gm.Expect(group.Filter).To(gm.Equal("memory > 0"))
		gm.Expect(group.Description).ToNot(gm.BeNil())
		gm.Expect(*group.Description).To(gm.Equal("e2e v2 test group"))
	})

	// Given two created groups
	// When listing groups
	// Then the total should be at least 2
	ginkgo.It("should list groups", func() {
		g1, err := agentSvc.CreateGroup("list-group-a", "memory > 0", "")
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(g1.Id) }()

		g2, err := agentSvc.CreateGroup("list-group-b", "memory > 0", "")
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(g2.Id) }()

		list, err := agentSvc.ListGroups(nil, nil, nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		ginkgo.GinkgoWriter.Printf("Listed groups: total=%d\n", list.Total)
		gm.Expect(list.Total).To(gm.BeNumerically(">=", 2))

		names := make([]string, len(list.Groups))
		for i, g := range list.Groups {
			names[i] = g.Name
		}
		gm.Expect(names).To(gm.ContainElement("list-group-a"))
		gm.Expect(names).To(gm.ContainElement("list-group-b"))
	})

	// Given a group with filter "memory > 0" matching all VMs
	// When getting the group by ID
	// Then VMs should be returned in the response
	ginkgo.It("should get group with VMs", func() {
		group, err := agentSvc.CreateGroup("vms-group-v2", "memory > 0", "")
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		pageSize := 100
		resp, err := agentSvc.GetGroup(group.Id, nil, nil, &pageSize)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		ginkgo.GinkgoWriter.Printf("Group %s has %d VMs\n", resp.Group.Name, resp.Total)
		gm.Expect(resp.Group.Name).To(gm.Equal("vms-group-v2"))
		gm.Expect(resp.Total).To(gm.BeNumerically(">", 0))
		gm.Expect(len(resp.Vms)).To(gm.BeNumerically(">", 0))
	})

	// Given a created group
	// When updating the group name
	// Then the returned group should reflect the new name
	ginkgo.It("should update a group", func() {
		group, err := agentSvc.CreateGroup("original-v2", "memory > 0", "")
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		newName := "updated-v2"
		updated, err := agentSvc.UpdateGroup(group.Id, v2.UpdateGroupRequest{Name: &newName})
		gm.Expect(err).ToNot(gm.HaveOccurred())

		ginkgo.GinkgoWriter.Printf("Updated group: name=%s\n", updated.Name)
		gm.Expect(updated.Name).To(gm.Equal("updated-v2"))
		gm.Expect(updated.Filter).To(gm.Equal("memory > 0"))
	})

	// Given a created group
	// When deleting the group and then getting it
	// Then GetGroup should return a not found error
	ginkgo.It("should delete a group", func() {
		group, err := agentSvc.CreateGroup("to-delete-v2", "memory > 0", "")
		gm.Expect(err).ToNot(gm.HaveOccurred())

		status, err := agentSvc.DeleteGroup(group.Id)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		gm.Expect(status).To(gm.Equal(http.StatusNoContent))

		_, err = agentSvc.GetGroup(group.Id, nil, nil, nil)
		gm.Expect(err).To(gm.HaveOccurred())
		gm.Expect(err.Error()).To(gm.ContainSubstring("not found"))
	})

	// Given multiple groups with distinct names
	// When listing groups filtered by name
	// Then only the matching group should be returned
	ginkgo.It("should filter groups by name", func() {
		g1, err := agentSvc.CreateGroup("filter-target-v2", "memory > 0", "")
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(g1.Id) }()

		g2, err := agentSvc.CreateGroup("other-group-v2", "memory > 0", "")
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(g2.Id) }()

		byName := "filter-target-v2"
		list, err := agentSvc.ListGroups(&byName, nil, nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		ginkgo.GinkgoWriter.Printf("Filtered groups by name '%s': total=%d\n", byName, list.Total)
		gm.Expect(list.Total).To(gm.Equal(1))
		gm.Expect(list.Groups).To(gm.HaveLen(1))
		gm.Expect(list.Groups[0].Name).To(gm.Equal("filter-target-v2"))
	})

	// Given a group matching all VMs
	// When updating the group's filter to a narrower expression
	// Then the group's VM list should shrink accordingly
	ginkgo.It("should update group filter via PATCH and change VM list", func() {
		group, err := agentSvc.CreateGroup("filter-test-v2", "memory > 0", "")
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		pageSize := 100
		before, err := agentSvc.GetGroup(group.Id, nil, nil, &pageSize)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		gm.Expect(before.Total).To(gm.Equal(totalVMs))

		newFilter := "memory >= 32768"
		_, err = agentSvc.UpdateGroup(group.Id, v2.UpdateGroupRequest{Filter: &newFilter})
		gm.Expect(err).ToNot(gm.HaveOccurred())

		expectedAfter := countVMs(func(vm v2.VirtualMachine) bool { return vm.Memory >= 32768 })
		after, err := agentSvc.GetGroup(group.Id, nil, nil, &pageSize)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		gm.Expect(after.Total).To(gm.Equal(expectedAfter))
		gm.Expect(after.Total).To(gm.BeNumerically("<", totalVMs))
	})

	// Given no group exists with the given ID
	// When deleting that group
	// Then the delete should be idempotent and return 204
	ginkgo.It("should return 204 when deleting non-existent group", func() {
		nonExistentUUID := uuid.NewString()
		status, err := agentSvc.DeleteGroup(nonExistentUUID)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		gm.Expect(status).To(gm.Equal(http.StatusNoContent))
	})

	// -----------------------------------------------------------------
	// Validation
	// -----------------------------------------------------------------
	ginkgo.Context("validation", func() {
		ginkgo.It("should reject empty name", func() {
			body, _ := json.Marshal(v2.CreateGroupRequest{Name: "", Filter: "memory > 0"})
			status, err := agentSvc.CreateGroupRaw(body)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
		})

		ginkgo.It("should reject name longer than 100 characters", func() {
			longName := strings.Repeat("a", 101)
			body, _ := json.Marshal(v2.CreateGroupRequest{Name: longName, Filter: "memory > 0"})
			status, err := agentSvc.CreateGroupRaw(body)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
		})

		ginkgo.It("should reject empty filter", func() {
			body, _ := json.Marshal(v2.CreateGroupRequest{Name: "valid-name", Filter: ""})
			status, err := agentSvc.CreateGroupRaw(body)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
		})

		ginkgo.It("should reject invalid filter syntax", func() {
			body, _ := json.Marshal(v2.CreateGroupRequest{Name: "valid-name", Filter: "invalid %%% filter"})
			status, err := agentSvc.CreateGroupRaw(body)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
		})

		ginkgo.It("should reject PATCH with no fields", func() {
			group, err := agentSvc.CreateGroup("patch-test-v2", "memory > 0", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

			body, _ := json.Marshal(v2.UpdateGroupRequest{})
			status, err := agentSvc.UpdateGroupRaw(group.Id, body)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
		})

		ginkgo.It("should reject duplicate group name on create", func() {
			group, err := agentSvc.CreateGroup("unique-name-v2", "memory > 0", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

			body, _ := json.Marshal(v2.CreateGroupRequest{Name: "unique-name-v2", Filter: "memory > 0"})
			status, err := agentSvc.CreateGroupRaw(body)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
		})

		ginkgo.It("should reject duplicate group name on update", func() {
			group1, err := agentSvc.CreateGroup("first-name-v2", "memory > 0", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(group1.Id) }()

			group2, err := agentSvc.CreateGroup("second-name-v2", "memory > 0", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(group2.Id) }()

			newName := "second-name-v2"
			body, _ := json.Marshal(v2.UpdateGroupRequest{Name: &newName})
			status, err := agentSvc.UpdateGroupRaw(group1.Id, body)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
		})
	})

	// -----------------------------------------------------------------
	// Filter DSL via groups
	// -----------------------------------------------------------------
	ginkgo.Context("filter DSL via groups", func() {
		ginkgo.AfterEach(func() {
			list, err := agentSvc.ListGroups(nil, nil, nil)
			if err == nil {
				for _, g := range list.Groups {
					_, _ = agentSvc.DeleteGroup(g.Id)
				}
			}
		})

		ginkgo.It("should match all VMs with memory > 0", func() {
			group, err := agentSvc.CreateGroup("all-v2", "memory > 0", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, nil, nil, &pageSize)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			ginkgo.GinkgoWriter.Printf("Filter 'memory > 0': total=%d\n", resp.Total)
			gm.Expect(resp.Total).To(gm.Equal(totalVMs))
			gm.Expect(len(resp.Vms)).To(gm.Equal(totalVMs))
		})

		ginkgo.It("should filter by powerstate", func() {
			poweredOn := countVMs(func(vm v2.VirtualMachine) bool { return vm.VCenterState == "poweredOn" })
			ginkgo.GinkgoWriter.Printf("VMs with poweredOn: %d / %d\n", poweredOn, totalVMs)

			group, err := agentSvc.CreateGroup("powered-on-v2", "powerstate = 'poweredOn'", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, nil, nil, &pageSize)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			ginkgo.GinkgoWriter.Printf("Filter 'poweredOn': total=%d\n", resp.Total)
			gm.Expect(resp.Total).To(gm.Equal(poweredOn))
			for _, vm := range resp.Vms {
				gm.Expect(vm.VCenterState).To(gm.Equal("poweredOn"))
			}
		})

		ginkgo.It("should filter by cluster", func() {
			clusterName := firstCluster()
			expected := countVMs(func(vm v2.VirtualMachine) bool { return vm.Cluster == clusterName })
			ginkgo.GinkgoWriter.Printf("Using cluster: %s (expected %d)\n", clusterName, expected)

			group, err := agentSvc.CreateGroup("one-cluster-v2",
				fmt.Sprintf("cluster = '%s'", clusterName), "")
			gm.Expect(err).ToNot(gm.HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, nil, nil, &pageSize)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			ginkgo.GinkgoWriter.Printf("Filter cluster='%s': total=%d\n", clusterName, resp.Total)
			gm.Expect(resp.Total).To(gm.Equal(expected))
			for _, vm := range resp.Vms {
				gm.Expect(vm.Cluster).To(gm.Equal(clusterName))
			}
		})

		ginkgo.It("should filter by memory >= 32768", func() {
			expected := countVMs(func(vm v2.VirtualMachine) bool { return vm.Memory >= 32768 })

			group, err := agentSvc.CreateGroup("big-memory-v2", "memory >= 32768", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, nil, nil, &pageSize)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			ginkgo.GinkgoWriter.Printf("Filter memory>=32768: total=%d (expected %d)\n", resp.Total, expected)
			gm.Expect(resp.Total).To(gm.Equal(expected))
			for _, vm := range resp.Vms {
				gm.Expect(vm.Memory).To(gm.BeNumerically(">=", 32768))
			}
		})

		ginkgo.It("should filter by exact memory tier", func() {
			expected := countVMs(func(vm v2.VirtualMachine) bool { return vm.Memory == 131072 })

			group, err := agentSvc.CreateGroup("mem-128g-v2", "memory = 131072", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, nil, nil, &pageSize)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			ginkgo.GinkgoWriter.Printf("Filter memory=131072: total=%d (expected %d)\n", resp.Total, expected)
			gm.Expect(resp.Total).To(gm.Equal(expected))
			for _, vm := range resp.Vms {
				gm.Expect(vm.Memory).To(gm.Equal(int64(131072)))
			}
		})

		ginkgo.It("should filter by name", func() {
			vmName := allVMs[0].Name

			group, err := agentSvc.CreateGroup("single-vm-v2",
				fmt.Sprintf("name = '%s'", vmName), "")
			gm.Expect(err).ToNot(gm.HaveOccurred())

			resp, err := agentSvc.GetGroup(group.Id, nil, nil, nil)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			ginkgo.GinkgoWriter.Printf("Filter name='%s': total=%d\n", vmName, resp.Total)
			gm.Expect(resp.Total).To(gm.Equal(1))
			gm.Expect(resp.Vms[0].Name).To(gm.Equal(vmName))
		})

		ginkgo.It("should filter by firmware", func() {
			group, err := agentSvc.CreateGroup("bios-only-v2", "firmware = 'bios'", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, nil, nil, &pageSize)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			ginkgo.GinkgoWriter.Printf("Filter firmware='bios': total=%d\n", resp.Total)
			gm.Expect(resp.Total).To(gm.BeNumerically(">", 0))
		})

		ginkgo.It("should filter by cpus", func() {
			group, err := agentSvc.CreateGroup("cpu-4-v2", "cpus = 4", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, nil, nil, &pageSize)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			ginkgo.GinkgoWriter.Printf("Filter cpus=4: total=%d\n", resp.Total)
			gm.Expect(resp.Total).To(gm.BeNumerically(">", 0))
			gm.Expect(resp.Total).To(gm.BeNumerically("<", totalVMs))
		})

		ginkgo.It("should apply combined cross-table filter", func() {
			clusterName := firstCluster()
			expected := countVMs(func(vm v2.VirtualMachine) bool {
				return vm.Memory >= 32768 && vm.Cluster == clusterName
			})
			ginkgo.GinkgoWriter.Printf("Expect %d VMs with memory>=32768 in cluster %s\n", expected, clusterName)

			group, err := agentSvc.CreateGroup("combined-filter-v2",
				fmt.Sprintf("memory >= 32768 and cluster = '%s'", clusterName), "")
			gm.Expect(err).ToNot(gm.HaveOccurred())

			pageSize := 100
			resp, err := agentSvc.GetGroup(group.Id, nil, nil, &pageSize)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			ginkgo.GinkgoWriter.Printf("Combined filter: total=%d\n", resp.Total)
			gm.Expect(resp.Total).To(gm.Equal(expected))
			for _, vm := range resp.Vms {
				gm.Expect(vm.Memory).To(gm.BeNumerically(">=", 32768))
				gm.Expect(vm.Cluster).To(gm.Equal(clusterName))
			}
		})

		ginkgo.It("should return 0 VMs for non-matching filter", func() {
			group, err := agentSvc.CreateGroup("no-match-v2", "memory > 999999999", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())

			resp, err := agentSvc.GetGroup(group.Id, nil, nil, nil)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			gm.Expect(resp.Total).To(gm.Equal(0))
			gm.Expect(resp.Vms).To(gm.BeEmpty())
		})
	})

	// -----------------------------------------------------------------
	// Pagination on group VMs
	// -----------------------------------------------------------------
	ginkgo.Context("pagination", func() {
		var groupID string

		ginkgo.BeforeAll(func() {
			group, err := agentSvc.CreateGroup("paginate-group-v2", "memory > 0", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())
			groupID = group.Id
		})

		ginkgo.AfterAll(func() {
			if groupID != "" {
				_, _ = agentSvc.DeleteGroup(groupID)
			}
		})

		ginkgo.It("should return correct pagination metadata", func() {
			page := 1
			pageSize := 5
			resp, err := agentSvc.GetGroup(groupID, nil, &page, &pageSize)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			expectedPages := (totalVMs + pageSize - 1) / pageSize
			ginkgo.GinkgoWriter.Printf("Pagination: page=%d, pageCount=%d, total=%d, returned=%d\n",
				resp.Page, resp.PageCount, resp.Total, len(resp.Vms))
			gm.Expect(resp.Page).To(gm.Equal(1))
			gm.Expect(resp.Total).To(gm.Equal(totalVMs))
			gm.Expect(resp.PageCount).To(gm.Equal(expectedPages))
			gm.Expect(len(resp.Vms)).To(gm.Equal(pageSize))
		})

		ginkgo.It("should return different VMs on page 2", func() {
			page1 := 1
			page2 := 2
			pageSize := 5

			resp1, err := agentSvc.GetGroup(groupID, nil, &page1, &pageSize)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			resp2, err := agentSvc.GetGroup(groupID, nil, &page2, &pageSize)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			for _, vm1 := range resp1.Vms {
				for _, vm2 := range resp2.Vms {
					gm.Expect(vm1.Id).ToNot(gm.Equal(vm2.Id),
						fmt.Sprintf("VM %s appeared on both page 1 and 2", vm1.Name))
				}
			}
		})
	})

	// -----------------------------------------------------------------
	// Sort on group VMs
	// -----------------------------------------------------------------
	ginkgo.Context("sorting", func() {
		var groupID string

		ginkgo.BeforeAll(func() {
			group, err := agentSvc.CreateGroup("sort-group-v2", "memory > 0", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())
			groupID = group.Id
		})

		ginkgo.AfterAll(func() {
			if groupID != "" {
				_, _ = agentSvc.DeleteGroup(groupID)
			}
		})

		ginkgo.It("should sort by name ascending", func() {
			pageSize := 100
			resp, err := agentSvc.GetGroup(groupID, []string{"name:asc"}, nil, &pageSize)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(resp.Vms)).To(gm.BeNumerically(">", 1))

			for i := 1; i < len(resp.Vms); i++ {
				gm.Expect(resp.Vms[i-1].Name <= resp.Vms[i].Name).To(gm.BeTrue(),
					fmt.Sprintf("expected %s <= %s", resp.Vms[i-1].Name, resp.Vms[i].Name))
			}
		})

		ginkgo.It("should sort by memory descending", func() {
			pageSize := 100
			resp, err := agentSvc.GetGroup(groupID, []string{"memory:desc"}, nil, &pageSize)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(resp.Vms)).To(gm.Equal(totalVMs))

			for i := 1; i < len(resp.Vms); i++ {
				gm.Expect(resp.Vms[i-1].Memory).To(gm.BeNumerically(">=", resp.Vms[i].Memory),
					"VMs should be sorted by memory descending")
			}
		})
	})
})

var _ = ginkgo.Describe("Group membership v2 e2e tests", ginkgo.Ordered, func() {
	var (
		agentSvc *service.AgentSvc
		allVMs   []v2.VirtualMachine
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
			defer func() {
				_ = resp.Body.Close()
			}()
			if resp.StatusCode >= 500 {
				return fmt.Errorf("server error: %d", resp.StatusCode)
			}
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

		gm.Eventually(func() int {
			collections, err := agentSvc.ListCollections()
			if err != nil {
				return 0
			}
			return len(collections.Collections)
		}, 120*time.Second, 2*time.Second).Should(gm.BeNumerically(">", 0), "expected at least 1 collection")

		// Get all VMs for reference
		pageSize := 100
		result, err := agentSvc.ListLatestVMs(&service.VMListParams{PageSize: &pageSize})
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to list VMs after collection")
		allVMs = result.VirtualMachines
		gm.Expect(len(allVMs)).To(gm.Equal(50), "vcsim model should produce 50 VMs")

		ginkgo.GinkgoWriter.Println("Group membership v2 test setup complete")
	})

	ginkgo.AfterAll(func() {
		ginkgo.GinkgoWriter.Println("Cleaning up group membership v2 tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	// Given a group matching some VMs
	// When listing VMs
	// Then matching VMs should have the group name in their groups field
	ginkgo.It("should show group names on matching VMs", func() {
		firstCluster := allVMs[0].Cluster
		group, err := agentSvc.CreateGroup("cluster-group-v2",
			fmt.Sprintf("cluster = '%s'", firstCluster), "")
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		pageSize := 100
		result, err := agentSvc.ListLatestVMs(&service.VMListParams{PageSize: &pageSize})
		gm.Expect(err).ToNot(gm.HaveOccurred())

		for _, vm := range result.VirtualMachines {
			if vm.Cluster == firstCluster {
				gm.Expect(vm.Groups).ToNot(gm.BeNil(), "VM %s in cluster should have groups", vm.Name)
				gm.Expect(*vm.Groups).To(gm.ContainElement("cluster-group-v2"),
					"VM %s should be in cluster-group-v2", vm.Name)
			}
		}
	})

	// Given multiple groups matching the same VM
	// When listing VMs
	// Then the VM should show all matching group names
	ginkgo.It("should show all matching group names on VMs", func() {
		firstCluster := allVMs[0].Cluster

		group1, err := agentSvc.CreateGroup("group-1-v2",
			fmt.Sprintf("cluster = '%s'", firstCluster), "")
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group1.Id) }()

		group2, err := agentSvc.CreateGroup("group-2-v2", "memory > 0", "")
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group2.Id) }()

		pageSize := 100
		result, err := agentSvc.ListLatestVMs(&service.VMListParams{PageSize: &pageSize})
		gm.Expect(err).ToNot(gm.HaveOccurred())

		for _, vm := range result.VirtualMachines {
			if vm.Cluster == firstCluster {
				gm.Expect(vm.Groups).ToNot(gm.BeNil(), "VM %s should have groups", vm.Name)
				gm.Expect(*vm.Groups).To(gm.ContainElement("group-1-v2"),
					"VM %s should be in group-1-v2", vm.Name)
				gm.Expect(*vm.Groups).To(gm.ContainElement("group-2-v2"),
					"VM %s should be in group-2-v2", vm.Name)
			}
		}
	})

	// Given a group
	// When the group is deleted
	// Then VMs should no longer show that group name
	ginkgo.It("should remove group name when group is deleted", func() {
		group, err := agentSvc.CreateGroup("temp-group-v2", "memory > 0", "")
		gm.Expect(err).ToNot(gm.HaveOccurred())

		pageSize := 100
		result, err := agentSvc.ListLatestVMs(&service.VMListParams{PageSize: &pageSize})
		gm.Expect(err).ToNot(gm.HaveOccurred())

		groupedCount := 0
		for _, vm := range result.VirtualMachines {
			if vm.Groups != nil && len(*vm.Groups) > 0 {
				groupedCount++
			}
		}
		gm.Expect(groupedCount).To(gm.BeNumerically(">", 0), "some VMs should be in groups")

		_, err = agentSvc.DeleteGroup(group.Id)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		result, err = agentSvc.ListLatestVMs(&service.VMListParams{PageSize: &pageSize})
		gm.Expect(err).ToNot(gm.HaveOccurred())

		for _, vm := range result.VirtualMachines {
			if vm.Groups != nil {
				gm.Expect(*vm.Groups).ToNot(gm.ContainElement("temp-group-v2"),
					"VM %s should not be in temp-group-v2 after deletion", vm.Name)
			}
		}
	})
})
