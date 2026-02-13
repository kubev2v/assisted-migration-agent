package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/test/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e/service"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

var _ = Describe("Agent e2e tests", Ordered, func() {
	BeforeAll(func() {
		GinkgoWriter.Println("Starting postgres...")
		err := infraManager.StartPostgres()
		Expect(err).ToNot(HaveOccurred(), "failed to start postgres")
		time.Sleep(2 * time.Second) // wait for postgres to be ready
	})

	AfterAll(func() {
		_ = infraManager.StopPostgres()
	})

	Context("disconnected env", func() {
		var (
			proxy    *infra.Proxy
			requests chan infra.Request
			obs      *infra.Observer
		)

		BeforeAll(func() {
			target, err := url.Parse(cfg.BackendAgentEndpoint)
			Expect(err).ToNot(HaveOccurred(), "failed to parse backend endpoint")

			proxy, requests = infra.NewObservableProxy("agent-proxy", "backend", target, ":8080")
			time.Sleep(100 * time.Millisecond)
			GinkgoWriter.Println("Proxy started on :8080")
		})

		AfterAll(func() {
			proxy.Stop()
		})

		Context("mode at startup", func() {
			var agentSvc *service.AgentSvc

			BeforeEach(func() {
				obs = infra.NewObserver(requests)
				agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)
			})

			AfterEach(func() {
				if cfg.KeepContainers {
					GinkgoWriter.Println("Keeping agent container running (--keep-containers flag set)")
					obs.Close()
					return
				}
				GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()
				obs.Close()
			})

			// Given an agent configured to start in disconnected mode
			// When the agent starts up
			// Then no requests should be made to the console
			It("should not make requests to console when starting in disconnected mode", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				// Act
				time.Sleep(5 * time.Second)
				reqs := obs.Requests()

				// Assert
				GinkgoWriter.Printf("Observed %d requests\n", len(reqs))
				Expect(reqs).To(BeEmpty(), "expected no requests in disconnected mode")
			})

			// Given an agent configured to start in connected mode
			// When the agent starts up
			// Then requests should be made to the console
			It("should make requests to console when starting in connected mode", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "connected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				// Act
				time.Sleep(5 * time.Second)
				reqs := obs.Requests()

				// Assert
				GinkgoWriter.Printf("Observed %d requests\n", len(reqs))
				Expect(reqs).ToNot(BeEmpty(), "expected requests in connected mode")
			})
		})

		Context("mode switching", func() {
			var agentSvc *service.AgentSvc

			BeforeEach(func() {
				obs = infra.NewObserver(requests)
				agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)
			})

			AfterEach(func() {
				if cfg.KeepContainers {
					GinkgoWriter.Println("Keeping agent container running (--keep-containers flag set)")
					obs.Close()
					return
				}
				GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()
				obs.Close()
			})

			// Given an agent running in connected mode making requests to console
			// When the agent mode is switched to disconnected
			// Then no new requests should be made to the console
			It("should stop making requests after switching from connected to disconnected mode", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "connected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				time.Sleep(5 * time.Second)
				initialReqs := obs.Requests()
				GinkgoWriter.Printf("Observed %d requests before mode switch\n", len(initialReqs))
				Expect(initialReqs).ToNot(BeEmpty(), "expected requests in connected mode")

				// Act
				status, err := agentSvc.SetAgentMode("disconnected")
				Expect(err).ToNot(HaveOccurred(), "failed to switch mode")
				Expect(status.Mode).To(Equal("disconnected"), "expected mode to be disconnected")

				time.Sleep(5 * time.Second)
				afterSwitchReqs := obs.Requests()

				// Assert
				GinkgoWriter.Printf("Observed %d requests after mode switch\n", len(afterSwitchReqs))
				Expect(len(afterSwitchReqs)).To(Equal(len(initialReqs)), "expected no new requests after switching to disconnected")
			})

			// Given an agent running in disconnected mode making no requests
			// When the agent mode is switched to connected
			// Then requests should start being made to the console
			It("should start making requests after switching from disconnected to connected mode", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				initialReqs := obs.Requests()
				GinkgoWriter.Printf("Observed %d requests before mode switch\n", len(initialReqs))
				Expect(initialReqs).To(BeEmpty(), "expected no requests in disconnected mode")

				// Act
				status, err := agentSvc.SetAgentMode("connected")
				Expect(err).ToNot(HaveOccurred(), "failed to switch mode")
				Expect(status.Mode).To(Equal("connected"), "expected mode to be connected")

				time.Sleep(5 * time.Second)
				afterSwitchReqs := obs.Requests()

				// Assert
				GinkgoWriter.Printf("Observed %d requests after mode switch\n", len(afterSwitchReqs))
				Expect(afterSwitchReqs).ToNot(BeEmpty(), "expected requests after switching to connected mode")
			})

			// Given an agent that was switched from connected to disconnected mode
			// When the agent container is restarted
			// Then the mode should persist as disconnected
			It("should persist mode after agent restart", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "connected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				status, err := agentSvc.SetAgentMode("disconnected")
				Expect(err).ToNot(HaveOccurred(), "failed to switch mode")
				Expect(status.Mode).To(Equal("disconnected"), "expected mode to be disconnected")

				// Act
				err = infraManager.RestartAgent()
				Expect(err).ToNot(HaveOccurred(), "failed to restart agent")

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				status, err = agentSvc.Status()
				Expect(err).ToNot(HaveOccurred(), "failed to get agent status")

				// Assert
				GinkgoWriter.Printf("Agent mode after restart: %s\n", status.Mode)
				Expect(status.Mode).To(Equal("disconnected"), "expected mode to persist as disconnected after restart")
			})
		})

		Context("collector", func() {
			var agentSvc *service.AgentSvc

			BeforeEach(func() {
				GinkgoWriter.Println("Starting vcsim...")
				err := infraManager.StartVcsim()
				Expect(err).ToNot(HaveOccurred(), "failed to start vcsim")
				time.Sleep(1 * time.Second) // allow vcsim to initialize

				client := &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
					},
				}

				Eventually(func() error {
					resp, err := client.Get("https://localhost:8989/sdk")
					if err != nil {
						return err
					}
					defer resp.Body.Close()
					if resp.StatusCode >= 500 {
						return fmt.Errorf("server error: %d", resp.StatusCode)
					}
					return nil
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				obs = infra.NewObserver(requests)
				agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)
			})

			AfterEach(func() {
				if cfg.KeepContainers {
					GinkgoWriter.Println("Keeping agent and vcsim containers running (--keep-containers flag set)")
					obs.Close()
					return
				}
				GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()
				obs.Close()

				GinkgoWriter.Println("Stopping vcsim...")
				_ = infraManager.StopVcsim()
			})

			// Given an agent in disconnected mode with vcsim running
			// When valid vCenter credentials are provided to the collector
			// Then the collector should reach "collected" status and inventory should be available
			It("should collect inventory successfully with valid credentials", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				// Act
				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", infra.VcsimUsername, infra.VcsimPassword)
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					GinkgoWriter.Printf("Collector status: %s %s\n", status.Status, status.Error)
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(Equal("collected"))

				inventory, err := agentSvc.Inventory()
				Expect(err).ToNot(HaveOccurred(), "failed to get inventory")

				// Assert
				Expect(inventory).ToNot(BeNil(), "expected inventory to be available")
			})

			// Given an agent in disconnected mode with vcsim running
			// When invalid credentials are provided to the collector
			// Then the collector should reach "error" status
			It("should report error with bad credentials", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				// Act
				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", "baduser", "badpass")
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				// Assert
				Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return ""
					}
					GinkgoWriter.Printf("Collector status: %s\n", status.Status)
					return status.Status
				}, 30*time.Second, 2*time.Second).Should(Equal("error"))
			})

			// Given an agent in disconnected mode
			// When an unreachable vCenter URL is provided to the collector
			// Then the collector should reach "error" status
			It("should report error with bad vCenter URL", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				// Act
				_, err = agentSvc.StartCollector("https://localhost:9999/sdk", "user", "pass")
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				// Assert
				Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return ""
					}
					GinkgoWriter.Printf("Collector status: %s\n", status.Status)
					return status.Status
				}, 30*time.Second, 2*time.Second).Should(Equal("error"))
			})

			// Given an agent in disconnected mode that failed collection with bad credentials
			// When valid credentials are provided on retry
			// Then the collector should recover and reach "collected" status
			It("should recover from bad credentials to successful collection", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", "baduser", "badpass")
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return ""
					}
					return status.Status
				}, 30*time.Second, 2*time.Second).Should(Equal("error"))

				// Act
				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", infra.VcsimUsername, infra.VcsimPassword)
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					GinkgoWriter.Printf("Collector status: %s\n", status.Status)
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(Equal("collected"))

				inventory, err := agentSvc.Inventory()
				Expect(err).ToNot(HaveOccurred(), "failed to get inventory")

				// Assert
				Expect(inventory).ToNot(BeNil(), "expected inventory to be available after recovery")
			})

			// Given an agent that has successfully collected inventory
			// When the agent container is restarted
			// Then the collector status should persist as "collected" and inventory should still be available
			It("should persist collected state and inventory after agent restart", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", infra.VcsimUsername, infra.VcsimPassword)
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(Equal("collected"))

				inventory, err := agentSvc.Inventory()
				Expect(err).ToNot(HaveOccurred(), "failed to get inventory")
				Expect(inventory).ToNot(BeNil(), "expected inventory to be available before restart")

				// Act
				err = infraManager.RestartAgent()
				Expect(err).ToNot(HaveOccurred(), "failed to restart agent")

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				collectorStatus, err := agentSvc.GetCollectorStatus()
				Expect(err).ToNot(HaveOccurred(), "failed to get collector status")

				inventory, err = agentSvc.Inventory()
				Expect(err).ToNot(HaveOccurred(), "failed to get inventory after restart")

				// Assert
				GinkgoWriter.Printf("Collector status after restart: %s\n", collectorStatus.Status)
				Expect(collectorStatus.Status).To(Equal("collected"), "expected collector status to persist as collected after restart")
				Expect(inventory).ToNot(BeNil(), "expected inventory to be available after restart")
			})
		})
	})

	Context("connected env", func() {
		var (
			plannerSvc *service.PlannerSvc
			proxy      *infra.Proxy
			oidcProxy  *infra.Proxy
			obs        *infra.Observer
		)

		BeforeAll(func() {
			// Start OIDC server before the backend so JWKS URL is available
			GinkgoWriter.Println("Starting OIDC server...")
			err := infraManager.StartOIDC(":9090")
			Expect(err).ToNot(HaveOccurred(), "failed to start OIDC server")

			// Add a proxy between backend and OIDC
			oidcUrl, _ := url.Parse("http://localhost:9090")
			oidcProxy = infra.NewProxy("oidc-proxy", "oidc", oidcUrl, ":8082")

			GinkgoWriter.Println("Starting backend...")
			err = infraManager.StartBackend()
			Expect(err).ToNot(HaveOccurred(), "failed to start backend")

			// Wait for backend to be ready
			Eventually(func() error {
				resp, err := http.DefaultClient.Get(cfg.BackendAgentEndpoint + "/health")
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				if resp.StatusCode >= 500 {
					return fmt.Errorf("server error: %d", resp.StatusCode)
				}
				return nil
			}, 30*time.Second, 1*time.Second).Should(BeNil())

			plannerSvc = service.NewPlannerServiceWithOIDC(cfg.BackendUserEndpoint, infraManager.GenerateToken)

			// Start proxy between agent and backend for logging
			target, err := url.Parse(cfg.BackendAgentEndpoint)
			Expect(err).ToNot(HaveOccurred(), "failed to parse backend endpoint")

			var requests chan infra.Request
			proxy, requests = infra.NewObservableProxy("agent-proxy", "backend", target, ":8081")
			obs = infra.NewObserver(requests)

			time.Sleep(100 * time.Millisecond)
			GinkgoWriter.Println("Proxy started on :8081")
		})

		AfterAll(func() {
			if proxy != nil {
				proxy.Stop()
			}
			if oidcProxy != nil {
				oidcProxy.Stop()
			}
			if obs != nil {
				obs.Close()
			}
			_ = infraManager.StopBackend()
			_ = infraManager.StopOIDC()
		})

		Context("mode at startup", func() {
			var (
				agentSvc *service.AgentSvc
				sourceID openapi_types.UUID
				userSvc  *service.PlannerSvc
			)

			BeforeEach(func() {
				agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)
				userSvc = plannerSvc.WithAuthUser("admin", "admin", "admin@example.com")

				source, err := userSvc.CreateSource("test-source-" + uuid.NewString()[:8])
				Expect(err).ToNot(HaveOccurred(), "failed to create source")
				sourceID = source.Id
				GinkgoWriter.Printf("Created source: %s\n", sourceID)
			})

			AfterEach(func() {
				if cfg.KeepContainers {
					GinkgoWriter.Println("Keeping agent container running (--keep-containers flag set)")
					return
				}
				GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()

				GinkgoWriter.Println("Deleting source...")
				_ = userSvc.RemoveSource(sourceID)
			})

			// Given an agent configured in connected mode with a valid source ID
			// When the agent starts and registers with the backend
			// Then the source should have the agent attached with status "waiting-for-credentials"
			It("should register agent with backend when starting in connected mode", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       sourceID.String(),
					Mode:           "connected",
					ConsoleURL:     cfg.BackendAgentEndpoint,
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				// Act
				time.Sleep(10 * time.Second) // allow agent to register with backend
				source, err := userSvc.GetSource(sourceID)
				Expect(err).ToNot(HaveOccurred(), "failed to get source")

				// Assert
				GinkgoWriter.Printf("Source agent: %+v\n", source.Agent)
				Expect(source.Agent).ToNot(BeNil(), "expected agent to be attached to source")
				Expect(string(source.Agent.Status)).To(Equal("waiting-for-credentials"), "expected agent status to be waiting-for-credentials")
			})
		})

		Context("mode switch", func() {
			var (
				agentSvc *service.AgentSvc
				sourceID openapi_types.UUID
				userSvc  *service.PlannerSvc
			)

			BeforeEach(func() {
				agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)
				userSvc = plannerSvc.WithAuthUser("admin", "admin", "admin@example.com")

				source, err := userSvc.CreateSource("test-source-" + uuid.NewString()[:8])
				Expect(err).ToNot(HaveOccurred(), "failed to create source")
				sourceID = source.Id
				GinkgoWriter.Printf("Created source: %s\n", sourceID)
			})

			AfterEach(func() {
				if cfg.KeepContainers {
					GinkgoWriter.Println("Keeping agent container running (--keep-containers flag set)")
					return
				}
				GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()

				GinkgoWriter.Println("Deleting source...")
				_ = userSvc.RemoveSource(sourceID)
			})

			// Given an agent started in disconnected mode with a valid source ID
			// When the agent mode is switched to connected
			// Then the source should have the agent attached
			It("should register agent with backend after switching from disconnected to connected", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       sourceID.String(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.BackendAgentEndpoint,
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				// Act
				_, err = agentSvc.SetAgentMode("connected")
				Expect(err).ToNot(HaveOccurred(), "failed to switch mode")

				time.Sleep(5 * time.Second) // allow agent to register with backend
				source, err := userSvc.GetSource(sourceID)
				Expect(err).ToNot(HaveOccurred(), "failed to get source")

				// Assert
				GinkgoWriter.Printf("Source agent: %+v\n", source.Agent)
				Expect(source.Agent).ToNot(BeNil(), "expected agent to be attached to source after mode switch")
			})

			// Given an agent started in disconnected mode without inventory
			// When the agent mode is switched to connected
			// Then the agent should communicate with backend without errors
			It("should communicate with backend without errors when no inventory is collected", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       sourceID.String(),
					Mode:           "disconnected",
					ConsoleURL:     "http://localhost:8081", // Use proxy to observe requests
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				// Act - switch to connected mode without collecting inventory
				_, err = agentSvc.SetAgentMode("connected")
				Expect(err).ToNot(HaveOccurred(), "failed to switch mode")

				// Wait for agent to make requests to backend
				time.Sleep(5 * time.Second)

				// Assert - check agent status has no error
				status, err := agentSvc.Status()
				Expect(err).ToNot(HaveOccurred(), "failed to get agent status")
				GinkgoWriter.Printf("Agent status: mode=%s, console_connection=%s, error=%s\n",
					status.Mode, status.ConsoleConnection, status.Error)

				Expect(status.Mode).To(Equal("connected"), "expected mode to be connected")
				Expect(status.Error).To(BeEmpty(), "expected no error in agent status")

				// Assert - verify requests were made to backend via observer
				reqs := obs.Requests()
				GinkgoWriter.Printf("Observed %d requests to backend\n", len(reqs))
				Expect(reqs).ToNot(BeEmpty(), "expected requests to be made to backend")
			})
		})

		Context("collector", func() {
			var (
				agentSvc *service.AgentSvc
				sourceID openapi_types.UUID
				userSvc  *service.PlannerSvc
			)

			BeforeEach(func() {
				GinkgoWriter.Println("Starting vcsim...")
				err := infraManager.StartVcsim()
				Expect(err).ToNot(HaveOccurred(), "failed to start vcsim")
				time.Sleep(1 * time.Second) // allow vcsim to initialize

				// Wait for vcsim to be ready
				client := &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
					},
				}
				Eventually(func() error {
					resp, err := client.Get("https://localhost:8989/sdk")
					if err != nil {
						return err
					}
					defer resp.Body.Close()
					if resp.StatusCode >= 500 {
						return fmt.Errorf("server error: %d", resp.StatusCode)
					}
					return nil
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)
				userSvc = plannerSvc.WithAuthUser("admin", "admin", "admin@example.com")

				source, err := userSvc.CreateSource("test-source-" + uuid.NewString()[:8])
				Expect(err).ToNot(HaveOccurred(), "failed to create source")
				sourceID = source.Id
				GinkgoWriter.Printf("Created source: %s\n", sourceID)
			})

			AfterEach(func() {
				if cfg.KeepContainers {
					GinkgoWriter.Println("Keeping agent and vcsim containers running (--keep-containers flag set)")
					return
				}
				GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()

				GinkgoWriter.Println("Deleting source...")
				_ = userSvc.RemoveSource(sourceID)

				GinkgoWriter.Println("Stopping vcsim...")
				_ = infraManager.StopVcsim()
			})

			// Given an agent in connected mode with valid vCenter credentials
			// When the collector successfully gathers inventory
			// Then the source should have the inventory populated
			It("should push inventory to backend after successful collection", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       sourceID.String(),
					Mode:           "connected",
					ConsoleURL:     "http://localhost:8081",
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				// Act
				GinkgoWriter.Println("Starting collector with valid credentials...")
				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", infra.VcsimUsername, infra.VcsimPassword)
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					GinkgoWriter.Printf("Collector status: %s %s\n", status.Status, status.Error)
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(Equal("collected"))

				// Give time for inventory to be pushed to backend
				time.Sleep(5 * time.Second)

				source, err := userSvc.GetSource(sourceID)
				Expect(err).ToNot(HaveOccurred(), "failed to get source")

				// Assert
				GinkgoWriter.Printf("Source inventory: %+v\n", source.Inventory)
				Expect(source.Inventory).ToNot(BeNil(), "expected inventory to be populated")
				Expect(source.Inventory.VcenterId).ToNot(BeEmpty(), "expected vcenter_id to be set")
			})

			// Given an agent that switches to disconnected mode before collecting
			// When the inventory is collected and manually uploaded to the backend
			// Then the source should have the inventory populated
			It("should manually upload inventory from disconnected agent to backend", func() {
				// Arrange - Start agent in connected mode first
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       sourceID.String(),
					Mode:           "connected",
					ConsoleURL:     "http://localhost:8081",
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				// Switch to disconnected mode before collecting
				GinkgoWriter.Println("Switching agent to disconnected mode...")
				status, err := agentSvc.SetAgentMode("disconnected")
				Expect(err).ToNot(HaveOccurred(), "failed to switch to disconnected mode")
				Expect(status.Mode).To(Equal("disconnected"))

				// Act - Collect inventory in disconnected mode
				GinkgoWriter.Println("Starting collector with valid credentials...")
				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", infra.VcsimUsername, infra.VcsimPassword)
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					GinkgoWriter.Printf("Collector status: %s %s\n", status.Status, status.Error)
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(Equal("collected"))

				// Get inventory from agent
				inventory, err := agentSvc.Inventory()
				Expect(err).ToNot(HaveOccurred(), "failed to get inventory")
				Expect(inventory).ToNot(BeNil(), "expected inventory to be available")
				GinkgoWriter.Printf("Collected inventory with vcenter_id: %s\n", inventory.VcenterId)

				// Manually upload inventory to backend
				GinkgoWriter.Println("Manually uploading inventory to backend...")
				err = userSvc.UpdateSource(sourceID, openapi_types.UUID(uuid.MustParse(agentID)), inventory)
				Expect(err).ToNot(HaveOccurred(), "failed to upload inventory to backend")

				// Assert - Verify inventory was uploaded
				source, err := userSvc.GetSource(sourceID)
				Expect(err).ToNot(HaveOccurred(), "failed to get source")
				GinkgoWriter.Printf("Source inventory after upload: vcenter_id=%s\n", source.Inventory.VcenterId)
				Expect(source.Inventory).ToNot(BeNil(), "expected inventory to be populated")
				Expect(source.Inventory.VcenterId).To(Equal(inventory.VcenterId), "expected vcenter_id to match")
			})

			// Given an agent in connected mode with vcsim running
			// When invalid credentials are provided to the collector
			// Then the backend should have statusInfo containing the error message
			It("should report error with statusInfo on backend for bad credentials", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       sourceID.String(),
					Mode:           "connected",
					ConsoleURL:     "http://localhost:8081",
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				// Act
				GinkgoWriter.Println("Starting collector with invalid credentials...")
				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", "baduser", "badpass")
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				// Wait for collector to reach error state
				Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return ""
					}
					GinkgoWriter.Printf("Collector status: %s %s\n", status.Status, status.Error)
					return status.Status
				}, 30*time.Second, 2*time.Second).Should(Equal("error"))

				// Give time for status to be pushed to backend
				time.Sleep(3 * time.Second)

				// Assert - check statusInfo on backend
				source, err := userSvc.GetSource(sourceID)
				Expect(err).ToNot(HaveOccurred(), "failed to get source")

				GinkgoWriter.Printf("Source agent: %+v\n", source.Agent)
				Expect(source.Agent).ToNot(BeNil(), "expected agent to be present on source")
				Expect(string(source.Agent.Status)).To(Equal("error"), "expected agent status to be error")
				Expect(source.Agent.StatusInfo).To(ContainSubstring("invalid credentials"), "expected statusInfo to contain error message")
			})

			// Given an agent in connected mode that has collected and pushed inventory
			// When the agent is restarted
			// Then the inventory should still be available on the backend
			It("should preserve inventory on backend after agent restart", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       sourceID.String(),
					Mode:           "connected",
					ConsoleURL:     "http://localhost:8081",
					UpdateInterval: "1s",
				})
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				// Act
				GinkgoWriter.Println("Starting collector with valid credentials...")
				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", infra.VcsimUsername, infra.VcsimPassword)
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					GinkgoWriter.Printf("Collector status: %s %s\n", status.Status, status.Error)
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(Equal("collected"))

				// Give time for inventory to be pushed to backend
				time.Sleep(5 * time.Second)

				source, err := userSvc.GetSource(sourceID)
				Expect(err).ToNot(HaveOccurred(), "failed to get source")

				// Assert
				GinkgoWriter.Printf("Source inventory: %+v\n", source.Inventory)
				Expect(source.Inventory).ToNot(BeNil(), "expected inventory to be populated")
				Expect(source.Inventory.VcenterId).ToNot(BeEmpty(), "expected vcenter_id to be set")

				err = infraManager.RestartAgent()
				Expect(err).To(BeNil())
				zap.S().Info("Agent restarted")

				<-time.After(5 * time.Second)

				source, err = userSvc.GetSource(sourceID)
				Expect(err).ToNot(HaveOccurred(), "failed to get source")

				// Assert
				GinkgoWriter.Printf("Source inventory: %+v\n", source.Inventory)
				Expect(source.Inventory).ToNot(BeNil(), "expected inventory to be populated")
				Expect(source.Inventory.VcenterId).ToNot(BeEmpty(), "expected vcenter_id to be set")
			})
		})
	})
})
