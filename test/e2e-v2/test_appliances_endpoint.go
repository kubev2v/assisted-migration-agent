package main

import (
	"time"

	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	gm "github.com/onsi/gomega"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"

	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/vcsim"
	"github.com/kubev2v/assisted-migration-agent/test/e2e-v2/service"
)

var _ = ginkgo.Describe("Appliances endpoint v2 e2e tests", ginkgo.Ordered, func() {
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
		if cfg.InfraMode == "container" {
			_, err = infraManager.AddVMs([]vcsim.VM{
				{
					Name:         "appliance-datasync-a",
					VAppProducts: []vcsim.VAppProduct{{Key: 1, Name: "DataSync", Vendor: "AWS", Version: "2.0"}},
				},
				{
					Name:         "appliance-datasync-b",
					VAppProducts: []vcsim.VAppProduct{{Key: 1, Name: "DataSync", Vendor: "AWS", Version: "2.0"}},
				},
				{
					Name:         "appliance-bigip",
					VAppProducts: []vcsim.VAppProduct{{Key: 1, Name: "BIG-IP", Vendor: "F5", Version: "17.1"}},
				},
				{
					Name: "appliance-none",
				},
			})
			gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to add appliance VMs")
		}

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
			ginkgo.GinkgoWriter.Printf("Collections: %d\n", len(collections.Collections))
			return len(collections.Collections)
		}, 120*time.Second, 2*time.Second).Should(gm.BeNumerically(">", 0), "expected at least 1 collection")

		collections, err := agentSvc.ListCollections()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to list collections")
		collectionID = collections.Collections[0].Id
		ginkgo.GinkgoWriter.Printf("Using collection: %s\n", collectionID)

		ginkgo.GinkgoWriter.Println("Appliances endpoint v2 test setup complete")
	})

	ginkgo.AfterAll(func() {
		ginkgo.GinkgoWriter.Println("Cleaning up appliances endpoint v2 tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	findAppliance := func(appliances []v2.Appliance, name string) v2.Appliance {
		for _, appliance := range appliances {
			if appliance.Name == name {
				return appliance
			}
		}
		ginkgo.Fail("appliance not found: " + name)
		return v2.Appliance{}
	}

	ginkgo.Context("Collection appliances", func() {
		ginkgo.BeforeEach(func() {
			if cfg.InfraMode != "container" {
				ginkgo.Skip("appliance fixtures require mutable vcsim")
			}
		})

		ginkgo.It("should group the same product across its VMs", func() {
			// Act
			result, err := agentSvc.ListAppliances(collectionID)

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())

			dataSync := findAppliance(result.Appliances, "DataSync")
			gm.Expect(dataSync.Vendor).To(gm.Equal("AWS"))
			gm.Expect(dataSync.Version).To(gm.Equal("2.0"))
			gm.Expect(dataSync.Confidence).To(gm.Equal(100))
			gm.Expect(dataSync.VmCount).To(gm.Equal(2))

			vmNames := make([]string, 0, len(dataSync.Vms))
			for _, vm := range dataSync.Vms {
				vmNames = append(vmNames, vm.Name)
			}
			gm.Expect(vmNames).To(gm.ConsistOf("appliance-datasync-a", "appliance-datasync-b"))
		})

		ginkgo.It("should return the VM for a single-VM appliance", func() {
			// Act
			result, err := agentSvc.ListAppliances(collectionID)

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())

			bigIP := findAppliance(result.Appliances, "BIG-IP")
			gm.Expect(bigIP.Vendor).To(gm.Equal("F5"))
			gm.Expect(bigIP.Version).To(gm.Equal("17.1"))
			gm.Expect(bigIP.VmCount).To(gm.Equal(1))
			gm.Expect(bigIP.Vms).To(gm.HaveLen(1))
			gm.Expect(bigIP.Vms[0].Name).To(gm.Equal("appliance-bigip"))
		})

		ginkgo.It("should not include VMs without vApp products", func() {
			// Act
			result, err := agentSvc.ListAppliances(collectionID)

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())

			for _, appliance := range result.Appliances {
				for _, vm := range appliance.Vms {
					gm.Expect(vm.Name).ToNot(gm.Equal("appliance-none"),
						"appliance %q should not include a VM without products", appliance.Name)
				}
			}
		})
	})

	ginkgo.Context("Group appliances", func() {
		ginkgo.BeforeEach(func() {
			if cfg.InfraMode != "container" {
				ginkgo.Skip("appliance fixtures require mutable vcsim")
			}
		})

		ginkgo.It("should return appliances for a group matching all VMs", func() {
			// Arrange
			group, err := agentSvc.CreateGroup("all-vms-appliances", "memory > 0", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

			// Act
			groupAppliances, err := agentSvc.ListGroupAppliances(group.Id)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			allAppliances, err := agentSvc.ListAppliances(collectionID)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Assert
			gm.Expect(groupAppliances.Appliances).To(gm.HaveLen(len(allAppliances.Appliances)))
		})

		ginkgo.It("should return empty appliances for a group with no matching VMs", func() {
			// Arrange
			group, err := agentSvc.CreateGroup("no-vms-appliances", "memory > 999999999", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

			// Act
			result, err := agentSvc.ListGroupAppliances(group.Id)

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.Appliances).ToNot(gm.BeNil())
			gm.Expect(result.Appliances).To(gm.BeEmpty())
		})

		ginkgo.It("should return the appliance of the group's VM", func() {
			// Arrange
			group, err := agentSvc.CreateGroup("single-vm-appliances", "name = 'appliance-datasync-a'", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

			// Act
			result, err := agentSvc.ListGroupAppliances(group.Id)

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.Appliances).To(gm.HaveLen(1))
			dataSync := findAppliance(result.Appliances, "DataSync")
			gm.Expect(dataSync.VmCount).To(gm.Equal(1))
			gm.Expect(dataSync.Vms).To(gm.HaveLen(1))
			gm.Expect(dataSync.Vms[0].Name).To(gm.Equal("appliance-datasync-a"))
		})

		ginkgo.It("should return empty appliances for a group whose VMs have none", func() {
			// Arrange
			group, err := agentSvc.CreateGroup("no-product-appliances", "name = 'appliance-none'", "")
			gm.Expect(err).ToNot(gm.HaveOccurred())
			defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

			// Act
			result, err := agentSvc.ListGroupAppliances(group.Id)

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.Appliances).ToNot(gm.BeNil())
			gm.Expect(result.Appliances).To(gm.BeEmpty())
		})
	})
})
