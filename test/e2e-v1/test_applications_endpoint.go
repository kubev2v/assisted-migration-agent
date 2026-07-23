package main

import (
	"crypto/tls"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2" // nolint:staticcheck
	. "github.com/onsi/gomega"    // nolint:staticcheck

	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e-v1/service"

	"github.com/google/uuid"
)

var _ = Describe("Applications endpoint e2e tests", Ordered, func() {
	var agentSvc *service.AgentSvc

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

		GinkgoWriter.Println("Applications endpoint test setup complete")
	})

	AfterAll(func() {
		GinkgoWriter.Println("Cleaning up applications endpoint test infra...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	Context("Application detection", func() {
		It("should detect Nginx on workload VMs", func() {
			result, err := agentSvc.ListApplications()
			Expect(err).ToNot(HaveOccurred())

			var found bool
			for _, app := range result.Applications {
				if app.Name == "Nginx" {
					found = true
					Expect(app.VmCount).To(Equal(3), "expected 3 VMs running Nginx")
					Expect(app.Vms).To(HaveLen(3))

					vmNames := make([]string, len(app.Vms))
					for i, vm := range app.Vms {
						vmNames[i] = vm.Name
					}
					Expect(vmNames).To(ContainElements("test-vm-18", "test-vm-19", "test-vm-20"))
					break
				}
			}
			Expect(found).To(BeTrue(), "Nginx application not found in response")
		})

		It("should detect SAP HANA Database on sap VMs", func() {
			result, err := agentSvc.ListApplications()
			Expect(err).ToNot(HaveOccurred())

			var found bool
			for _, app := range result.Applications {
				if app.Name == "SAP HANA Database" {
					found = true
					Expect(app.VmCount).To(Equal(3), "expected 3 VMs running SAP HANA")
					Expect(app.Vms).To(HaveLen(3))

					vmNames := make([]string, len(app.Vms))
					for i, vm := range app.Vms {
						vmNames[i] = vm.Name
					}
					Expect(vmNames).To(ContainElements("test-vm-35", "test-vm-36", "test-vm-37"))
					break
				}
			}
			Expect(found).To(BeTrue(), "SAP HANA Database application not found in response")
		})

		It("should not include applications with zero matching VMs", func() {
			result, err := agentSvc.ListApplications()
			Expect(err).ToNot(HaveOccurred())

			for _, app := range result.Applications {
				Expect(app.VmCount).To(BeNumerically(">", 0),
					"application %q should not appear with zero VMs", app.Name)
			}
		})

		It("should return applications sorted alphabetically", func() {
			result, err := agentSvc.ListApplications()
			Expect(err).ToNot(HaveOccurred())

			for i := 1; i < len(result.Applications); i++ {
				Expect(result.Applications[i].Name >= result.Applications[i-1].Name).To(BeTrue(),
					"expected %q >= %q", result.Applications[i].Name, result.Applications[i-1].Name)
			}
		})
	})

	Context("VM filtering by application", func() {
		It("should filter VMs by application name", func() {
			expr := "application = 'Nginx'"
			result, err := agentSvc.ListVMs(&service.VMListParams{ByExpression: &expr})
			Expect(err).ToNot(HaveOccurred())

			Expect(result.Total).To(Equal(3), "expected 3 VMs with Nginx")
		})

		It("should filter VMs by SAP HANA application", func() {
			expr := "application = 'SAP HANA Database'"
			result, err := agentSvc.ListVMs(&service.VMListParams{ByExpression: &expr})
			Expect(err).ToNot(HaveOccurred())

			Expect(result.Total).To(Equal(3), "expected 3 VMs with SAP HANA")
		})

		It("should return empty result for non-existent application", func() {
			expr := "application = 'NonExistentApp'"
			result, err := agentSvc.ListVMs(&service.VMListParams{ByExpression: &expr})
			Expect(err).ToNot(HaveOccurred())

			Expect(result.Total).To(Equal(0))
		})
	})
})
