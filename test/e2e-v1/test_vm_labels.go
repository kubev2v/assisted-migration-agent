package main

import (
	"crypto/tls"
	"net/http"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/google/uuid"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e-v1/service"
)

var _ = ginkgo.Describe("VM Labels e2e tests", ginkgo.Ordered, func() {
	var agentSvc *service.AgentSvc

	ginkgo.BeforeAll(func() {
		ginkgo.GinkgoWriter.Println("Starting postgres...")
		err := infraManager.StartPostgres()
		gomega.Expect(err).ToNot(gomega.HaveOccurred(), "failed to start postgres")
		time.Sleep(2 * time.Second)

		ginkgo.GinkgoWriter.Println("Starting vcsim...")
		err = infraManager.StartVcsim()
		gomega.Expect(err).ToNot(gomega.HaveOccurred(), "failed to start vcsim")

		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
		gomega.Eventually(func() error {
			resp, err := client.Get(infra.VcsimURL)
			if err != nil {
				return err
			}
			_ = resp.Body.Close()
			return nil
		}, 30*time.Second, 1*time.Second).Should(gomega.BeNil(), "vcsim did not become ready")

		agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)

		agentID := uuid.NewString()
		ginkgo.GinkgoWriter.Printf("Starting agent %s in disconnected mode...\n", agentID)
		_, err = infraManager.StartAgent(infra.AgentConfig{
			AgentID:        agentID,
			SourceID:       uuid.NewString(),
			Mode:           "disconnected",
			ConsoleURL:     cfg.AgentProxyUrl,
			UpdateInterval: "1s",
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred(), "failed to start agent")

		gomega.Eventually(func() error {
			_, err := agentSvc.Status()
			return err
		}, 30*time.Second, 1*time.Second).Should(gomega.BeNil(), "agent did not become ready")

		ginkgo.GinkgoWriter.Println("Starting collector...")
		_, err = agentSvc.StartCollector(infra.VcsimURL, infra.VcsimUsername, infra.VcsimPassword)
		gomega.Expect(err).ToNot(gomega.HaveOccurred(), "failed to start collector")

		gomega.Eventually(func() string {
			status, err := agentSvc.GetCollectorStatus()
			if err != nil {
				return "error"
			}
			ginkgo.GinkgoWriter.Printf("Collector status: %s\n", status.Status)
			return status.Status
		}, 120*time.Second, 2*time.Second).Should(gomega.Equal("collected"), "collector did not reach collected state")

		ginkgo.GinkgoWriter.Println("VM Labels test setup complete")
	})

	ginkgo.AfterAll(func() {
		ginkgo.GinkgoWriter.Println("Cleaning up VM labels tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	ginkgo.Context("VM Labels CRUD Operations", func() {
		var testVMID string

		ginkgo.BeforeEach(func() {
			// Get a VM ID from the collected inventory
			result, err := agentSvc.ListVMs(&service.VMListParams{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).ToNot(gomega.BeEmpty(), "no VMs available for testing")

			testVMID = result.Vms[0].Id
			ginkgo.GinkgoWriter.Printf("Using test VM: %s (%s)\n", testVMID, result.Vms[0].Name)

			// Clear any existing labels from previous tests
			err = agentSvc.UpdateVMLabels(testVMID, []string{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		// Given a VM exists with no labels
		// When I set labels via PATCH /vms/{id}
		// Then the VM should have those labels
		ginkgo.It("should successfully set labels on a VM", func() {
			// Act
			err := agentSvc.UpdateVMLabels(testVMID, []string{"production", "critical"})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), "failed to set labels on VM")

			// Verify via Get API
			vm, err := agentSvc.GetVM(testVMID)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(vm.Labels).ToNot(gomega.BeNil(), "Labels should not be nil")
			gomega.Expect(*vm.Labels).To(gomega.Equal([]string{"production", "critical"}))

			// Verify via List API
			result, err := agentSvc.ListVMs(&service.VMListParams{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			found := false
			for _, listedVM := range result.Vms {
				if listedVM.Id == testVMID {
					found = true
					gomega.Expect(listedVM.Labels).ToNot(gomega.BeNil(), "Labels should not be nil in list")
					gomega.Expect(*listedVM.Labels).To(gomega.Equal([]string{"production", "critical"}))
					break
				}
			}
			gomega.Expect(found).To(gomega.BeTrue(), "VM should appear in list")
		})

		// Given a VM exists with labels
		// When I update labels with different values
		// Then labels should be replaced (not appended)
		ginkgo.It("should replace labels (not append)", func() {
			// Arrange - set initial labels
			err := agentSvc.UpdateVMLabels(testVMID, []string{"old-label-1", "old-label-2"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Act - update with new labels
			err = agentSvc.UpdateVMLabels(testVMID, []string{"new-label"})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), "failed to update labels")

			vm, err := agentSvc.GetVM(testVMID)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(vm.Labels).ToNot(gomega.BeNil())
			gomega.Expect(*vm.Labels).To(gomega.Equal([]string{"new-label"}))
			gomega.Expect(*vm.Labels).NotTo(gomega.ContainElement("old-label-1"))
			gomega.Expect(*vm.Labels).NotTo(gomega.ContainElement("old-label-2"))
		})

		// Given a VM exists with labels
		// When I clear labels with empty array
		// Then the VM should have no labels
		ginkgo.It("should successfully clear labels with empty array", func() {
			// Arrange - set some labels first
			err := agentSvc.UpdateVMLabels(testVMID, []string{"label1", "label2"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Act - clear labels
			err = agentSvc.UpdateVMLabels(testVMID, []string{})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), "failed to clear labels")

			vm, err := agentSvc.GetVM(testVMID)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			// Labels can be nil or empty array when cleared
			if vm.Labels != nil {
				gomega.Expect(*vm.Labels).To(gomega.BeEmpty())
			}
		})

		// Given a non-existent VM ID
		// When I try to set labels
		// Then I should receive a 404 error
		ginkgo.It("should return error for non-existent VM", func() {
			// Act
			err := agentSvc.UpdateVMLabels("non-existent-vm-id", []string{"label"})

			// Assert
			gomega.Expect(err).To(gomega.HaveOccurred(), "should fail for non-existent VM")
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("VM not found"), "should return VM not found error")
		})

		// Given labels with special characters
		// When I set them on a VM
		// Then they should be stored and retrieved correctly
		ginkgo.It("should handle labels with special characters", func() {
			// Act
			labels := []string{"prod-server", "tier_1", "wave.2", "env:staging"}
			err := agentSvc.UpdateVMLabels(testVMID, labels)

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			vm, err := agentSvc.GetVM(testVMID)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(vm.Labels).ToNot(gomega.BeNil())
			gomega.Expect(*vm.Labels).To(gomega.ConsistOf(labels))
		})
	})

	ginkgo.Context("GetVMLabels endpoint - autocomplete support", func() {
		ginkgo.BeforeEach(func() {
			// Set up VMs with various labels for testing
			result, err := agentSvc.ListVMs(&service.VMListParams{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(len(result.Vms)).To(gomega.BeNumerically(">=", 5), "need at least 5 VMs for testing")

			// Clear all labels first
			for _, vm := range result.Vms {
				_ = agentSvc.UpdateVMLabels(vm.Id, []string{})
			}

			// Set up test data
			_ = agentSvc.UpdateVMLabels(result.Vms[0].Id, []string{"production", "critical"})
			_ = agentSvc.UpdateVMLabels(result.Vms[1].Id, []string{"production", "database"})
			_ = agentSvc.UpdateVMLabels(result.Vms[2].Id, []string{"staging", "test"})
			_ = agentSvc.UpdateVMLabels(result.Vms[3].Id, []string{"prod-cluster", "cache"})
			_ = agentSvc.UpdateVMLabels(result.Vms[4].Id, []string{"prod-database", "critical"})

			// Let the database settle
			time.Sleep(500 * time.Millisecond)
		})

		// Given multiple VMs with various labels
		// When I call GET /vms/labels
		// Then I should get all distinct labels with their counts
		ginkgo.It("should return all distinct labels across all VMs with counts", func() {
			// Act
			response, err := agentSvc.GetVMLabels()

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(response.Labels).To(gomega.ContainElements(
				"production", "critical", "database", "staging", "test",
				"prod-cluster", "cache", "prod-database",
			))
			// Should be sorted alphabetically
			gomega.Expect(response.Labels).To(gomega.Equal([]string{
				"cache", "critical", "database", "prod-cluster", "prod-database", "production", "staging", "test",
			}))
			// Counts should have the same length as labels
			gomega.Expect(response.Counts).To(gomega.HaveLen(len(response.Labels)))
			// Each count should be at least 1
			for i, count := range response.Counts {
				gomega.Expect(count).To(gomega.BeNumerically(">=", 1), "label %s should have count >= 1", response.Labels[i])
			}
		})

		// Given multiple VMs with duplicate labels
		// When I call GET /vms/labels
		// Then each label should appear only once
		ginkgo.It("should return distinct labels (no duplicates)", func() {
			// Act
			response, err := agentSvc.GetVMLabels()

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Check for uniqueness
			seen := make(map[string]bool)
			for _, label := range response.Labels {
				gomega.Expect(seen[label]).To(gomega.BeFalse(), "label %s appears multiple times", label)
				seen[label] = true
			}
		})

		// Given VMs with specific label distributions
		// When I call GET /vms/labels
		// Then counts should accurately reflect VM distribution
		ginkgo.It("should return accurate counts reflecting actual VM label usage", func() {
			// Arrange - Get VMs and set specific label patterns
			result, err := agentSvc.ListVMs(&service.VMListParams{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(len(result.Vms)).To(gomega.BeNumerically(">=", 5), "need at least 5 VMs")

			// Clear all labels first
			for _, vm := range result.Vms {
				_ = agentSvc.UpdateVMLabels(vm.Id, []string{})
			}
			time.Sleep(300 * time.Millisecond)

			// Set up known label distribution:
			// - "common": 3 VMs
			// - "rare": 1 VM
			// - "shared": 2 VMs
			err = agentSvc.UpdateVMLabels(result.Vms[0].Id, []string{"common", "shared"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			err = agentSvc.UpdateVMLabels(result.Vms[1].Id, []string{"common", "shared"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			err = agentSvc.UpdateVMLabels(result.Vms[2].Id, []string{"common"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			err = agentSvc.UpdateVMLabels(result.Vms[3].Id, []string{"rare"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			time.Sleep(500 * time.Millisecond)

			// Act
			response, err := agentSvc.GetVMLabels()

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Build a map for easier validation
			labelCounts := make(map[string]int)
			for i, label := range response.Labels {
				labelCounts[label] = response.Counts[i]
			}

			// Verify specific counts
			gomega.Expect(labelCounts["common"]).To(gomega.Equal(3), "common label should have 3 VMs")
			gomega.Expect(labelCounts["rare"]).To(gomega.Equal(1), "rare label should have 1 VM")
			gomega.Expect(labelCounts["shared"]).To(gomega.Equal(2), "shared label should have 2 VMs")
		})

		// Given labels are added and removed dynamically
		// When I call GET /vms/labels
		// Then counts should update accordingly
		ginkgo.It("should update counts when labels are added or removed", func() {
			// Arrange - Get VMs
			result, err := agentSvc.ListVMs(&service.VMListParams{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(len(result.Vms)).To(gomega.BeNumerically(">=", 3), "need at least 3 VMs")

			// Clear all labels
			for _, vm := range result.Vms {
				_ = agentSvc.UpdateVMLabels(vm.Id, []string{})
			}
			time.Sleep(300 * time.Millisecond)

			// Add "dynamic" label to 1 VM
			err = agentSvc.UpdateVMLabels(result.Vms[0].Id, []string{"dynamic"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			time.Sleep(300 * time.Millisecond)

			// Verify count is 1
			response, err := agentSvc.GetVMLabels()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			labelCounts := make(map[string]int)
			for i, label := range response.Labels {
				labelCounts[label] = response.Counts[i]
			}
			gomega.Expect(labelCounts["dynamic"]).To(gomega.Equal(1))

			// Add "dynamic" label to 2 more VMs
			err = agentSvc.UpdateVMLabels(result.Vms[1].Id, []string{"dynamic"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			err = agentSvc.UpdateVMLabels(result.Vms[2].Id, []string{"dynamic"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			time.Sleep(300 * time.Millisecond)

			// Verify count is now 3
			response, err = agentSvc.GetVMLabels()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			labelCounts = make(map[string]int)
			for i, label := range response.Labels {
				labelCounts[label] = response.Counts[i]
			}
			gomega.Expect(labelCounts["dynamic"]).To(gomega.Equal(3))

			// Remove label from 1 VM
			err = agentSvc.UpdateVMLabels(result.Vms[0].Id, []string{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			time.Sleep(300 * time.Millisecond)

			// Verify count is now 2
			response, err = agentSvc.GetVMLabels()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			labelCounts = make(map[string]int)
			for i, label := range response.Labels {
				labelCounts[label] = response.Counts[i]
			}
			gomega.Expect(labelCounts["dynamic"]).To(gomega.Equal(2))
		})

		// Given labels and counts arrays
		// When I call GET /vms/labels
		// Then arrays should have matching lengths
		ginkgo.It("should always return labels and counts arrays of same length", func() {
			// Act
			response, err := agentSvc.GetVMLabels()

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(len(response.Labels)).To(gomega.Equal(len(response.Counts)),
				"labels and counts arrays must have the same length")

			// Verify all counts are positive
			for i, count := range response.Counts {
				gomega.Expect(count).To(gomega.BeNumerically(">", 0),
					"count for label %s should be positive", response.Labels[i])
			}
		})

		// Given batch label operations
		// When I call GET /vms/labels
		// Then counts should reflect all changes
		ginkgo.It("should accurately count after batch label operations", func() {
			// Arrange - Get VMs
			result, err := agentSvc.ListVMs(&service.VMListParams{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(len(result.Vms)).To(gomega.BeNumerically(">=", 4), "need at least 4 VMs")

			// Clear all labels
			for _, vm := range result.Vms {
				_ = agentSvc.UpdateVMLabels(vm.Id, []string{})
			}
			time.Sleep(300 * time.Millisecond)

			// Batch operation: add "batch-label" to multiple VMs at once
			vm1ID := result.Vms[0].Id
			vm2ID := result.Vms[1].Id
			vm3ID := result.Vms[2].Id
			vm4ID := result.Vms[3].Id

			err = agentSvc.UpdateLabelVMs("batch-label", []string{vm1ID, vm2ID, vm3ID}, nil)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			time.Sleep(300 * time.Millisecond)

			// Verify count is 3
			response, err := agentSvc.GetVMLabels()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			labelCounts := make(map[string]int)
			for i, label := range response.Labels {
				labelCounts[label] = response.Counts[i]
			}
			gomega.Expect(labelCounts["batch-label"]).To(gomega.Equal(3))

			// Add one more VM to the label
			err = agentSvc.UpdateLabelVMs("batch-label", []string{vm4ID}, nil)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			time.Sleep(300 * time.Millisecond)

			// Verify count is now 4
			response, err = agentSvc.GetVMLabels()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			labelCounts = make(map[string]int)
			for i, label := range response.Labels {
				labelCounts[label] = response.Counts[i]
			}
			gomega.Expect(labelCounts["batch-label"]).To(gomega.Equal(4))

			// Batch remove 2 VMs
			err = agentSvc.UpdateLabelVMs("batch-label", nil, []string{vm1ID, vm2ID})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			time.Sleep(300 * time.Millisecond)

			// Verify count is now 2
			response, err = agentSvc.GetVMLabels()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			labelCounts = make(map[string]int)
			for i, label := range response.Labels {
				labelCounts[label] = response.Counts[i]
			}
			gomega.Expect(labelCounts["batch-label"]).To(gomega.Equal(2))
		})
	})

	ginkgo.Context("Filtering VMs by labels using contains operator", func() {
		var vmIDs map[string]string // label description -> VM ID

		ginkgo.BeforeEach(func() {
			result, err := agentSvc.ListVMs(&service.VMListParams{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(len(result.Vms)).To(gomega.BeNumerically(">=", 6), "need at least 6 VMs for testing")

			// Clear all labels first
			for _, vm := range result.Vms {
				_ = agentSvc.UpdateVMLabels(vm.Id, []string{})
			}

			vmIDs = make(map[string]string)

			// Set up VMs with specific label combinations
			vmIDs["prod-critical"] = result.Vms[0].Id
			_ = agentSvc.UpdateVMLabels(vmIDs["prod-critical"], []string{"production", "critical", "wave-1"})

			vmIDs["prod-db"] = result.Vms[1].Id
			_ = agentSvc.UpdateVMLabels(vmIDs["prod-db"], []string{"production", "database"})

			vmIDs["staging-worker"] = result.Vms[2].Id
			_ = agentSvc.UpdateVMLabels(vmIDs["staging-worker"], []string{"staging", "worker"})

			vmIDs["staging-critical"] = result.Vms[3].Id
			_ = agentSvc.UpdateVMLabels(vmIDs["staging-critical"], []string{"staging", "critical"})

			vmIDs["test"] = result.Vms[4].Id
			_ = agentSvc.UpdateVMLabels(vmIDs["test"], []string{"test", "temporary"})

			vmIDs["no-labels"] = result.Vms[5].Id
			// Intentionally no labels

			// Let the database settle
			time.Sleep(500 * time.Millisecond)

			ginkgo.GinkgoWriter.Printf("Set up test VMs with labels\n")
		})

		// Given VMs with "production" label
		// When I filter by "labels contains 'production'"
		// Then only VMs with production label should be returned
		ginkgo.It("should find VMs with 'production' label", func() {
			// Act
			expression := "labels contains 'production'"
			result, err := agentSvc.ListVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).To(gomega.HaveLen(2))

			ids := []string{}
			for _, vm := range result.Vms {
				ids = append(ids, vm.Id)
			}
			gomega.Expect(ids).To(gomega.ConsistOf(vmIDs["prod-critical"], vmIDs["prod-db"]))
		})

		// Given VMs with "critical" label
		// When I filter by "labels contains 'critical'"
		// Then only VMs with critical label should be returned
		ginkgo.It("should find VMs with 'critical' label", func() {
			// Act
			expression := "labels contains 'critical'"
			result, err := agentSvc.ListVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).To(gomega.HaveLen(2))

			ids := []string{}
			for _, vm := range result.Vms {
				ids = append(ids, vm.Id)
			}
			gomega.Expect(ids).To(gomega.ConsistOf(vmIDs["prod-critical"], vmIDs["staging-critical"]))
		})

		// Given VMs exist
		// When I filter by "labels not contains 'production'"
		// Then VMs without production label should be returned
		ginkgo.It("should find VMs without 'production' label", func() {
			// Act
			expression := "labels not contains 'production'"
			result, err := agentSvc.ListVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).To(gomega.Not(gomega.BeEmpty()))

			// Verify none of the returned VMs have the production label
			for _, vm := range result.Vms {
				if vm.Labels != nil {
					gomega.Expect(*vm.Labels).NotTo(gomega.ContainElement("production"))
				}
			}

			// Our specific test VMs without production
			ids := []string{}
			for _, vm := range result.Vms {
				if vm.Id == vmIDs["staging-worker"] || vm.Id == vmIDs["staging-critical"] ||
					vm.Id == vmIDs["test"] || vm.Id == vmIDs["no-labels"] {
					ids = append(ids, vm.Id)
				}
			}
			gomega.Expect(ids).To(gomega.ContainElements(
				vmIDs["staging-worker"], vmIDs["staging-critical"], vmIDs["test"], vmIDs["no-labels"],
			))
		})

		// Given VMs with various labels
		// When I combine "labels contains 'production' and labels contains 'critical'"
		// Then only VMs with both labels should be returned
		ginkgo.It("should support AND combination with multiple contains", func() {
			// Act
			expression := "labels contains 'production' and labels contains 'critical'"
			result, err := agentSvc.ListVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).To(gomega.HaveLen(1))
			gomega.Expect(result.Vms[0].Id).To(gomega.Equal(vmIDs["prod-critical"]))
		})

		// Given VMs with various labels
		// When I use "labels contains 'production' or labels contains 'staging'"
		// Then VMs with either label should be returned
		ginkgo.It("should support OR combination with contains", func() {
			// Act
			expression := "labels contains 'production' or labels contains 'staging'"
			result, err := agentSvc.ListVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).To(gomega.HaveLen(4))

			ids := []string{}
			for _, vm := range result.Vms {
				ids = append(ids, vm.Id)
			}
			gomega.Expect(ids).To(gomega.ConsistOf(
				vmIDs["prod-critical"], vmIDs["prod-db"],
				vmIDs["staging-worker"], vmIDs["staging-critical"],
			))
		})

		// Given VMs with various labels
		// When I use "labels contains 'production' and labels not contains 'critical'"
		// Then only production VMs without critical label should be returned
		ginkgo.It("should support mixing contains and not contains", func() {
			// Act
			expression := "labels contains 'production' and labels not contains 'critical'"
			result, err := agentSvc.ListVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).To(gomega.HaveLen(1))
			gomega.Expect(result.Vms[0].Id).To(gomega.Equal(vmIDs["prod-db"]))
		})

		// Given a label that doesn't exist
		// When I filter by that label
		// Then no VMs should be returned
		ginkgo.It("should return empty result for non-existent label", func() {
			// Act
			expression := "labels contains 'nonexistent'"
			result, err := agentSvc.ListVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).To(gomega.BeEmpty())
		})
	})

	ginkgo.Context("Combining label filters with other filters", func() {
		var testCluster string
		var vmWithLabelInCluster string
		var vmNoLabelInCluster string

		ginkgo.BeforeEach(func() {
			result, err := agentSvc.ListVMs(&service.VMListParams{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Group VMs by cluster
			clusterVMs := make(map[string][]v1.VirtualMachine)
			for _, vm := range result.Vms {
				clusterVMs[vm.Cluster] = append(clusterVMs[vm.Cluster], vm)
			}

			// Find a cluster with at least 2 VMs
			for cluster, vms := range clusterVMs {
				if len(vms) >= 2 {
					testCluster = cluster
					vmWithLabelInCluster = vms[0].Id
					vmNoLabelInCluster = vms[1].Id
					break
				}
			}

			gomega.Expect(testCluster).ToNot(gomega.BeEmpty(), "need a cluster with at least 2 VMs")

			// Set up labels
			err = agentSvc.UpdateVMLabels(vmWithLabelInCluster, []string{"production", "web"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			err = agentSvc.UpdateVMLabels(vmNoLabelInCluster, []string{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Let the database settle
			time.Sleep(500 * time.Millisecond)

			ginkgo.GinkgoWriter.Printf("Test cluster: %s, VM with labels: %s, VM without labels: %s\n",
				testCluster, vmWithLabelInCluster, vmNoLabelInCluster)
		})

		// Given VMs in a cluster with various labels
		// When I filter by cluster AND labels
		// Then only matching VMs should be returned
		ginkgo.It("should combine cluster filter with labels filter", func() {
			// Act
			expression := `cluster = "` + testCluster + `" and labels contains 'production'`
			result, err := agentSvc.ListVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).ToNot(gomega.BeEmpty())

			// Verify all returned VMs are in the cluster and have the label
			for _, vm := range result.Vms {
				gomega.Expect(vm.Cluster).To(gomega.Equal(testCluster))
				gomega.Expect(vm.Labels).ToNot(gomega.BeNil())
				gomega.Expect(*vm.Labels).To(gomega.ContainElement("production"))
			}

			// Verify our specific VM is in the results
			foundVM := false
			for _, vm := range result.Vms {
				if vm.Id == vmWithLabelInCluster {
					foundVM = true
					break
				}
			}
			gomega.Expect(foundVM).To(gomega.BeTrue(), "VM with label should be in results")
		})
	})

	ginkgo.Context("Validation and error cases", func() {
		var testVMID string

		ginkgo.BeforeEach(func() {
			result, err := agentSvc.ListVMs(&service.VMListParams{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).ToNot(gomega.BeEmpty())
			testVMID = result.Vms[0].Id
		})

		// Given a label exceeds maximum length (100 chars)
		// When I try to set it
		// Then I should receive a 400 Bad Request error
		ginkgo.It("should reject labels exceeding 100 characters", func() {
			// Arrange - create a 101 character label
			longLabel := strings.Repeat("a", 101)

			// Act
			err := agentSvc.UpdateVMLabels(testVMID, []string{"valid", longLabel})

			// Assert
			gomega.Expect(err).To(gomega.HaveOccurred(), "should fail for label exceeding max length")
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("400"), "should return 400 Bad Request")
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("maximum string length is 100"), "should include validation message")
		})

		// Given a label is exactly 100 characters
		// When I set it
		// Then it should succeed
		ginkgo.It("should accept labels with exactly 100 characters", func() {
			// Arrange - create a 100 character label
			maxLabel := strings.Repeat("a", 100)

			// Act
			err := agentSvc.UpdateVMLabels(testVMID, []string{maxLabel})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			vm, err := agentSvc.GetVM(testVMID)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(vm.Labels).ToNot(gomega.BeNil())
			gomega.Expect(*vm.Labels).To(gomega.HaveLen(1))
			gomega.Expect((*vm.Labels)[0]).To(gomega.Equal(maxLabel))
		})

		// Given a label is an empty string or whitespace-only
		// When I try to set it
		// Then I should receive a 400 Bad Request error
		ginkgo.It("should reject empty or whitespace-only labels", func() {
			// Act - try with empty string
			err := agentSvc.UpdateVMLabels(testVMID, []string{"valid", ""})

			// Assert
			gomega.Expect(err).To(gomega.HaveOccurred(), "should fail for empty label")
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("400"), "should return 400 Bad Request")
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("minimum string length is 1"), "should include validation message")

			// Act - try with whitespace-only
			err = agentSvc.UpdateVMLabels(testVMID, []string{"valid", "   "})

			// Assert
			gomega.Expect(err).To(gomega.HaveOccurred(), "should fail for whitespace-only label")
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("400"), "should return 400 Bad Request")
		})
	})

	ginkgo.Context("Batch Label Operations via PATCH", func() {
		var testVMIDs []string

		ginkgo.BeforeEach(func() {
			// Get multiple VM IDs from the collected inventory
			result, err := agentSvc.ListVMs(&service.VMListParams{PageSize: intPtr(10)})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(len(result.Vms)).To(gomega.BeNumerically(">=", 3), "need at least 3 VMs for testing")

			testVMIDs = []string{result.Vms[0].Id, result.Vms[1].Id, result.Vms[2].Id}
			ginkgo.GinkgoWriter.Printf("Using test VMs: %v\n", testVMIDs)

			// Clear any existing labels from previous tests
			for _, vmID := range testVMIDs {
				_ = agentSvc.UpdateVMLabels(vmID, []string{})
			}
		})

		// Given multiple VMs exist
		// When I add a label to them via PATCH
		// Then all VMs should have the label (operation is atomic)
		ginkgo.It("should add label to multiple VMs via PATCH", func() {
			// Act
			err := agentSvc.UpdateLabelVMs("batch-test", testVMIDs, []string{})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Verify all VMs have the label
			for _, vmID := range testVMIDs {
				vm, err := agentSvc.GetVM(vmID)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(vm.Labels).ToNot(gomega.BeNil())
				gomega.Expect(*vm.Labels).To(gomega.ContainElement("batch-test"))
			}
		})

		// Given multiple VMs have a label
		// When I remove the label via PATCH
		// Then the label should be removed from all VMs (operation is atomic)
		ginkgo.It("should remove label from multiple VMs via PATCH", func() {
			// Arrange - Add label to all VMs first
			for _, vmID := range testVMIDs {
				err := agentSvc.UpdateVMLabels(vmID, []string{"to-remove", "keep-this"})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			// Act
			err := agentSvc.UpdateLabelVMs("to-remove", []string{}, testVMIDs)

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Verify label removed from all VMs
			for _, vmID := range testVMIDs {
				vm, err := agentSvc.GetVM(vmID)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(vm.Labels).ToNot(gomega.BeNil())
				gomega.Expect(*vm.Labels).To(gomega.Equal([]string{"keep-this"}))
			}
		})

		// Given VMs have different labels
		// When I add and remove in single PATCH request
		// Then both operations should succeed atomically
		ginkgo.It("should add and remove in single PATCH request", func() {
			// Arrange
			err := agentSvc.UpdateVMLabels(testVMIDs[0], []string{"old-label"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			err = agentSvc.UpdateVMLabels(testVMIDs[1], []string{"old-label"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Act - Add to vm-3, remove from vm-1 and vm-2
			err = agentSvc.UpdateLabelVMs(
				"new-label",
				[]string{testVMIDs[2]},               // add
				[]string{testVMIDs[0], testVMIDs[1]}, // remove (even though they don't have it yet)
			)

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Verify vm-3 has new-label
			vm, err := agentSvc.GetVM(testVMIDs[2])
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(*vm.Labels).To(gomega.ContainElement("new-label"))

			// Verify vm-1 and vm-2 don't have new-label (idempotent remove)
			vm1, _ := agentSvc.GetVM(testVMIDs[0])
			gomega.Expect(*vm1.Labels).ToNot(gomega.ContainElement("new-label"))
		})

		// Given some VMs don't exist
		// When I try to add a label via PATCH
		// Then it should fail atomically (all-or-nothing)
		ginkgo.It("should rollback all changes if any VM doesn't exist", func() {
			// Act - Include non-existent VM ID
			err := agentSvc.UpdateLabelVMs(
				"test-label",
				append(testVMIDs, "non-existent-vm-999"),
				[]string{},
			)

			// Assert - Should fail completely
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("404"))

			// Verify NO VMs got the label (transaction rolled back)
			for _, vmID := range testVMIDs {
				vm, err := agentSvc.GetVM(vmID)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				if vm.Labels != nil {
					gomega.Expect(*vm.Labels).ToNot(gomega.ContainElement("test-label"))
				}
			}
		})

		// Given neither add nor remove arrays are provided
		// When I send PATCH request
		// Then it should return 400 Bad Request
		ginkgo.It("should reject PATCH request with neither add nor remove", func() {
			// Act
			err := agentSvc.UpdateLabelVMs("test-label", []string{}, []string{})

			// Assert
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("400"))
		})

		// Given adding an existing label (idempotency test)
		// When I add the same label again
		// Then it should succeed without creating duplicates
		ginkgo.It("should be idempotent when adding existing label", func() {
			// Arrange - Add label first time
			err := agentSvc.UpdateVMLabels(testVMIDs[0], []string{"duplicate-test"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Act - Add same label again via PATCH
			err = agentSvc.UpdateLabelVMs("duplicate-test", []string{testVMIDs[0]}, []string{})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Verify no duplicates
			vm, err := agentSvc.GetVM(testVMIDs[0])
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(*vm.Labels).To(gomega.Equal([]string{"duplicate-test"}))
		})
	})

	ginkgo.Context("Global Label Deletion via DELETE", func() {
		var testVMIDs []string

		ginkgo.BeforeEach(func() {
			// Get multiple VM IDs
			result, err := agentSvc.ListVMs(&service.VMListParams{PageSize: intPtr(5)})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(len(result.Vms)).To(gomega.BeNumerically(">=", 3))

			testVMIDs = []string{result.Vms[0].Id, result.Vms[1].Id, result.Vms[2].Id}

			// Clear labels
			for _, vmID := range testVMIDs {
				_ = agentSvc.UpdateVMLabels(vmID, []string{})
			}
		})

		// Given multiple VMs have the same label
		// When I delete the label globally
		// Then it should be removed from all VMs
		ginkgo.It("should delete label from all VMs globally", func() {
			// Arrange - Add same label to all VMs
			for _, vmID := range testVMIDs {
				err := agentSvc.UpdateVMLabels(vmID, []string{"global-delete-test", "keep-this"})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			// Act
			result, err := agentSvc.DeleteLabelGlobally("global-delete-test")

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Affected).To(gomega.Equal(3))
			gomega.Expect(result.Label).To(gomega.Equal("global-delete-test"))

			// Verify label removed from all VMs
			for _, vmID := range testVMIDs {
				vm, err := agentSvc.GetVM(vmID)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(*vm.Labels).To(gomega.Equal([]string{"keep-this"}))
			}
		})

		// Given no VMs have the label
		// When I delete the label globally
		// Then it should return 0 affected
		ginkgo.It("should return 0 affected when label doesn't exist", func() {
			// Act
			result, err := agentSvc.DeleteLabelGlobally("non-existent-label")

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Affected).To(gomega.Equal(0))
			gomega.Expect(result.Label).To(gomega.Equal("non-existent-label"))
		})

		// Given a VM has the label as its only label
		// When I delete the label globally
		// Then the VM should have empty labels array
		ginkgo.It("should leave empty array when removing last label", func() {
			// Arrange
			err := agentSvc.UpdateVMLabels(testVMIDs[0], []string{"only-label"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Act
			result, err := agentSvc.DeleteLabelGlobally("only-label")

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Affected).To(gomega.Equal(1))

			vm, err := agentSvc.GetVM(testVMIDs[0])
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			// Labels can be nil or empty array when all labels are removed
			if vm.Labels != nil {
				gomega.Expect(*vm.Labels).To(gomega.BeEmpty())
			}
		})
	})
})

func intPtr(i int) *int {
	return &i
}
