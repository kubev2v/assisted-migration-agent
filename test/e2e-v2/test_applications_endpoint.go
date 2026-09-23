package main

import (
	"fmt"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gm "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e-v2/service"

	"github.com/google/uuid"
)

var _ = ginkgo.Describe("Applications endpoint v2 e2e tests", ginkgo.Ordered, func() {
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
		collectionID = collections.Collections[0].Id
		ginkgo.GinkgoWriter.Printf("Using collection: %s\n", collectionID)

		ginkgo.GinkgoWriter.Println("Applications endpoint v2 test setup complete")
	})

	ginkgo.AfterAll(func() {
		ginkgo.GinkgoWriter.Println("Cleaning up applications endpoint v2 tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	ginkgo.Context("Application detection", func() {
		// Given an agent that has collected inventory from vcsim
		// When listing applications for the collection
		// Then Nginx should be detected with VmCount > 0
		ginkgo.It("should detect Nginx on workload VMs", func() {
			result, err := agentSvc.ListApplications(collectionID)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			var found bool
			for _, app := range result.Applications {
				if app.Name == "Nginx" {
					found = true
					gm.Expect(app.VmCount).To(gm.BeNumerically(">", 0), "expected VMs running Nginx")
					break
				}
			}
			gm.Expect(found).To(gm.BeTrue(), "Nginx application not found in response")
		})

		// Given an agent that has collected inventory from vcsim
		// When listing applications for the collection
		// Then SAP HANA Database should be detected with VmCount > 0
		ginkgo.It("should detect SAP HANA Database on sap VMs", func() {
			result, err := agentSvc.ListApplications(collectionID)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			var found bool
			for _, app := range result.Applications {
				if app.Name == "SAP HANA Database" {
					found = true
					gm.Expect(app.VmCount).To(gm.BeNumerically(">", 0), "expected VMs running SAP HANA Database")
					break
				}
			}
			gm.Expect(found).To(gm.BeTrue(), "SAP HANA Database application not found in response")
		})

		// Given an agent that has collected inventory from vcsim
		// When listing applications for the collection
		// Then no application should have VmCount of zero
		ginkgo.It("should not include applications with zero matching VMs", func() {
			result, err := agentSvc.ListApplications(collectionID)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			for _, app := range result.Applications {
				gm.Expect(app.VmCount).To(gm.BeNumerically(">", 0),
					"application %q should not appear with zero VMs", app.Name)
			}
		})

		// Given an agent that has collected inventory from vcsim
		// When listing applications for the collection
		// Then applications should be returned sorted alphabetically by name
		ginkgo.It("should return applications sorted alphabetically", func() {
			result, err := agentSvc.ListApplications(collectionID)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			for i := 1; i < len(result.Applications); i++ {
				gm.Expect(result.Applications[i].Name >= result.Applications[i-1].Name).To(gm.BeTrue(),
					"expected %q >= %q", result.Applications[i].Name, result.Applications[i-1].Name)
			}
		})
	})

	ginkgo.Context("VM filtering by application", func() {
		// Given an agent that has collected inventory with Nginx detected
		// When filtering VMs by application = 'Nginx'
		// Then only VMs running Nginx should be returned
		ginkgo.It("should filter VMs by application name", func() {
			expr := "application = 'Nginx'"
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{ByExpression: &expr})
			gm.Expect(err).ToNot(gm.HaveOccurred())

			gm.Expect(result.Total).To(gm.BeNumerically(">", 0), "expected VMs with Nginx")
		})

		// Given an agent that has collected inventory with SAP HANA detected
		// When filtering VMs by application = 'SAP HANA Database'
		// Then only VMs running SAP HANA should be returned
		ginkgo.It("should filter VMs by SAP HANA application", func() {
			expr := "application = 'SAP HANA Database'"
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{ByExpression: &expr})
			gm.Expect(err).ToNot(gm.HaveOccurred())

			gm.Expect(result.Total).To(gm.BeNumerically(">", 0), "expected VMs with SAP HANA Database")
		})

		// Given an agent that has collected inventory
		// When filtering VMs by a non-existent application name
		// Then no VMs should be returned
		ginkgo.It("should return empty result for non-existent application", func() {
			expr := "application = 'NonExistentApp'"
			result, err := agentSvc.ListLatestVMs(&service.VMListParams{ByExpression: &expr})
			gm.Expect(err).ToNot(gm.HaveOccurred())

			gm.Expect(result.Total).To(gm.Equal(0))
		})
	})

	ginkgo.Context("Group applications", func() {
		ginkgo.It("should return applications for a group matching all VMs", func() {
			group, err := agentSvc.CreateGroup("all-vms-apps", "memory > 0", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

			groupApps, err := agentSvc.ListGroupApplications(group.Id)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			allApps, err := agentSvc.ListApplications(collectionID)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			ginkgo.GinkgoWriter.Printf("Group apps: %d, All apps: %d\n", len(groupApps.Applications), len(allApps.Applications))
			gm.Expect(len(groupApps.Applications)).To(gm.Equal(len(allApps.Applications)))
		})

		ginkgo.It("should return empty applications for group with no matching VMs", func() {
			group, err := agentSvc.CreateGroup("no-vms-apps", "memory > 999999999", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

			result, err := agentSvc.ListGroupApplications(group.Id)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			ginkgo.GinkgoWriter.Printf("Empty group apps: %d\n", len(result.Applications))
			gm.Expect(result.Applications).To(gm.BeEmpty())
		})

		ginkgo.It("should return subset of applications for a filtered group", func() {
			pageSize := 1
			vms, err := agentSvc.ListLatestVMs(&service.VMListParams{PageSize: &pageSize})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(len(vms.VirtualMachines)).To(gm.BeNumerically(">", 0))

			vmName := vms.VirtualMachines[0].Name
			group, err := agentSvc.CreateGroup("single-vm-apps", fmt.Sprintf("name = '%s'", vmName), "")
			gm.Expect(err).ToNot(gm.HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

			groupApps, err := agentSvc.ListGroupApplications(group.Id)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			allApps, err := agentSvc.ListApplications(collectionID)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			ginkgo.GinkgoWriter.Printf("Single VM '%s' apps: %d, All apps: %d\n", vmName, len(groupApps.Applications), len(allApps.Applications))
			gm.Expect(len(groupApps.Applications)).To(gm.BeNumerically("<=", len(allApps.Applications)))

			for _, app := range groupApps.Applications {
				for _, vm := range app.Vms {
					gm.Expect(vm.Name).To(gm.Equal(vmName),
						"app %q should only contain VM %s", app.Name, vmName)
				}
			}
		})
	})
})
