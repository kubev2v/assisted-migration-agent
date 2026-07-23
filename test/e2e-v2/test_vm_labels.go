package main

import (
	"crypto/tls"
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

var _ = ginkgo.Describe("VM Labels v2 e2e tests", ginkgo.Ordered, func() {
	var agentSvc *service.AgentSvc

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

		ginkgo.GinkgoWriter.Println("VM Labels v2 test setup complete")
	})

	ginkgo.AfterAll(func() {
		ginkgo.GinkgoWriter.Println("Cleaning up VM labels v2 tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	// vmHasLabel returns true if the given VM appears in a labels-contains filter result.
	vmHasLabel := func(vmID, label string) bool {
		expression := fmt.Sprintf("labels contains '%s'", label)
		result, err := agentSvc.ListLatestVMs(&service.VMListParams{
			ByExpression: &expression,
		})
		if err != nil {
			return false
		}
		for _, vm := range result.VirtualMachines {
			if vm.Id == vmID {
				return true
			}
		}
		return false
	}

	ginkgo.Context("VM Labels CRUD Operations", func() {
		var testVMID string

		ginkgo.BeforeEach(func() {
			// Get a VM ID from the collected inventory
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).ToNot(gm.BeEmpty(), "no VMs available for testing")

			testVMID = result.VirtualMachines[0].Id
			ginkgo.GinkgoWriter.Printf("Using test VM: %s (%s)\n", testVMID, result.VirtualMachines[0].Name)

			// Clear any existing labels from previous tests
			err = agentSvc.UpdateVMLabels(testVMID, []string{})
			gm.Expect(err).ToNot(gm.HaveOccurred())
		})

		// Given a VM exists with no labels
		// When I set labels via PATCH /virtualmachines/{id}
		// Then the VM should have those labels
		ginkgo.It("should successfully set labels on a VM", func() {
			// Act
			err := agentSvc.UpdateVMLabels(testVMID, []string{"production", "critical"})

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to set labels on VM")

			// Verify via GetVMLabels API
			response, err := agentSvc.GetVMLabels()
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(response.Labels).To(gm.ContainElement("production"))
			gm.Expect(response.Labels).To(gm.ContainElement("critical"))

			// Verify via filter that the VM has both labels
			gm.Expect(vmHasLabel(testVMID, "production")).To(gm.BeTrue(), "VM should have production label")
			gm.Expect(vmHasLabel(testVMID, "critical")).To(gm.BeTrue(), "VM should have critical label")
		})

		// Given a VM exists with labels
		// When I update labels with different values
		// Then labels should be replaced (not appended)
		ginkgo.It("should replace labels (not append)", func() {
			// Arrange - set initial labels
			err := agentSvc.UpdateVMLabels(testVMID, []string{"old-label-1", "old-label-2"})
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Act - update with new labels
			err = agentSvc.UpdateVMLabels(testVMID, []string{"new-label"})

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to update labels")

			// Verify via filter that VM has new label but not old ones
			gm.Expect(vmHasLabel(testVMID, "new-label")).To(gm.BeTrue(), "VM should have new-label")
			gm.Expect(vmHasLabel(testVMID, "old-label-1")).To(gm.BeFalse(), "VM should not have old-label-1")
			gm.Expect(vmHasLabel(testVMID, "old-label-2")).To(gm.BeFalse(), "VM should not have old-label-2")
		})

		// Given a VM exists with labels
		// When I clear labels with empty array
		// Then the VM should have no labels
		ginkgo.It("should successfully clear labels with empty array", func() {
			// Arrange - set some labels first
			err := agentSvc.UpdateVMLabels(testVMID, []string{"label1", "label2"})
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Act - clear labels
			err = agentSvc.UpdateVMLabels(testVMID, []string{})

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to clear labels")

			// Verify via filter that VM no longer has those labels
			gm.Expect(vmHasLabel(testVMID, "label1")).To(gm.BeFalse(), "VM should not have label1 after clearing")
			gm.Expect(vmHasLabel(testVMID, "label2")).To(gm.BeFalse(), "VM should not have label2 after clearing")
		})

		// Given a non-existent VM ID
		// When I try to set labels
		// Then I should receive a 404 error
		ginkgo.It("should return error for non-existent VM", func() {
			// Act
			err := agentSvc.UpdateVMLabels("non-existent-vm-id", []string{"label"})

			// Assert
			gm.Expect(err).To(gm.HaveOccurred(), "should fail for non-existent VM")
			gm.Expect(err.Error()).To(gm.ContainSubstring("VM not found"), "should return VM not found error")
		})

		// Given labels with special characters
		// When I set them on a VM
		// Then they should be stored and retrieved correctly
		ginkgo.It("should handle labels with special characters", func() {
			// Act
			labels := []string{"prod-server", "tier_1", "wave.2", "env:staging"}
			err := agentSvc.UpdateVMLabels(testVMID, labels)

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Verify all labels are present via filter
			for _, label := range labels {
				gm.Expect(vmHasLabel(testVMID, label)).To(gm.BeTrue(),
					fmt.Sprintf("VM should have label %q", label))
			}

			// Verify via GetVMLabels API
			response, err := agentSvc.GetVMLabels()
			gm.Expect(err).ToNot(gm.HaveOccurred())
			for _, label := range labels {
				gm.Expect(response.Labels).To(gm.ContainElement(label))
			}
		})
	})

	ginkgo.Context("GetVMLabels endpoint - autocomplete support", func() {
		ginkgo.BeforeEach(func() {
			// Set up VMs with various labels for testing
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(result.VirtualMachines)).To(gm.BeNumerically(">=", 5), "need at least 5 VMs for testing")

			// Clear all labels first
			for _, vm := range result.VirtualMachines {
				_ = agentSvc.UpdateVMLabels(vm.Id, []string{})
			}

			// Set up test data
			_ = agentSvc.UpdateVMLabels(result.VirtualMachines[0].Id, []string{"production", "critical"})
			_ = agentSvc.UpdateVMLabels(result.VirtualMachines[1].Id, []string{"production", "database"})
			_ = agentSvc.UpdateVMLabels(result.VirtualMachines[2].Id, []string{"staging", "test"})
			_ = agentSvc.UpdateVMLabels(result.VirtualMachines[3].Id, []string{"prod-cluster", "cache"})
			_ = agentSvc.UpdateVMLabels(result.VirtualMachines[4].Id, []string{"prod-database", "critical"})

			// Let the database settle
			time.Sleep(500 * time.Millisecond)
		})

		// Given multiple VMs with various labels
		// When I call GET /virtualmachines/labels
		// Then I should get all distinct labels with their counts
		ginkgo.It("should return all distinct labels across all VMs with counts", func() {
			// Act
			response, err := agentSvc.GetVMLabels()

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(response.Labels).To(gm.ContainElements(
				"production", "critical", "database", "staging", "test",
				"prod-cluster", "cache", "prod-database",
			))
			// Should be sorted alphabetically
			gm.Expect(response.Labels).To(gm.Equal([]string{
				"cache", "critical", "database", "prod-cluster", "prod-database", "production", "staging", "test",
			}))
			// Counts should have the same length as labels
			gm.Expect(response.Counts).To(gm.HaveLen(len(response.Labels)))
			// Each count should be at least 1
			for i, count := range response.Counts {
				gm.Expect(count).To(gm.BeNumerically(">=", 1), "label %s should have count >= 1", response.Labels[i])
			}
		})

		// Given multiple VMs with duplicate labels
		// When I call GET /virtualmachines/labels
		// Then each label should appear only once
		ginkgo.It("should return distinct labels (no duplicates)", func() {
			// Act
			response, err := agentSvc.GetVMLabels()

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Check for uniqueness
			seen := make(map[string]bool)
			for _, label := range response.Labels {
				gm.Expect(seen[label]).To(gm.BeFalse(), "label %s appears multiple times", label)
				seen[label] = true
			}
		})

		// Given VMs with specific label distributions
		// When I call GET /virtualmachines/labels
		// Then counts should accurately reflect VM distribution
		ginkgo.It("should return accurate counts reflecting actual VM label usage", func() {
			// Arrange - Get VMs and set specific label patterns
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(result.VirtualMachines)).To(gm.BeNumerically(">=", 5), "need at least 5 VMs")

			// Clear all labels first
			for _, vm := range result.VirtualMachines {
				_ = agentSvc.UpdateVMLabels(vm.Id, []string{})
			}
			time.Sleep(300 * time.Millisecond)

			// Set up known label distribution:
			// - "common": 3 VMs
			// - "rare": 1 VM
			// - "shared": 2 VMs
			err = agentSvc.UpdateVMLabels(result.VirtualMachines[0].Id, []string{"common", "shared"})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			err = agentSvc.UpdateVMLabels(result.VirtualMachines[1].Id, []string{"common", "shared"})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			err = agentSvc.UpdateVMLabels(result.VirtualMachines[2].Id, []string{"common"})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			err = agentSvc.UpdateVMLabels(result.VirtualMachines[3].Id, []string{"rare"})
			gm.Expect(err).ToNot(gm.HaveOccurred())

			time.Sleep(500 * time.Millisecond)

			// Act
			response, err := agentSvc.GetVMLabels()

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Build a map for easier validation
			labelCounts := make(map[string]int)
			for i, label := range response.Labels {
				labelCounts[label] = response.Counts[i]
			}

			// Verify specific counts
			gm.Expect(labelCounts["common"]).To(gm.Equal(3), "common label should have 3 VMs")
			gm.Expect(labelCounts["rare"]).To(gm.Equal(1), "rare label should have 1 VM")
			gm.Expect(labelCounts["shared"]).To(gm.Equal(2), "shared label should have 2 VMs")
		})

		// Given labels are added and removed dynamically
		// When I call GET /virtualmachines/labels
		// Then counts should update accordingly
		ginkgo.It("should update counts when labels are added or removed", func() {
			// Arrange - Get VMs
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(result.VirtualMachines)).To(gm.BeNumerically(">=", 3), "need at least 3 VMs")

			// Clear all labels
			for _, vm := range result.VirtualMachines {
				_ = agentSvc.UpdateVMLabels(vm.Id, []string{})
			}
			time.Sleep(300 * time.Millisecond)

			// Add "dynamic" label to 1 VM
			err = agentSvc.UpdateVMLabels(result.VirtualMachines[0].Id, []string{"dynamic"})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			time.Sleep(300 * time.Millisecond)

			// Verify count is 1
			response, err := agentSvc.GetVMLabels()
			gm.Expect(err).ToNot(gm.HaveOccurred())
			labelCounts := make(map[string]int)
			for i, label := range response.Labels {
				labelCounts[label] = response.Counts[i]
			}
			gm.Expect(labelCounts["dynamic"]).To(gm.Equal(1))

			// Add "dynamic" label to 2 more VMs
			err = agentSvc.UpdateVMLabels(result.VirtualMachines[1].Id, []string{"dynamic"})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			err = agentSvc.UpdateVMLabels(result.VirtualMachines[2].Id, []string{"dynamic"})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			time.Sleep(300 * time.Millisecond)

			// Verify count is now 3
			response, err = agentSvc.GetVMLabels()
			gm.Expect(err).ToNot(gm.HaveOccurred())
			labelCounts = make(map[string]int)
			for i, label := range response.Labels {
				labelCounts[label] = response.Counts[i]
			}
			gm.Expect(labelCounts["dynamic"]).To(gm.Equal(3))

			// Remove label from 1 VM
			err = agentSvc.UpdateVMLabels(result.VirtualMachines[0].Id, []string{})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			time.Sleep(300 * time.Millisecond)

			// Verify count is now 2
			response, err = agentSvc.GetVMLabels()
			gm.Expect(err).ToNot(gm.HaveOccurred())
			labelCounts = make(map[string]int)
			for i, label := range response.Labels {
				labelCounts[label] = response.Counts[i]
			}
			gm.Expect(labelCounts["dynamic"]).To(gm.Equal(2))
		})

		// Given labels and counts arrays
		// When I call GET /virtualmachines/labels
		// Then arrays should have matching lengths
		ginkgo.It("should always return labels and counts arrays of same length", func() {
			// Act
			response, err := agentSvc.GetVMLabels()

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(response.Labels)).To(gm.Equal(len(response.Counts)),
				"labels and counts arrays must have the same length")

			// Verify all counts are positive
			for i, count := range response.Counts {
				gm.Expect(count).To(gm.BeNumerically(">", 0),
					"count for label %s should be positive", response.Labels[i])
			}
		})

		// Given batch label operations
		// When I call GET /virtualmachines/labels
		// Then counts should reflect all changes
		ginkgo.It("should accurately count after batch label operations", func() {
			// Arrange - Get VMs
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(result.VirtualMachines)).To(gm.BeNumerically(">=", 4), "need at least 4 VMs")

			// Clear all labels
			for _, vm := range result.VirtualMachines {
				_ = agentSvc.UpdateVMLabels(vm.Id, []string{})
			}
			time.Sleep(300 * time.Millisecond)

			// Batch operation: add "batch-label" to multiple VMs at once
			vm1ID := result.VirtualMachines[0].Id
			vm2ID := result.VirtualMachines[1].Id
			vm3ID := result.VirtualMachines[2].Id
			vm4ID := result.VirtualMachines[3].Id

			err = agentSvc.UpdateLabelVMs("batch-label", []string{vm1ID, vm2ID, vm3ID}, nil)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			time.Sleep(300 * time.Millisecond)

			// Verify count is 3
			response, err := agentSvc.GetVMLabels()
			gm.Expect(err).ToNot(gm.HaveOccurred())
			labelCounts := make(map[string]int)
			for i, label := range response.Labels {
				labelCounts[label] = response.Counts[i]
			}
			gm.Expect(labelCounts["batch-label"]).To(gm.Equal(3))

			// Add one more VM to the label
			err = agentSvc.UpdateLabelVMs("batch-label", []string{vm4ID}, nil)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			time.Sleep(300 * time.Millisecond)

			// Verify count is now 4
			response, err = agentSvc.GetVMLabels()
			gm.Expect(err).ToNot(gm.HaveOccurred())
			labelCounts = make(map[string]int)
			for i, label := range response.Labels {
				labelCounts[label] = response.Counts[i]
			}
			gm.Expect(labelCounts["batch-label"]).To(gm.Equal(4))

			// Batch remove 2 VMs
			err = agentSvc.UpdateLabelVMs("batch-label", nil, []string{vm1ID, vm2ID})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			time.Sleep(300 * time.Millisecond)

			// Verify count is now 2
			response, err = agentSvc.GetVMLabels()
			gm.Expect(err).ToNot(gm.HaveOccurred())
			labelCounts = make(map[string]int)
			for i, label := range response.Labels {
				labelCounts[label] = response.Counts[i]
			}
			gm.Expect(labelCounts["batch-label"]).To(gm.Equal(2))
		})
	})

	ginkgo.Context("Filtering VMs by labels using contains operator", func() {
		var vmIDs map[string]string // label description -> VM ID

		ginkgo.BeforeEach(func() {
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(result.VirtualMachines)).To(gm.BeNumerically(">=", 6), "need at least 6 VMs for testing")

			// Clear all labels first
			for _, vm := range result.VirtualMachines {
				_ = agentSvc.UpdateVMLabels(vm.Id, []string{})
			}

			vmIDs = make(map[string]string)

			// Set up VMs with specific label combinations
			vmIDs["prod-critical"] = result.VirtualMachines[0].Id
			_ = agentSvc.UpdateVMLabels(vmIDs["prod-critical"], []string{"production", "critical", "wave-1"})

			vmIDs["prod-db"] = result.VirtualMachines[1].Id
			_ = agentSvc.UpdateVMLabels(vmIDs["prod-db"], []string{"production", "database"})

			vmIDs["staging-worker"] = result.VirtualMachines[2].Id
			_ = agentSvc.UpdateVMLabels(vmIDs["staging-worker"], []string{"staging", "worker"})

			vmIDs["staging-critical"] = result.VirtualMachines[3].Id
			_ = agentSvc.UpdateVMLabels(vmIDs["staging-critical"], []string{"staging", "critical"})

			vmIDs["test"] = result.VirtualMachines[4].Id
			_ = agentSvc.UpdateVMLabels(vmIDs["test"], []string{"test", "temporary"})

			vmIDs["no-labels"] = result.VirtualMachines[5].Id
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
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).To(gm.HaveLen(2))

			ids := []string{}
			for _, vm := range result.VirtualMachines {
				ids = append(ids, vm.Id)
			}
			gm.Expect(ids).To(gm.ConsistOf(vmIDs["prod-critical"], vmIDs["prod-db"]))
		})

		// Given VMs with "critical" label
		// When I filter by "labels contains 'critical'"
		// Then only VMs with critical label should be returned
		ginkgo.It("should find VMs with 'critical' label", func() {
			// Act
			expression := "labels contains 'critical'"
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).To(gm.HaveLen(2))

			ids := []string{}
			for _, vm := range result.VirtualMachines {
				ids = append(ids, vm.Id)
			}
			gm.Expect(ids).To(gm.ConsistOf(vmIDs["prod-critical"], vmIDs["staging-critical"]))
		})

		// Given VMs exist
		// When I filter by "labels not contains 'production'"
		// Then VMs without production label should be returned
		ginkgo.It("should find VMs without 'production' label", func() {
			// Act
			expression := "labels not contains 'production'"
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).To(gm.Not(gm.BeEmpty()))

			// Verify none of the returned VMs are our production VMs
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Id).ToNot(gm.Equal(vmIDs["prod-critical"]),
					"production VM should not appear in not-contains results")
				gm.Expect(vm.Id).ToNot(gm.Equal(vmIDs["prod-db"]),
					"production VM should not appear in not-contains results")
			}

			// Our specific test VMs without production should be present
			ids := []string{}
			for _, vm := range result.VirtualMachines {
				if vm.Id == vmIDs["staging-worker"] || vm.Id == vmIDs["staging-critical"] ||
					vm.Id == vmIDs["test"] || vm.Id == vmIDs["no-labels"] {
					ids = append(ids, vm.Id)
				}
			}
			gm.Expect(ids).To(gm.ContainElements(
				vmIDs["staging-worker"], vmIDs["staging-critical"], vmIDs["test"], vmIDs["no-labels"],
			))
		})

		// Given VMs with various labels
		// When I combine "labels contains 'production' and labels contains 'critical'"
		// Then only VMs with both labels should be returned
		ginkgo.It("should support AND combination with multiple contains", func() {
			// Act
			expression := "labels contains 'production' and labels contains 'critical'"
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).To(gm.HaveLen(1))
			gm.Expect(result.VirtualMachines[0].Id).To(gm.Equal(vmIDs["prod-critical"]))
		})

		// Given VMs with various labels
		// When I use "labels contains 'production' or labels contains 'staging'"
		// Then VMs with either label should be returned
		ginkgo.It("should support OR combination with contains", func() {
			// Act
			expression := "labels contains 'production' or labels contains 'staging'"
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).To(gm.HaveLen(4))

			ids := []string{}
			for _, vm := range result.VirtualMachines {
				ids = append(ids, vm.Id)
			}
			gm.Expect(ids).To(gm.ConsistOf(
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
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).To(gm.HaveLen(1))
			gm.Expect(result.VirtualMachines[0].Id).To(gm.Equal(vmIDs["prod-db"]))
		})

		// Given a label that doesn't exist
		// When I filter by that label
		// Then no VMs should be returned
		ginkgo.It("should return empty result for non-existent label", func() {
			// Act
			expression := "labels contains 'nonexistent'"
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).To(gm.BeEmpty())
		})
	})

	ginkgo.Context("Combining label filters with other filters", func() {
		var testCluster string
		var vmWithLabelInCluster string
		var vmNoLabelInCluster string

		ginkgo.BeforeEach(func() {
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{})
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Group VMs by cluster
			clusterVMs := make(map[string][]v2.VirtualMachine)
			for _, vm := range result.VirtualMachines {
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

			gm.Expect(testCluster).ToNot(gm.BeEmpty(), "need a cluster with at least 2 VMs")

			// Set up labels
			err = agentSvc.UpdateVMLabels(vmWithLabelInCluster, []string{"production", "web"})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			err = agentSvc.UpdateVMLabels(vmNoLabelInCluster, []string{})
			gm.Expect(err).ToNot(gm.HaveOccurred())

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
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).ToNot(gm.BeEmpty())

			// Verify all returned VMs are in the cluster
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Cluster).To(gm.Equal(testCluster))
			}

			// Verify our specific VM is in the results
			foundVM := false
			for _, vm := range result.VirtualMachines {
				if vm.Id == vmWithLabelInCluster {
					foundVM = true
					break
				}
			}
			gm.Expect(foundVM).To(gm.BeTrue(), "VM with label should be in results")
		})
	})

	ginkgo.Context("Validation and error cases", func() {
		var testVMID string

		ginkgo.BeforeEach(func() {
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).ToNot(gm.BeEmpty())
			testVMID = result.VirtualMachines[0].Id
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
			gm.Expect(err).To(gm.HaveOccurred(), "should fail for label exceeding max length")
			gm.Expect(err.Error()).To(gm.ContainSubstring("400"), "should return 400 Bad Request")
			gm.Expect(err.Error()).To(gm.ContainSubstring("must not exceed 100 characters"), "should include validation message")
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
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Verify the label is present via GetVMLabels
			response, err := agentSvc.GetVMLabels()
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(response.Labels).To(gm.ContainElement(maxLabel))
		})

		// Given a label is an empty string or whitespace-only
		// When I try to set it
		// Then I should receive a 400 Bad Request error
		ginkgo.It("should reject empty or whitespace-only labels", func() {
			// Act - try with empty string
			err := agentSvc.UpdateVMLabels(testVMID, []string{"valid", ""})

			// Assert
			gm.Expect(err).To(gm.HaveOccurred(), "should fail for empty label")
			gm.Expect(err.Error()).To(gm.ContainSubstring("400"), "should return 400 Bad Request")
			gm.Expect(err.Error()).To(gm.ContainSubstring("must not be empty or whitespace-only"), "should include validation message")

			// Act - try with whitespace-only
			err = agentSvc.UpdateVMLabels(testVMID, []string{"valid", "   "})

			// Assert
			gm.Expect(err).To(gm.HaveOccurred(), "should fail for whitespace-only label")
			gm.Expect(err.Error()).To(gm.ContainSubstring("400"), "should return 400 Bad Request")
		})
	})

	ginkgo.Context("Batch Label Operations via PATCH", func() {
		var testVMIDs []string

		ginkgo.BeforeEach(func() {
			// Get multiple VM IDs from the collected inventory
			pageSize := 10
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{PageSize: &pageSize})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(result.VirtualMachines)).To(gm.BeNumerically(">=", 3), "need at least 3 VMs for testing")

			testVMIDs = []string{result.VirtualMachines[0].Id, result.VirtualMachines[1].Id, result.VirtualMachines[2].Id}
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
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Verify all VMs have the label via filter
			for _, vmID := range testVMIDs {
				gm.Expect(vmHasLabel(vmID, "batch-test")).To(gm.BeTrue(),
					fmt.Sprintf("VM %s should have batch-test label", vmID))
			}
		})

		// Given multiple VMs have a label
		// When I remove the label via PATCH
		// Then the label should be removed from all VMs (operation is atomic)
		ginkgo.It("should remove label from multiple VMs via PATCH", func() {
			// Arrange - Add label to all VMs first
			for _, vmID := range testVMIDs {
				err := agentSvc.UpdateVMLabels(vmID, []string{"to-remove", "keep-this"})
				gm.Expect(err).ToNot(gm.HaveOccurred())
			}

			// Act
			err := agentSvc.UpdateLabelVMs("to-remove", []string{}, testVMIDs)

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Verify label removed from all VMs
			for _, vmID := range testVMIDs {
				gm.Expect(vmHasLabel(vmID, "to-remove")).To(gm.BeFalse(),
					fmt.Sprintf("VM %s should not have to-remove label", vmID))
				gm.Expect(vmHasLabel(vmID, "keep-this")).To(gm.BeTrue(),
					fmt.Sprintf("VM %s should still have keep-this label", vmID))
			}
		})

		// Given VMs have different labels
		// When I add and remove in single PATCH request
		// Then both operations should succeed atomically
		ginkgo.It("should add and remove in single PATCH request", func() {
			// Arrange
			err := agentSvc.UpdateVMLabels(testVMIDs[0], []string{"old-label"})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			err = agentSvc.UpdateVMLabels(testVMIDs[1], []string{"old-label"})
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Act - Add to vm-3, remove from vm-1 and vm-2
			err = agentSvc.UpdateLabelVMs(
				"new-label",
				[]string{testVMIDs[2]},               // add
				[]string{testVMIDs[0], testVMIDs[1]}, // remove (even though they don't have it yet)
			)

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Verify vm-3 has new-label
			gm.Expect(vmHasLabel(testVMIDs[2], "new-label")).To(gm.BeTrue(), "VM 3 should have new-label")

			// Verify vm-1 and vm-2 don't have new-label (idempotent remove)
			gm.Expect(vmHasLabel(testVMIDs[0], "new-label")).To(gm.BeFalse(), "VM 1 should not have new-label")
			gm.Expect(vmHasLabel(testVMIDs[1], "new-label")).To(gm.BeFalse(), "VM 2 should not have new-label")
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
			gm.Expect(err).To(gm.HaveOccurred())
			gm.Expect(err.Error()).To(gm.ContainSubstring("404"))

			// Verify NO VMs got the label (transaction rolled back)
			for _, vmID := range testVMIDs {
				gm.Expect(vmHasLabel(vmID, "test-label")).To(gm.BeFalse(),
					fmt.Sprintf("VM %s should not have test-label after rollback", vmID))
			}
		})

		// Given neither add nor remove arrays are provided
		// When I send PATCH request
		// Then it should return 400 Bad Request
		ginkgo.It("should reject PATCH request with neither add nor remove", func() {
			// Act
			err := agentSvc.UpdateLabelVMs("test-label", []string{}, []string{})

			// Assert
			gm.Expect(err).To(gm.HaveOccurred())
			gm.Expect(err.Error()).To(gm.ContainSubstring("400"))
		})

		// Given adding an existing label (idempotency test)
		// When I add the same label again
		// Then it should succeed without creating duplicates
		ginkgo.It("should be idempotent when adding existing label", func() {
			// Arrange - Add label first time
			err := agentSvc.UpdateVMLabels(testVMIDs[0], []string{"duplicate-test"})
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Act - Add same label again via PATCH
			err = agentSvc.UpdateLabelVMs("duplicate-test", []string{testVMIDs[0]}, []string{})

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Verify label appears exactly once in GetVMLabels (count should be 1 for this VM)
			response, err := agentSvc.GetVMLabels()
			gm.Expect(err).ToNot(gm.HaveOccurred())
			labelCounts := make(map[string]int)
			for i, label := range response.Labels {
				labelCounts[label] = response.Counts[i]
			}
			gm.Expect(labelCounts["duplicate-test"]).To(gm.Equal(1),
				"duplicate-test label should have count 1 (no duplicates)")
		})
	})

	ginkgo.Context("Global Label Deletion via DELETE", func() {
		var testVMIDs []string

		ginkgo.BeforeEach(func() {
			// Get multiple VM IDs
			pageSize := 5
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{PageSize: &pageSize})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(result.VirtualMachines)).To(gm.BeNumerically(">=", 3))

			testVMIDs = []string{result.VirtualMachines[0].Id, result.VirtualMachines[1].Id, result.VirtualMachines[2].Id}

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
				gm.Expect(err).ToNot(gm.HaveOccurred())
			}

			// Act
			result, err := agentSvc.DeleteLabelGlobally("global-delete-test")

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.Affected).To(gm.Equal(3))
			gm.Expect(result.Label).To(gm.Equal("global-delete-test"))

			// Verify label removed from all VMs via filter
			for _, vmID := range testVMIDs {
				gm.Expect(vmHasLabel(vmID, "global-delete-test")).To(gm.BeFalse(),
					fmt.Sprintf("VM %s should not have global-delete-test after global deletion", vmID))
				gm.Expect(vmHasLabel(vmID, "keep-this")).To(gm.BeTrue(),
					fmt.Sprintf("VM %s should still have keep-this label", vmID))
			}
		})

		// Given no VMs have the label
		// When I delete the label globally
		// Then it should return 0 affected
		ginkgo.It("should return 0 affected when label doesn't exist", func() {
			// Act
			result, err := agentSvc.DeleteLabelGlobally("non-existent-label")

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.Affected).To(gm.Equal(0))
			gm.Expect(result.Label).To(gm.Equal("non-existent-label"))
		})

		// Given a VM has the label as its only label
		// When I delete the label globally
		// Then the VM should have empty labels array
		ginkgo.It("should leave empty array when removing last label", func() {
			// Arrange
			err := agentSvc.UpdateVMLabels(testVMIDs[0], []string{"only-label"})
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Act
			result, err := agentSvc.DeleteLabelGlobally("only-label")

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.Affected).To(gm.Equal(1))

			// Verify the VM no longer has the label
			gm.Expect(vmHasLabel(testVMIDs[0], "only-label")).To(gm.BeFalse(),
				"VM should not have only-label after global deletion")

			// Verify the label no longer appears in GetVMLabels
			labelsResp, err := agentSvc.GetVMLabels()
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(labelsResp.Labels).ToNot(gm.ContainElement("only-label"),
				"only-label should not appear in labels after deletion")
		})
	})
})
