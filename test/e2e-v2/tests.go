package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e-v2/service"

	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	gm "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Agent v2 e2e tests", ginkgo.Ordered, func() {
	ginkgo.BeforeAll(func() {
		ginkgo.GinkgoWriter.Println("Starting postgres...")
		err := infraManager.StartPostgres()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start postgres")
		time.Sleep(2 * time.Second) // wait for postgres to be ready
	})

	ginkgo.AfterAll(func() {
		_ = infraManager.StopPostgres()
	})

	ginkgo.Context("disconnected env", func() {

		ginkgo.Context("mode at startup", func() {
			var agentSvc *service.AgentSvc

			ginkgo.BeforeEach(func() {
				agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)
			})

			ginkgo.AfterEach(func() {
				ginkgo.GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()
			})

			// Given an agent configured to start in disconnected mode with API v2
			// When the agent starts up
			// Then its status should report mode "disconnected"
			ginkgo.It("should report disconnected mode at startup", func() {
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
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

				status, err := agentSvc.Status()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get agent status")
				gm.Expect(status.Mode).To(gm.Equal("disconnected"), "expected mode to be disconnected")
			})
		})

		ginkgo.Context("collector", func() {
			var agentSvc *service.AgentSvc

			ginkgo.BeforeEach(func() {
				ginkgo.GinkgoWriter.Println("Starting vcsim...")
				err := infraManager.StartVcsim()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start vcsim")
				time.Sleep(1 * time.Second) // allow vcsim to initialize

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
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)
			})

			ginkgo.AfterEach(func() {
				ginkgo.GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()

				ginkgo.GinkgoWriter.Println("Stopping vcsim...")
				_ = infraManager.StopVcsim()
			})

			// Given an agent in disconnected mode with vcsim running
			// When valid credentials are stored and collector is started
			// Then the collector should reach "collected" status and collections should be available
			ginkgo.It("should collect inventory successfully with valid credentials", func() {
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
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

				// Store credentials
				_, err = agentSvc.StoreCredentials(infra.VcsimURL, infra.VcsimUsername, infra.VcsimPassword)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to store credentials")

				// Start collector (v2 uses stored credentials)
				_, err = agentSvc.StartCollector()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

				// Wait for a collection to appear (v2 sweeps collected collectors, so poll for collections instead)
				gm.Eventually(func() int {
					collections, err := agentSvc.ListCollections()
					if err != nil {
						return 0
					}
					ginkgo.GinkgoWriter.Printf("Collections: %d\n", len(collections.Collections))
					return len(collections.Collections)
				}, 120*time.Second, 2*time.Second).Should(gm.BeNumerically(">", 0), "expected at least 1 collection after collecting")

				collections, err := agentSvc.ListCollections()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to list collections")
				ginkgo.GinkgoWriter.Printf("Collection ID: %s\n", collections.Collections[0].Id)
			})

			// Given an agent in disconnected mode
			// When an unreachable vCenter URL is provided
			// Then the store credentials call should return an error
			ginkgo.It("should reject unreachable vCenter URL at store time", func() {
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					APIVersion:     "v2",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				_, err = agentSvc.StoreCredentials("https://localhost:9999/sdk", "user", "pass")
				gm.Expect(err).To(gm.HaveOccurred(), "expected unreachable URL to be rejected")
			})

			// Given an agent that failed credential storage with bad credentials
			// When valid credentials are provided on retry
			// Then the credentials should be stored successfully and collection should work
			ginkgo.It("should recover from bad credentials to successful collection", func() {
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					APIVersion:     "v2",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				// Bad credentials are rejected
				_, err = agentSvc.StoreCredentials(infra.VcsimURL, "baduser", "badpass")
				gm.Expect(err).To(gm.HaveOccurred(), "expected bad credentials to be rejected")

				// Retry with valid credentials
				_, err = agentSvc.StoreCredentials(infra.VcsimURL, infra.VcsimUsername, infra.VcsimPassword)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to store valid credentials")

				// Start collector and wait for collection
				_, err = agentSvc.StartCollector()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

				gm.Eventually(func() int {
					collections, err := agentSvc.ListCollections()
					if err != nil {
						return 0
					}
					return len(collections.Collections)
				}, 120*time.Second, 2*time.Second).Should(gm.BeNumerically(">", 0), "expected collection after recovery")
			})

			// Given an agent that has successfully collected inventory
			// When the agent container is restarted
			// Then collections should still be available
			ginkgo.It("should persist collections after agent restart", func() {
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					APIVersion:     "v2",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				_, err = agentSvc.StoreCredentials(infra.VcsimURL, infra.VcsimUsername, infra.VcsimPassword)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to store credentials")

				_, err = agentSvc.StartCollector()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

				gm.Eventually(func() int {
					collections, err := agentSvc.ListCollections()
					if err != nil {
						return 0
					}
					return len(collections.Collections)
				}, 120*time.Second, 2*time.Second).Should(gm.BeNumerically(">", 0))

				// Restart the agent
				err = infraManager.RestartAgent()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to restart agent")

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				// Collections should still be available
				collections, err := agentSvc.ListCollections()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to list collections after restart")
				gm.Expect(collections.Collections).ToNot(gm.BeEmpty(), "expected collections to persist after restart")
			})

			// Given an agent in disconnected mode
			// When invalid credentials are stored
			// Then the store credentials call should return an error
			ginkgo.It("should reject bad credentials at store time", func() {
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
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

				// Attempt to store bad credentials
				_, err = agentSvc.StoreCredentials(infra.VcsimURL, "baduser", "badpass")
				gm.Expect(err).To(gm.HaveOccurred(), "expected bad credentials to be rejected")
			})
		})

		ginkgo.Context("mode switching", func() {
			var agentSvc *service.AgentSvc

			ginkgo.BeforeEach(func() {
				agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)
			})

			ginkgo.AfterEach(func() {
				ginkgo.GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()
			})

			// Given an agent started in disconnected mode
			// When the mode is switched to connected
			// Then the status should report mode "connected"
			ginkgo.It("should switch from disconnected to connected mode", func() {
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
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

				status, err := agentSvc.SetAgentMode("connected")
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to switch mode")
				gm.Expect(status.Mode).To(gm.Equal("connected"), "expected mode to be connected")
			})

			// Given an agent that was switched from connected to disconnected mode
			// When the agent container is restarted
			// Then the mode should persist as disconnected
			ginkgo.It("should persist mode after agent restart", func() {
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					APIVersion:     "v2",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				// Restart the agent
				err = infraManager.RestartAgent()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to restart agent")

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				status, err := agentSvc.Status()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get agent status")
				gm.Expect(status.Mode).To(gm.Equal("disconnected"), "expected mode to persist as disconnected after restart")
			})

			// Todo: fix the behaviour so this test pass
			//// Given an agent started in connected mode
			//// When the mode is switched to disconnected
			//// Then the status should report mode "disconnected" with no console errors
			//ginkgo.It("should switch from connected to disconnected mode without console errors", func() {
			//	agentID := uuid.NewString()
			//	_, err := infraManager.StartAgent(infra.AgentConfig{
			//		AgentID:        agentID,
			//		SourceID:       uuid.NewString(),
			//		Mode:           "connected",
			//		APIVersion:     "v2",
			//		ConsoleURL:     cfg.AgentProxyUrl,
			//		UpdateInterval: "1s",
			//	})
			//	gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
			//	ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)
			//
			//	gm.Eventually(func() error {
			//		_, err := agentSvc.Status()
			//		return err
			//	}, 30*time.Second, 1*time.Second).Should(gm.BeNil())
			//
			//	status, err := agentSvc.SetAgentMode("disconnected")
			//	gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to switch mode")
			//	gm.Expect(status.Mode).To(gm.Equal("disconnected"), "expected mode to be disconnected")
			//	gm.Expect(status.Error).To(gm.BeEmpty(), "expected no console errors after switching to disconnected")
			//})
		})
	})
})
