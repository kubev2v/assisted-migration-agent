package main

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/google/uuid"

	"github.com/kubev2v/assisted-migration-agent/test/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e/service"
)

var _ = ginkgo.Describe("VM Migration Exclusion e2e tests", ginkgo.Ordered, func() {
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

		ginkgo.GinkgoWriter.Println("Migration exclusion test setup complete")
	})

	ginkgo.AfterAll(func() {
		ginkgo.GinkgoWriter.Println("Cleaning up migration exclusion tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	ginkgo.Context("Migration Exclusion CRUD Operations", func() {
		var testVMID string

		ginkgo.BeforeEach(func() {
			// Get a VM ID from the collected inventory
			result, err := agentSvc.ListVMsV1(&service.VMListParamsV1{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).ToNot(gomega.BeEmpty(), "no VMs available for testing")

			testVMID = result.Vms[0].ID
			ginkgo.GinkgoWriter.Printf("Using test VM: %s (%s)\n", testVMID, result.Vms[0].Name)
		})

		// Given a VM exists with migration_excluded = false (default)
		// When I exclude it via PATCH /virtualmachines/{id}
		// Then the VM should be marked as excluded
		ginkgo.It("should successfully exclude a VM", func() {
			// Act
			err := agentSvc.UpdateVMMigrationExclusion(testVMID, true)

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), "failed to exclude VM")

			// Verify via list API
			result, err := agentSvc.ListVMsV1(&service.VMListParamsV1{
				ByExpression: ptrString("migration_excluded = true"),
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Find our test VM in the excluded list
			found := false
			for _, vm := range result.Vms {
				if vm.ID == testVMID {
					found = true
					gomega.Expect(vm.MigrationExcluded).To(gomega.BeTrue(), "VM should be marked as excluded")
					break
				}
			}
			gomega.Expect(found).To(gomega.BeTrue(), "excluded VM should appear in filtered list")
		})

		// Given a VM is excluded
		// When I include it via PATCH /virtualmachines/{id}
		// Then the VM should be marked as included
		ginkgo.It("should successfully include a previously excluded VM", func() {
			// Arrange - exclude the VM first
			err := agentSvc.UpdateVMMigrationExclusion(testVMID, true)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Act - include it
			err = agentSvc.UpdateVMMigrationExclusion(testVMID, false)

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), "failed to include VM")

			// Verify via list API
			result, err := agentSvc.ListVMsV1(&service.VMListParamsV1{
				ByExpression: ptrString("migration_excluded = false"),
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Find our test VM in the included list
			found := false
			for _, vm := range result.Vms {
				if vm.ID == testVMID {
					found = true
					gomega.Expect(vm.MigrationExcluded).To(gomega.BeFalse(), "VM should be marked as included")
					break
				}
			}
			gomega.Expect(found).To(gomega.BeTrue(), "included VM should appear in filtered list")
		})

		// Given a non-existent VM ID
		// When I try to exclude it
		// Then I should receive an error
		ginkgo.It("should return error for non-existent VM", func() {
			// Act
			err := agentSvc.UpdateVMMigrationExclusion("non-existent-vm-id", true)

			// Assert
			gomega.Expect(err).To(gomega.HaveOccurred(), "should fail for non-existent VM")
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("404"), "should return 404 error")
		})
	})

	ginkgo.Context("Filtering VMs by migration exclusion status", func() {
		var excludedVMIDs []string
		var includedVMIDs []string

		ginkgo.BeforeEach(func() {
			// Get all VMs and mark some as excluded
			result, err := agentSvc.ListVMsV1(&service.VMListParamsV1{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(len(result.Vms)).To(gomega.BeNumerically(">", 5), "need at least 6 VMs for testing")

			// Exclude the first 3 VMs
			excludedVMIDs = []string{}
			includedVMIDs = []string{}

			for i := 0; i < 3 && i < len(result.Vms); i++ {
				vmID := result.Vms[i].ID
				err := agentSvc.UpdateVMMigrationExclusion(vmID, true)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				excludedVMIDs = append(excludedVMIDs, vmID)
				ginkgo.GinkgoWriter.Printf("Excluded VM: %s\n", vmID)
			}

			// Keep the rest as included
			for i := 3; i < len(result.Vms); i++ {
				vmID := result.Vms[i].ID
				err := agentSvc.UpdateVMMigrationExclusion(vmID, false)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				includedVMIDs = append(includedVMIDs, vmID)
			}

			ginkgo.GinkgoWriter.Printf("Setup: %d excluded, %d included VMs\n", len(excludedVMIDs), len(includedVMIDs))
		})

		// Given VMs with mixed exclusion status
		// When I list with migrationExcluded=true
		// Then only excluded VMs should be returned
		ginkgo.It("should return only excluded VMs when filtering by migrationExcluded=true", func() {
			// Act
			result, err := agentSvc.ListVMsV1(&service.VMListParamsV1{
				ByExpression: ptrString("migration_excluded = true"),
			})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).To(gomega.HaveLen(len(excludedVMIDs)), "should return exactly %d excluded VMs", len(excludedVMIDs))

			for _, vm := range result.Vms {
				gomega.Expect(vm.MigrationExcluded).To(gomega.BeTrue(), "all returned VMs should be excluded")
				gomega.Expect(excludedVMIDs).To(gomega.ContainElement(vm.ID), "returned VM should be in excluded list")
			}
		})

		// Given VMs with mixed exclusion status
		// When I list with migrationExcluded=false
		// Then only included VMs should be returned
		ginkgo.It("should return only included VMs when filtering by migrationExcluded=false", func() {
			// Act
			result, err := agentSvc.ListVMsV1(&service.VMListParamsV1{
				ByExpression: ptrString("migration_excluded = false"),
			})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).To(gomega.HaveLen(len(includedVMIDs)), "should return exactly %d included VMs", len(includedVMIDs))

			for _, vm := range result.Vms {
				gomega.Expect(vm.MigrationExcluded).To(gomega.BeFalse(), "all returned VMs should not be excluded")
				gomega.Expect(includedVMIDs).To(gomega.ContainElement(vm.ID), "returned VM should be in included list")
			}
		})

		// Given VMs with mixed exclusion status
		// When I list without filter
		// Then all VMs should be returned
		ginkgo.It("should return all VMs when no exclusion filter is specified", func() {
			// Act
			result, err := agentSvc.ListVMsV1(&service.VMListParamsV1{})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			totalExpected := len(excludedVMIDs) + len(includedVMIDs)
			gomega.Expect(result.Vms).To(gomega.HaveLen(totalExpected), "should return all VMs")
		})
	})

	ginkgo.Context("Combining filters", func() {
		var testCluster string
		var excludedVMInCluster string
		var includedVMInCluster string

		ginkgo.BeforeEach(func() {
			// Find a cluster with at least 2 VMs
			result, err := agentSvc.ListVMsV1(&service.VMListParamsV1{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Group VMs by cluster
			clusterVMs := make(map[string][]service.VirtualMachineV1)
			for _, vm := range result.Vms {
				clusterVMs[vm.Cluster] = append(clusterVMs[vm.Cluster], vm)
			}

			// Find a cluster with at least 2 VMs
			for cluster, vms := range clusterVMs {
				if len(vms) >= 2 {
					testCluster = cluster
					// Exclude first VM, include second
					excludedVMInCluster = vms[0].ID
					includedVMInCluster = vms[1].ID
					break
				}
			}

			gomega.Expect(testCluster).ToNot(gomega.BeEmpty(), "need a cluster with at least 2 VMs")

			// Set up exclusion status
			err = agentSvc.UpdateVMMigrationExclusion(excludedVMInCluster, true)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			err = agentSvc.UpdateVMMigrationExclusion(includedVMInCluster, false)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.GinkgoWriter.Printf("Test cluster: %s, excluded VM: %s, included VM: %s\n",
				testCluster, excludedVMInCluster, includedVMInCluster)
		})

		// Given VMs in a specific cluster with mixed exclusion status
		// When I filter by cluster AND migrationExcluded=false
		// Then only included VMs in that cluster should be returned
		ginkgo.It("should combine cluster filter with migration exclusion filter", func() {
			// Act
			expression := `cluster = "` + testCluster + `" and migration_excluded = false`
			result, err := agentSvc.ListVMsV1(&service.VMListParamsV1{
				ByExpression: &expression,
			})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).ToNot(gomega.BeEmpty())

			// Verify all returned VMs are in the cluster and not excluded
			for _, vm := range result.Vms {
				gomega.Expect(vm.Cluster).To(gomega.Equal(testCluster), "VM should be in test cluster")
				gomega.Expect(vm.MigrationExcluded).To(gomega.BeFalse(), "VM should not be excluded")
			}

			// Verify our included VM is in the results
			foundIncluded := false
			for _, vm := range result.Vms {
				if vm.ID == includedVMInCluster {
					foundIncluded = true
					break
				}
			}
			gomega.Expect(foundIncluded).To(gomega.BeTrue(), "included VM should be in results")

			// Verify our excluded VM is NOT in the results
			for _, vm := range result.Vms {
				gomega.Expect(vm.ID).ToNot(gomega.Equal(excludedVMInCluster), "excluded VM should not be in results")
			}
		})
	})

	ginkgo.Context("Filter DSL with migration_excluded", func() {
		ginkgo.BeforeEach(func() {
			// Set up some VMs with exclusion status
			result, err := agentSvc.ListVMsV1(&service.VMListParamsV1{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).ToNot(gomega.BeEmpty())

			// Exclude first VM
			if len(result.Vms) > 0 {
				err = agentSvc.UpdateVMMigrationExclusion(result.Vms[0].ID, true)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}
		})

		// Given VMs exist
		// When I use filter expression "migration_excluded = true"
		// Then only excluded VMs should be returned
		ginkgo.It("should support migration_excluded in filter DSL", func() {
			// Act
			expression := "migration_excluded = true"
			result, err := agentSvc.ListVMsV1(&service.VMListParamsV1{
				ByExpression: &expression,
			})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).ToNot(gomega.BeEmpty(), "should have at least one excluded VM")

			for _, vm := range result.Vms {
				gomega.Expect(vm.MigrationExcluded).To(gomega.BeTrue(), "all VMs should be excluded")
			}
		})

		// Given VMs exist with mixed status
		// When I use complex filter expression
		// Then the filter should work correctly
		ginkgo.It("should support complex filter expressions with migration_excluded", func() {
			// Get a cluster name
			result, err := agentSvc.ListVMsV1(&service.VMListParamsV1{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result.Vms).ToNot(gomega.BeEmpty())

			cluster := result.Vms[0].Cluster

			// Act
			expression := `migration_excluded = false and cluster = "` + cluster + `"`
			filteredResult, err := agentSvc.ListVMsV1(&service.VMListParamsV1{
				ByExpression: &expression,
			})

			// Assert
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			for _, vm := range filteredResult.Vms {
				gomega.Expect(vm.MigrationExcluded).To(gomega.BeFalse(), "VM should not be excluded")
				gomega.Expect(vm.Cluster).To(gomega.Equal(cluster), "VM should be in specified cluster")
			}
		})
	})
})

func ptrString(s string) *string {
	return &s
}
