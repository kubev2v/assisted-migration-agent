package main

import (
	"time"

	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/vcsim"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e-v2/service"

	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	gm "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Collection lifecycle v2 e2e tests", ginkgo.Ordered, func() {
	var agentSvc *service.AgentSvc

	ginkgo.BeforeAll(func() {
		ginkgo.GinkgoWriter.Println("Starting postgres...")
		err := infraManager.StartPostgres()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start postgres")
		time.Sleep(2 * time.Second) // wait for postgres to be ready

		ginkgo.GinkgoWriter.Println("Starting vcsim...")
		err = infraManager.StartVcsim()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start vcsim")

		agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)

		agentID := uuid.NewString()
		_, err = infraManager.StartAgent(infra.AgentConfig{
			AgentID:        agentID,
			SourceID:       uuid.NewString(),
			Mode:           "disconnected",
			APIVersion:     "v2",
			ConsoleURL:     cfg.AgentProxyUrl,
			UpdateInterval: "1s",
		})
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
		ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

		gm.Eventually(func() error {
			_, err := agentSvc.Status()
			return err
		}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

		// Store credentials and start collector
		_, err = agentSvc.StoreCredentials(infra.VcsimURL, infra.VcsimUsername, infra.VcsimPassword)
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to store credentials")

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
	})

	ginkgo.AfterAll(func() {
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	// Given a successful collection has completed
	// When listing collections
	// Then at least one collection should be returned with non-empty ID and Name
	ginkgo.It("should list collections after successful collection", func() {
		collections, err := agentSvc.ListCollections()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to list collections")
		gm.Expect(collections.Collections).ToNot(gm.BeEmpty(), "expected at least 1 collection")

		first := collections.Collections[0]
		gm.Expect(first.Id).ToNot(gm.BeEmpty(), "expected collection ID to be non-empty")
		gm.Expect(first.Name).ToNot(gm.BeEmpty(), "expected collection Name to be non-empty")
		ginkgo.GinkgoWriter.Printf("Collection: id=%s name=%s createdAt=%s\n", first.Id, first.Name, first.CreatedAt)
	})

	// Given a first collection has completed
	// When a VM is added to the vCenter inventory and a second collection runs
	// Then comparing the two collections should report exactly one extra VM in the second
	ginkgo.It("should reflect an added VM as a diff between two collections", func() {
		before, err := agentSvc.ListCollections()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to list collections")
		gm.Expect(before.Collections).ToNot(gm.BeEmpty(), "expected at least 1 collection before mutating inventory")
		firstID := before.Collections[0].Id

		ginkgo.GinkgoWriter.Println("Adding a VM to the vcsim inventory...")
		_, err = infraManager.AddVMs([]vcsim.VM{{Name: "collection-test-vm-3"}})
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to add VM to vcsim inventory")

		_, err = agentSvc.StartCollector()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start second collector run")

		gm.Eventually(func() int {
			collections, err := agentSvc.ListCollections()
			if err != nil {
				return 0
			}
			return len(collections.Collections)
		}, 120*time.Second, 2*time.Second).Should(gm.BeNumerically(">", 1), "expected a second collection")

		after, err := agentSvc.ListCollections()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to list collections")
		gm.Expect(after.Collections).To(gm.HaveLen(2), "expected exactly two collections")

		var secondID string
		for _, c := range after.Collections {
			if c.Id != firstID {
				secondID = c.Id
			}
		}
		gm.Expect(secondID).ToNot(gm.BeEmpty(), "expected a distinct second collection ID")

		summary, err := agentSvc.CompareCollections(firstID, secondID)
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to compare collections")
		gm.Expect(summary.Diff.TotalVMs.Delta).To(gm.Equal(1), "expected exactly one more VM in the second collection")
		gm.Expect(summary.Diff.TotalVMs.OnlyInB).To(gm.HaveValue(gm.Equal(1)), "expected the added VM to be unique to the second collection")
		gm.Expect(summary.Diff.TotalVMs.OnlyInA).To(gm.HaveValue(gm.Equal(0)), "expected no VMs unique to the first collection")

		// Cross-check the per-dimension diff endpoint: it should name the
		// exact VM ID that the aggregate summary only counted.
		pageSize := 100
		secondVMs, err := agentSvc.ListVMs(secondID, &service.VMListParams{PageSize: &pageSize})
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to list VMs for second collection")

		var addedVMID string
		for _, vm := range secondVMs.VirtualMachines {
			if vm.Name == "collection-test-vm-3" {
				addedVMID = vm.Id
			}
		}
		gm.Expect(addedVMID).ToNot(gm.BeEmpty(), "expected to find the added VM in the second collection")

		diff, err := agentSvc.CompareCollectionsDiff(firstID, secondID, v2.CompareCollectionsDiffParamsDimensionTotal, nil)
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to compare collections by dimension")
		gm.Expect(diff.OnlyInA.VmIds).To(gm.BeEmpty(), "expected no VM IDs unique to the first collection")
		gm.Expect(diff.OnlyInB.VmIds).To(gm.ConsistOf(addedVMID), "expected the added VM's ID to be the only one unique to the second collection")
	})

	// Given collections exist from previous tests
	// When DELETE /collections is called
	// Then all collections should be removed and the agent should be in disconnected mode
	ginkgo.It("should delete all collections and switch to disconnected mode", func() {
		collections, err := agentSvc.ListCollections()
		gm.Expect(err).ToNot(gm.HaveOccurred())
		gm.Expect(collections.Collections).ToNot(gm.BeEmpty(), "expected at least 1 collection before delete")

		err = agentSvc.DeleteCollections()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to delete all collections")

		collections, err = agentSvc.ListCollections()
		gm.Expect(err).ToNot(gm.HaveOccurred())
		gm.Expect(collections.Collections).To(gm.BeEmpty(), "expected no collections after delete")

		status, err := agentSvc.Status()
		gm.Expect(err).ToNot(gm.HaveOccurred())
		gm.Expect(status.Mode).To(gm.Equal("disconnected"), "expected agent to be in disconnected mode after delete")
	})
})
