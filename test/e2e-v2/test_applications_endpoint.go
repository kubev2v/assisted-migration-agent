package main

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	gm "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/vcsim"
	"github.com/kubev2v/assisted-migration-agent/test/e2e-v2/service"
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
		if cfg.InfraMode == "container" {
			_, err = infraManager.AddVMs([]vcsim.VM{
				{
					Name: "product-datasync-exact",
					VAppProducts: []vcsim.VAppProduct{{
						Key: 1, ClassID: "com.amazonaws.datasync", InstanceID: "datasync-1",
						Name: "DataSync", Vendor: "AWS", Version: "2.0",
						FullVersion: "2.0-build-7", VendorURL: "https://aws.amazon.com",
						ProductURL: "https://aws.amazon.com/datasync", AppURL: "https://example.test/datasync",
					}},
				},
				{
					Name:         "product-datasync-near",
					VAppProducts: []vcsim.VAppProduct{{Key: 1, Name: "DataSync Extended", Vendor: "AWS"}},
				},
				{
					Name:         "product-tenable-both",
					GuestApps:    []vcsim.AppInfo{{Name: "nessusd", Version: "10.0"}},
					VAppProducts: []vcsim.VAppProduct{{Key: 1, Name: "Tenable Core Appliance", Vendor: "Tenable"}},
				},
				{
					Name:      "product-tenable-process-only",
					GuestApps: []vcsim.AppInfo{{Name: "nessusd", Version: "10.0"}},
				},
				{
					Name:         "product-tenable-product-only",
					VAppProducts: []vcsim.VAppProduct{{Key: 1, Name: "Tenable Core Appliance", Vendor: "Tenable"}},
				},
			})
			gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to add product VMs")
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

	ginkgo.Context("vApp product detection", func() {
		ginkgo.BeforeEach(func() {
			if cfg.InfraMode != "container" {
				ginkgo.Skip("product fixture requires mutable vcsim")
			}
		})

		ginkgo.It("should preserve product metadata in VM detail", func() {
			expr := "name = 'product-datasync-exact'"
			vms, err := agentSvc.ListVMs(collectionID, &service.VMListParams{ByExpression: &expr})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(vms.VirtualMachines).To(gm.HaveLen(1))

			detail, err := agentSvc.GetVM(collectionID, vms.VirtualMachines[0].Id)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(detail.Products).ToNot(gm.BeNil())
			gm.Expect(*detail.Products).To(gm.HaveLen(1))
			product := (*detail.Products)[0]
			gm.Expect(product.Key).To(gm.Equal(int32(1)))
			gm.Expect(product.ClassId).To(gm.HaveValue(gm.Equal("com.amazonaws.datasync")))
			gm.Expect(product.InstanceId).To(gm.HaveValue(gm.Equal("datasync-1")))
			gm.Expect(product.Name).To(gm.HaveValue(gm.Equal("DataSync")))
			gm.Expect(product.Vendor).To(gm.HaveValue(gm.Equal("AWS")))
			gm.Expect(product.Version).To(gm.HaveValue(gm.Equal("2.0")))
			gm.Expect(product.FullVersion).To(gm.HaveValue(gm.Equal("2.0-build-7")))
			gm.Expect(product.VendorUrl).To(gm.HaveValue(gm.Equal("https://aws.amazon.com")))
			gm.Expect(product.ProductUrl).To(gm.HaveValue(gm.Equal("https://aws.amazon.com/datasync")))
			gm.Expect(product.AppUrl).To(gm.HaveValue(gm.Equal("https://example.test/datasync")))
		})

		ginkgo.It("should detect exact and combined product rules only on matching VMs", func() {
			result, err := agentSvc.ListApplications(collectionID)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			found := make(map[string][]string)
			for _, app := range result.Applications {
				if app.Name != "DataSync" && app.Name != "Tenable Core" {
					continue
				}
				gm.Expect(app.VmCount).To(gm.Equal(1), "unexpected VM count for %s", app.Name)
				for _, vm := range app.Vms {
					found[app.Name] = append(found[app.Name], vm.Name)
				}
			}
			gm.Expect(found["DataSync"]).To(gm.Equal([]string{"product-datasync-exact"}))
			gm.Expect(found["Tenable Core"]).To(gm.Equal([]string{"product-tenable-both"}))
		})

		ginkgo.It("should filter VMs by product and detected application", func() {
			expr := "product.name = 'DataSync'"
			productVMs, err := agentSvc.ListVMs(collectionID, &service.VMListParams{ByExpression: &expr})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(productVMs.Total).To(gm.Equal(1))
			gm.Expect(productVMs.VirtualMachines[0].Name).To(gm.Equal("product-datasync-exact"))

			expr = "application = 'Tenable Core'"
			appVMs, err := agentSvc.ListVMs(collectionID, &service.VMListParams{ByExpression: &expr})
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(appVMs.Total).To(gm.Equal(1))
			gm.Expect(appVMs.VirtualMachines[0].Name).To(gm.Equal("product-tenable-both"))
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
