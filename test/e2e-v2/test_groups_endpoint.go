package main

import (
	"crypto/tls"
	"net/http"
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

		ginkgo.GinkgoWriter.Println("Group endpoint v2 test setup complete")
	})

	ginkgo.AfterAll(func() {
		ginkgo.GinkgoWriter.Println("Cleaning up group endpoint v2 tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	// Given a collected inventory
	// When creating a group with a name and filter
	// Then the returned group should have matching Name and Filter
	ginkgo.It("should create a group", func() {
		group, err := agentSvc.CreateGroup(collectionID, "test-group-v2", "memory > 0", "e2e v2 test group", nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(collectionID, group.Id) }()

		ginkgo.GinkgoWriter.Printf("Created group: id=%s, name=%s\n", group.Id, group.Name)
		gm.Expect(group.Id).ToNot(gm.BeEmpty())
		gm.Expect(group.Name).To(gm.Equal("test-group-v2"))
		gm.Expect(group.Filter).To(gm.Equal("memory > 0"))
		gm.Expect(group.Description).ToNot(gm.BeNil())
		gm.Expect(*group.Description).To(gm.Equal("e2e v2 test group"))
	})

	// Given a collected inventory
	// When creating a group with tags
	// Then the returned group should have the specified tags
	ginkgo.It("should create a group with tags", func() {
		tags := []string{"prod", "web"}
		group, err := agentSvc.CreateGroup(collectionID, "tagged-group-v2", "memory > 0", "", tags)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(collectionID, group.Id) }()

		ginkgo.GinkgoWriter.Printf("Created group with tags: id=%s, tags=%v\n", group.Id, group.Tags)
		gm.Expect(group.Tags).ToNot(gm.BeNil())
		gm.Expect(*group.Tags).To(gm.ConsistOf("prod", "web"))
	})

	// Given two created groups
	// When listing groups
	// Then the total should be at least 2
	ginkgo.It("should list groups", func() {
		g1, err := agentSvc.CreateGroup(collectionID, "list-group-a", "memory > 0", "", nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(collectionID, g1.Id) }()

		g2, err := agentSvc.CreateGroup(collectionID, "list-group-b", "memory > 0", "", nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(collectionID, g2.Id) }()

		list, err := agentSvc.ListGroups(collectionID, nil, nil, nil)
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
		group, err := agentSvc.CreateGroup(collectionID, "vms-group-v2", "memory > 0", "", nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(collectionID, group.Id) }()

		pageSize := 100
		resp, err := agentSvc.GetGroup(collectionID, group.Id, nil, nil, &pageSize)
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
		group, err := agentSvc.CreateGroup(collectionID, "original-v2", "memory > 0", "", nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(collectionID, group.Id) }()

		newName := "updated-v2"
		updated, err := agentSvc.UpdateGroup(collectionID, group.Id, v2.UpdateGroupRequest{Name: &newName})
		gm.Expect(err).ToNot(gm.HaveOccurred())

		ginkgo.GinkgoWriter.Printf("Updated group: name=%s\n", updated.Name)
		gm.Expect(updated.Name).To(gm.Equal("updated-v2"))
		gm.Expect(updated.Filter).To(gm.Equal("memory > 0"))
	})

	// Given a created group
	// When deleting the group and then getting it
	// Then GetGroup should return a not found error
	ginkgo.It("should delete a group", func() {
		group, err := agentSvc.CreateGroup(collectionID, "to-delete-v2", "memory > 0", "", nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		status, err := agentSvc.DeleteGroup(collectionID, group.Id)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		gm.Expect(status).To(gm.Equal(http.StatusNoContent))

		_, err = agentSvc.GetGroup(collectionID, group.Id, nil, nil, nil)
		gm.Expect(err).To(gm.HaveOccurred())
		gm.Expect(err.Error()).To(gm.ContainSubstring("not found"))
	})

	// Given multiple groups with distinct names
	// When listing groups filtered by name
	// Then only the matching group should be returned
	ginkgo.It("should filter groups by name", func() {
		g1, err := agentSvc.CreateGroup(collectionID, "filter-target-v2", "memory > 0", "", nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(collectionID, g1.Id) }()

		g2, err := agentSvc.CreateGroup(collectionID, "other-group-v2", "memory > 0", "", nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(collectionID, g2.Id) }()

		byName := "filter-target-v2"
		list, err := agentSvc.ListGroups(collectionID, &byName, nil, nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		ginkgo.GinkgoWriter.Printf("Filtered groups by name '%s': total=%d\n", byName, list.Total)
		gm.Expect(list.Total).To(gm.Equal(1))
		gm.Expect(list.Groups).To(gm.HaveLen(1))
		gm.Expect(list.Groups[0].Name).To(gm.Equal("filter-target-v2"))
	})
})
