package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gm "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e-v2/service"

	"github.com/google/uuid"
)

var _ = ginkgo.Describe("VM Migration Exclusion v2 e2e tests", ginkgo.Ordered, func() {
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

		ginkgo.GinkgoWriter.Println("Migration exclusion v2 test setup complete")
	})

	ginkgo.AfterAll(func() {
		ginkgo.GinkgoWriter.Println("Cleaning up migration exclusion v2 tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	ptrStr := func(s string) *string { return &s }

	ginkgo.Context("Migration Exclusion CRUD Operations", func() {
		var testVMID string

		ginkgo.BeforeEach(func() {
			// Get a VM ID from the collected inventory
			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{PageSize: &pageSize})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).ToNot(gm.BeEmpty(), "no VMs available for testing")

			testVMID = result.VirtualMachines[0].Id
			ginkgo.GinkgoWriter.Printf("Using test VM: %s (%s)\n", testVMID, result.VirtualMachines[0].Name)
		})

		// Given a VM exists with migration_excluded = false (default)
		// When I exclude it via PATCH /virtualmachines/{id}
		// Then the VM should appear when filtering by migration_excluded = true
		ginkgo.It("should successfully exclude a VM", func() {
			err := agentSvc.UpdateVMMigrationExclusion(testVMID, true)
			gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to exclude VM")

			// Verify via filter expression
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: ptrStr("migration_excluded = true"),
			})
			gm.Expect(err).ToNot(gm.HaveOccurred())

			found := false
			for _, vm := range result.VirtualMachines {
				if vm.Id == testVMID {
					found = true
					break
				}
			}
			gm.Expect(found).To(gm.BeTrue(), "excluded VM should appear in migration_excluded = true filter results")
		})

		// Given a VM is excluded
		// When I include it via PATCH /virtualmachines/{id}
		// Then the VM should appear when filtering by migration_excluded = false
		ginkgo.It("should successfully include a previously excluded VM", func() {
			// Arrange - exclude the VM first
			err := agentSvc.UpdateVMMigrationExclusion(testVMID, true)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Act - include it
			err = agentSvc.UpdateVMMigrationExclusion(testVMID, false)
			gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to include VM")

			// Verify via filter expression
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: ptrStr("migration_excluded = false"),
			})
			gm.Expect(err).ToNot(gm.HaveOccurred())

			found := false
			for _, vm := range result.VirtualMachines {
				if vm.Id == testVMID {
					found = true
					break
				}
			}
			gm.Expect(found).To(gm.BeTrue(), "included VM should appear in migration_excluded = false filter results")
		})

		// Given a non-existent VM ID
		// When I try to exclude it
		// Then I should receive an error
		ginkgo.It("should return error for non-existent VM", func() {
			err := agentSvc.UpdateVMMigrationExclusion("non-existent-vm-id", true)
			gm.Expect(err).To(gm.HaveOccurred(), "should fail for non-existent VM")
			gm.Expect(err.Error()).To(gm.ContainSubstring("404"), "should return 404 error")
		})
	})

	ginkgo.Context("Filtering VMs by migration exclusion status", func() {
		var excludedVMIDs []string
		var includedVMIDs []string

		ginkgo.BeforeEach(func() {
			// Get all VMs across all pages
			pageSize := 100
			page := 1
			var allVMIDs []string

			for {
				result, err := agentSvc.ListLatestVMs(&service.VMListParams{
					Page:     &page,
					PageSize: &pageSize,
				})
				gm.Expect(err).ToNot(gm.HaveOccurred())

				for _, vm := range result.VirtualMachines {
					allVMIDs = append(allVMIDs, vm.Id)
				}
				ginkgo.GinkgoWriter.Printf("Fetched page %d: %d VMs (total so far: %d)\n", page, len(result.VirtualMachines), len(allVMIDs))

				if len(result.VirtualMachines) < pageSize {
					break
				}
				page++
			}

			gm.Expect(len(allVMIDs)).To(gm.BeNumerically(">", 5), "need at least 6 VMs for testing")
			ginkgo.GinkgoWriter.Printf("Total VMs fetched: %d\n", len(allVMIDs))

			// Exclude the first 3 VMs
			excludedVMIDs = []string{}
			includedVMIDs = []string{}

			for i := 0; i < 3 && i < len(allVMIDs); i++ {
				err := agentSvc.UpdateVMMigrationExclusion(allVMIDs[i], true)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				excludedVMIDs = append(excludedVMIDs, allVMIDs[i])
				ginkgo.GinkgoWriter.Printf("Excluded VM: %s\n", allVMIDs[i])
			}

			// Keep the rest as included
			for i := 3; i < len(allVMIDs); i++ {
				err := agentSvc.UpdateVMMigrationExclusion(allVMIDs[i], false)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				includedVMIDs = append(includedVMIDs, allVMIDs[i])
			}

			ginkgo.GinkgoWriter.Printf("Setup: %d excluded, %d included VMs (total: %d)\n",
				len(excludedVMIDs), len(includedVMIDs), len(allVMIDs))
		})

		// Given VMs with mixed exclusion status
		// When I list with migration_excluded = true
		// Then only excluded VMs should be returned
		ginkgo.It("should return only excluded VMs when filtering by migration_excluded = true", func() {
			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: ptrStr("migration_excluded = true"),
				PageSize:     &pageSize,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).To(gm.HaveLen(len(excludedVMIDs)),
				"should return exactly %d excluded VMs", len(excludedVMIDs))

			for _, vm := range result.VirtualMachines {
				gm.Expect(excludedVMIDs).To(gm.ContainElement(vm.Id), "returned VM should be in excluded list")
			}
		})

		// Given VMs with mixed exclusion status
		// When I list with migration_excluded = false
		// Then only included VMs should be returned
		ginkgo.It("should return only included VMs when filtering by migration_excluded = false", func() {
			pageSize := 100
			page := 1
			var filteredIDs []string

			for {
				result, err := agentSvc.ListLatestVMs(&service.VMListParams{
					ByExpression: ptrStr("migration_excluded = false"),
					Page:         &page,
					PageSize:     &pageSize,
				})
				gm.Expect(err).ToNot(gm.HaveOccurred())

				for _, vm := range result.VirtualMachines {
					filteredIDs = append(filteredIDs, vm.Id)
				}

				if len(result.VirtualMachines) < pageSize {
					break
				}
				page++
			}

			gm.Expect(filteredIDs).To(gm.HaveLen(len(includedVMIDs)),
				"should return exactly %d included VMs", len(includedVMIDs))

			for _, id := range filteredIDs {
				gm.Expect(includedVMIDs).To(gm.ContainElement(id), "returned VM should be in included list")
			}
		})

		// Given VMs with mixed exclusion status
		// When I list without filter
		// Then all VMs should be returned
		ginkgo.It("should return all VMs when no exclusion filter is specified", func() {
			pageSize := 100
			page := 1
			totalFetched := 0

			for {
				result, err := agentSvc.ListLatestVMs(&service.VMListParams{
					Page:     &page,
					PageSize: &pageSize,
				})
				gm.Expect(err).ToNot(gm.HaveOccurred())

				totalFetched += len(result.VirtualMachines)

				if len(result.VirtualMachines) < pageSize {
					break
				}
				page++
			}

			totalExpected := len(excludedVMIDs) + len(includedVMIDs)
			gm.Expect(totalFetched).To(gm.Equal(totalExpected), "should return all VMs")
		})
	})

	ginkgo.Context("Combining filters", func() {
		var testCluster string
		var excludedVMInCluster string
		var includedVMInCluster string

		ginkgo.BeforeEach(func() {
			// Find a cluster with at least 2 VMs
			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{PageSize: &pageSize})
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Group VMs by cluster
			type vmEntry struct {
				id      string
				cluster string
			}
			clusterVMs := make(map[string][]vmEntry)
			for _, vm := range result.VirtualMachines {
				clusterVMs[vm.Cluster] = append(clusterVMs[vm.Cluster], vmEntry{id: vm.Id, cluster: vm.Cluster})
			}

			// Find a cluster with at least 2 VMs
			for cluster, vms := range clusterVMs {
				if len(vms) >= 2 {
					testCluster = cluster
					excludedVMInCluster = vms[0].id
					includedVMInCluster = vms[1].id
					break
				}
			}

			gm.Expect(testCluster).ToNot(gm.BeEmpty(), "need a cluster with at least 2 VMs")

			// Set up exclusion status
			err = agentSvc.UpdateVMMigrationExclusion(excludedVMInCluster, true)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			err = agentSvc.UpdateVMMigrationExclusion(includedVMInCluster, false)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			ginkgo.GinkgoWriter.Printf("Test cluster: %s, excluded VM: %s, included VM: %s\n",
				testCluster, excludedVMInCluster, includedVMInCluster)
		})

		// Given VMs in a specific cluster with mixed exclusion status
		// When I filter by cluster AND migration_excluded = false
		// Then only included VMs in that cluster should be returned
		ginkgo.It("should combine cluster filter with migration exclusion filter", func() {
			expression := `cluster = "` + testCluster + `" and migration_excluded = false`
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).ToNot(gm.BeEmpty())

			// Verify all returned VMs are in the cluster and not excluded
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Cluster).To(gm.Equal(testCluster), "VM should be in test cluster")
			}

			// Verify our included VM is in the results
			foundIncluded := false
			for _, vm := range result.VirtualMachines {
				if vm.Id == includedVMInCluster {
					foundIncluded = true
					break
				}
			}
			gm.Expect(foundIncluded).To(gm.BeTrue(), "included VM should be in results")

			// Verify our excluded VM is NOT in the results
			for _, vm := range result.VirtualMachines {
				gm.Expect(vm.Id).ToNot(gm.Equal(excludedVMInCluster), "excluded VM should not be in results")
			}
		})
	})

	ginkgo.Context("Filter DSL with migration_excluded", func() {
		ginkgo.BeforeEach(func() {
			// Set up some VMs with exclusion status
			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{PageSize: &pageSize})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).ToNot(gm.BeEmpty())

			// Exclude first VM
			if len(result.VirtualMachines) > 0 {
				err = agentSvc.UpdateVMMigrationExclusion(result.VirtualMachines[0].Id, true)
				gm.Expect(err).ToNot(gm.HaveOccurred())
			}
		})

		// Given VMs exist
		// When I use filter expression "migration_excluded = true"
		// Then only excluded VMs should be returned
		ginkgo.It("should support migration_excluded in filter DSL", func() {
			expression := "migration_excluded = true"
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).ToNot(gm.BeEmpty(), "should have at least one excluded VM")
		})

		// Given VMs exist with mixed status
		// When I use complex filter expression
		// Then the filter should work correctly
		ginkgo.It("should support complex filter expressions with migration_excluded", func() {
			// Get a cluster name
			pageSize := 100
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{PageSize: &pageSize})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.VirtualMachines).ToNot(gm.BeEmpty())

			cluster := result.VirtualMachines[0].Cluster

			expression := `migration_excluded = false and cluster = "` + cluster + `"`
			filteredResult, err := agentSvc.ListLatestVMs(&service.VMListParams{
				ByExpression: &expression,
			})

			gm.Expect(err).ToNot(gm.HaveOccurred())

			for _, vm := range filteredResult.VirtualMachines {
				gm.Expect(vm.Cluster).To(gm.Equal(cluster), "VM should be in specified cluster")
			}
		})
	})
})
