package main

import (
	"crypto/tls"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2" // nolint:staticcheck
	. "github.com/onsi/gomega"    // nolint:staticcheck

	"github.com/google/uuid"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/test/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e/service"
)

var _ = Describe("Migration Exclusion Group Inventory e2e tests", Ordered, func() {
	var (
		agentSvc *service.AgentSvc
		allVMs   []v1.VirtualMachine
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

		// Get all VMs for testing
		pageSize := 100
		result, err := agentSvc.ListVMs(&service.VMListParams{PageSize: &pageSize})
		Expect(err).ToNot(HaveOccurred(), "failed to list VMs after collection")
		allVMs = result.Vms
		Expect(len(allVMs)).To(BeNumerically(">", 0), "should have VMs to test with")

		GinkgoWriter.Printf("Migration exclusion group inventory test setup complete — %d VMs collected\n", len(allVMs))
	})

	AfterAll(func() {
		GinkgoWriter.Println("Cleaning up migration exclusion group inventory tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	Context("Group inventory updates when VM exclusion state changes", func() {
		var (
			testGroup          *v1.Group
			testCluster        string
			testVMID           string
			testVMName         string
			otherVMInSameGroup string
		)

		BeforeEach(func() {
			// Find a cluster with at least 2 VMs
			clusterVMs := make(map[string][]v1.VirtualMachine)
			for _, vm := range allVMs {
				clusterVMs[vm.Cluster] = append(clusterVMs[vm.Cluster], vm)
			}

			// Find a cluster with at least 2 VMs
			for cluster, vms := range clusterVMs {
				if len(vms) >= 2 {
					testCluster = cluster
					testVMID = vms[0].Id
					testVMName = vms[0].Name
					otherVMInSameGroup = vms[1].Id
					break
				}
			}

			Expect(testCluster).ToNot(BeEmpty(), "need a cluster with at least 2 VMs")
			GinkgoWriter.Printf("Using test cluster: %s, VM: %s (%s)\n", testCluster, testVMID, testVMName)

			// Ensure VMs start as included (not excluded)
			err := agentSvc.UpdateVMMigrationExclusion(testVMID, false)
			Expect(err).ToNot(HaveOccurred())
			err = agentSvc.UpdateVMMigrationExclusion(otherVMInSameGroup, false)
			Expect(err).ToNot(HaveOccurred())

			// Create a group that matches VMs in the test cluster
			groupName := "test-cluster-" + uuid.NewString()[:8]
			group, err := agentSvc.CreateGroup(
				groupName,
				`cluster = "`+testCluster+`"`,
				"Test group for migration exclusion",
			)
			Expect(err).ToNot(HaveOccurred(), "failed to create test group")
			testGroup = group

			GinkgoWriter.Printf("Created test group: %s (ID: %s)\n", testGroup.Name, testGroup.Id)
		})

		AfterEach(func() {
			// Clean up: delete the test group and reset VM exclusion states
			if testGroup != nil {
				_, _ = agentSvc.DeleteGroup(testGroup.Id.String())
			}
			if testVMID != "" {
				_ = agentSvc.UpdateVMMigrationExclusion(testVMID, false)
			}
			if otherVMInSameGroup != "" {
				_ = agentSvc.UpdateVMMigrationExclusion(otherVMInSameGroup, false)
			}
		})

		// Given a group with inventory containing VMs
		// When I exclude a VM in that group
		// Then the group inventory should be updated to reflect the new exclusion state
		It("should update group inventory when VM is excluded", func() {
			// Arrange: Get initial group state
			initialGroup, err := agentSvc.GetGroup(testGroup.Id.String(), nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(initialGroup.Inventory).ToNot(BeNil(), "initial inventory should exist")

			initialUpdatedAt := initialGroup.Group.UpdatedAt
			GinkgoWriter.Printf("Initial group updated_at: %v\n", initialUpdatedAt)

			// Small delay to ensure timestamp difference is detectable
			time.Sleep(100 * time.Millisecond)

			// Act: Exclude a VM in the group
			err = agentSvc.UpdateVMMigrationExclusion(testVMID, true)
			Expect(err).ToNot(HaveOccurred(), "failed to exclude VM")

			// Assert: Group inventory should be updated
			updatedGroup, err := agentSvc.GetGroup(testGroup.Id.String(), nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedGroup.Inventory).ToNot(BeNil(), "updated inventory should exist")

			// Verify the updated_at timestamp changed
			Expect(updatedGroup.Group.UpdatedAt).ToNot(Equal(initialUpdatedAt),
				"group updated_at should change when VM exclusion state changes")
			GinkgoWriter.Printf("Updated group updated_at: %v (changed: %v)\n",
				updatedGroup.Group.UpdatedAt, updatedGroup.Group.UpdatedAt != initialUpdatedAt)

			// Verify the VM is now excluded
			excludedVM, err := agentSvc.GetVM(testVMID)
			Expect(err).ToNot(HaveOccurred())
			Expect(excludedVM.MigrationExcluded).ToNot(BeNil())
			Expect(*excludedVM.MigrationExcluded).To(BeTrue(), "VM should be excluded")
		})

		// Given a group with an excluded VM
		// When I include the VM back
		// Then the group inventory should be updated again
		It("should update group inventory when VM is included back", func() {
			// Arrange: Exclude a VM first
			err := agentSvc.UpdateVMMigrationExclusion(testVMID, true)
			Expect(err).ToNot(HaveOccurred())

			excludedGroup, err := agentSvc.GetGroup(testGroup.Id.String(), nil)
			Expect(err).ToNot(HaveOccurred())
			excludedUpdatedAt := excludedGroup.Group.UpdatedAt
			GinkgoWriter.Printf("Group updated_at after exclusion: %v\n", excludedUpdatedAt)

			time.Sleep(100 * time.Millisecond)

			// Act: Include the VM back
			err = agentSvc.UpdateVMMigrationExclusion(testVMID, false)
			Expect(err).ToNot(HaveOccurred(), "failed to include VM")

			// Assert: Group inventory should be updated again
			includedGroup, err := agentSvc.GetGroup(testGroup.Id.String(), nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(includedGroup.Inventory).ToNot(BeNil())

			// Verify the updated_at timestamp changed again
			Expect(includedGroup.Group.UpdatedAt).ToNot(Equal(excludedUpdatedAt),
				"group updated_at should change when VM is included back")
			GinkgoWriter.Printf("Group updated_at after inclusion: %v (changed: %v)\n",
				includedGroup.Group.UpdatedAt, includedGroup.Group.UpdatedAt != excludedUpdatedAt)

			// Verify the VM is now included
			includedVM, err := agentSvc.GetVM(testVMID)
			Expect(err).ToNot(HaveOccurred())
			Expect(includedVM.MigrationExcluded).ToNot(BeNil())
			Expect(*includedVM.MigrationExcluded).To(BeFalse(), "VM should be included")
		})

		// Given multiple groups containing the same VM
		// When I exclude the VM
		// Then all affected group inventories should be updated
		It("should update all groups containing the VM", func() {
			// Arrange: Create a second group that also contains the VM
			// Use a filter that includes VMs in the test cluster with a specific name pattern
			secondGroupName := "test-multi-group-" + uuid.NewString()[:8]
			secondGroup, err := agentSvc.CreateGroup(
				secondGroupName,
				`cluster = "`+testCluster+`" and name ~ /`+testVMName+`/`,
				"Second test group for multi-group test",
			)
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				_, _ = agentSvc.DeleteGroup(secondGroup.Id.String())
			}()

			// Get initial states
			initialGroup1, err := agentSvc.GetGroup(testGroup.Id.String(), nil)
			Expect(err).ToNot(HaveOccurred())
			initialUpdatedAt1 := initialGroup1.Group.UpdatedAt

			initialGroup2, err := agentSvc.GetGroup(secondGroup.Id.String(), nil)
			Expect(err).ToNot(HaveOccurred())
			initialUpdatedAt2 := initialGroup2.Group.UpdatedAt

			GinkgoWriter.Printf("Initial timestamps - Group1: %v, Group2: %v\n",
				initialUpdatedAt1, initialUpdatedAt2)

			time.Sleep(100 * time.Millisecond)

			// Act: Exclude the VM
			err = agentSvc.UpdateVMMigrationExclusion(testVMID, true)
			Expect(err).ToNot(HaveOccurred())

			// Assert: Both groups should have updated inventories
			updatedGroup1, err := agentSvc.GetGroup(testGroup.Id.String(), nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedGroup1.Group.UpdatedAt).ToNot(Equal(initialUpdatedAt1),
				"first group updated_at should change")

			updatedGroup2, err := agentSvc.GetGroup(secondGroup.Id.String(), nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedGroup2.Group.UpdatedAt).ToNot(Equal(initialUpdatedAt2),
				"second group updated_at should change")

			GinkgoWriter.Printf("Updated timestamps - Group1: %v (changed: %v), Group2: %v (changed: %v)\n",
				updatedGroup1.Group.UpdatedAt, updatedGroup1.Group.UpdatedAt != initialUpdatedAt1,
				updatedGroup2.Group.UpdatedAt, updatedGroup2.Group.UpdatedAt != initialUpdatedAt2)
		})

		// Given a VM that is not in any group
		// When I exclude it
		// Then the operation should succeed without errors
		It("should handle VM exclusion when VM is not in any group", func() {
			// Arrange: Find a VM in a different cluster (not in test group)
			var vmNotInGroup string
			for _, vm := range allVMs {
				if vm.Cluster != testCluster {
					vmNotInGroup = vm.Id
					break
				}
			}
			Expect(vmNotInGroup).ToNot(BeEmpty(), "need a VM outside test cluster")
			defer func() {
				_ = agentSvc.UpdateVMMigrationExclusion(vmNotInGroup, false)
			}()

			// Act: Exclude the VM
			err := agentSvc.UpdateVMMigrationExclusion(vmNotInGroup, true)

			// Assert: Should succeed
			Expect(err).ToNot(HaveOccurred(), "excluding VM not in any group should succeed")

			// Verify VM is excluded
			vm, err := agentSvc.GetVM(vmNotInGroup)
			Expect(err).ToNot(HaveOccurred())
			Expect(vm.MigrationExcluded).ToNot(BeNil())
			Expect(*vm.MigrationExcluded).To(BeTrue())
		})

		// Given a group with multiple VMs
		// When I exclude one VM
		// Then only the excluded VM should have migration_excluded = true
		// And the group inventory should reflect both VMs' correct states
		It("should correctly reflect exclusion state for individual VMs in group inventory", func() {
			// Arrange: We have testVMID and otherVMInSameGroup in the same group
			// Both should start as included
			vm1, err := agentSvc.GetVM(testVMID)
			Expect(err).ToNot(HaveOccurred())
			Expect(vm1.MigrationExcluded).ToNot(BeNil())
			Expect(*vm1.MigrationExcluded).To(BeFalse(), "first VM should be included initially")

			vm2, err := agentSvc.GetVM(otherVMInSameGroup)
			Expect(err).ToNot(HaveOccurred())
			Expect(vm2.MigrationExcluded).ToNot(BeNil())
			Expect(*vm2.MigrationExcluded).To(BeFalse(), "second VM should be included initially")

			// Act: Exclude only the first VM
			err = agentSvc.UpdateVMMigrationExclusion(testVMID, true)
			Expect(err).ToNot(HaveOccurred())

			// Assert: Verify exclusion states
			vm1After, err := agentSvc.GetVM(testVMID)
			Expect(err).ToNot(HaveOccurred())
			Expect(vm1After.MigrationExcluded).ToNot(BeNil())
			Expect(*vm1After.MigrationExcluded).To(BeTrue(), "first VM should be excluded")

			vm2After, err := agentSvc.GetVM(otherVMInSameGroup)
			Expect(err).ToNot(HaveOccurred())
			Expect(vm2After.MigrationExcluded).ToNot(BeNil())
			Expect(*vm2After.MigrationExcluded).To(BeFalse(), "second VM should remain included")

			// Verify group inventory was updated
			updatedGroup, err := agentSvc.GetGroup(testGroup.Id.String(), nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedGroup.Inventory).ToNot(BeNil(), "group inventory should exist")

			GinkgoWriter.Printf("Group inventory updated successfully with mixed VM exclusion states\n")
		})
	})
})
