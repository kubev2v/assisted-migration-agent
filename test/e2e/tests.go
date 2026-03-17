package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/test"
	"github.com/kubev2v/assisted-migration-agent/test/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e/service"

	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	gm "github.com/onsi/gomega"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

var _ = ginkgo.Describe("Agent e2e tests", ginkgo.Ordered, func() {
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
		var (
			proxy    *infra.Proxy
			requests chan infra.Request
			obs      *infra.Observer
		)

		ginkgo.BeforeAll(func() {
			target, err := url.Parse(cfg.BackendAgentEndpoint)
			gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to parse backend endpoint")

			proxy, requests = infra.NewObservableProxy("agent-proxy", "backend", target, ":8080")
			time.Sleep(100 * time.Millisecond)
			ginkgo.GinkgoWriter.Println("Proxy started on :8080")
		})

		ginkgo.AfterAll(func() {
			proxy.Stop()
		})

		ginkgo.Context("mode at startup", func() {
			var agentSvc *service.AgentSvc

			ginkgo.BeforeEach(func() {
				obs = infra.NewObserver(requests)
				agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)
			})

			ginkgo.AfterEach(func() {
				obs.Close()
				if ginkgo.CurrentSpecReport().Failed() {
					ginkgo.GinkgoWriter.Println("Keeping containers running (test failed)")
					return
				}
				ginkgo.GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()
			})

			// Given an agent configured to start in disconnected mode
			// When the agent starts up
			// Then no requests should be made to the console
			ginkgo.It("should not make requests to console when starting in disconnected mode", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				// Act
				time.Sleep(5 * time.Second)
				reqs := obs.Requests()

				// Assert
				ginkgo.GinkgoWriter.Printf("Observed %d requests\n", len(reqs))
				gm.Expect(reqs).To(gm.BeEmpty(), "gm.Expected no requests in disconnected mode")
			})

			// Given an agent configured to start in connected mode
			// When the agent starts up
			// Then requests should be made to the console
			ginkgo.It("should make requests to console when starting in connected mode", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "connected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				// Act
				time.Sleep(5 * time.Second)
				reqs := obs.Requests()

				// Assert
				ginkgo.GinkgoWriter.Printf("Observed %d requests\n", len(reqs))
				gm.Expect(reqs).ToNot(gm.BeEmpty(), "gm.Expected requests in connected mode")
			})
		})

		ginkgo.Context("vddk", func() {
			var agentSvc *service.AgentSvc

			ginkgo.BeforeEach(func() {
				obs = infra.NewObserver(requests)
				agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)
			})

			ginkgo.AfterEach(func() {
				obs.Close()
				if ginkgo.CurrentSpecReport().Failed() {
					ginkgo.GinkgoWriter.Println("Keeping containers running (test failed)")
					return
				}
				ginkgo.GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()
			})

			// Given an agent running
			// When a VDDK tarball is uploaded via POST /vddk
			// Then GET /vddk returns 200 with version, bytes, and md5 matching the upload
			ginkgo.It("should upload VDDK tarball and return status with version, bytes, and md5", func() {
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				tarGz := test.BuildTarGz(
					test.TarEntry{
						Path:    "lib/lib64.so",
						Content: "vddk-library-content",
					})
				filename := "VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz"

				uploaded, err := agentSvc.UploadVddk(tarGz, filename)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to upload VDDK")
				gm.Expect(uploaded).ToNot(gm.BeNil())
				gm.Expect(uploaded.Version).To(gm.Equal("8.0.3"), "version should be parsed from filename")
				gm.Expect(*uploaded.Bytes).To(gm.Equal(int64(len(tarGz))), "bytes should match uploaded size")
				gm.Expect(uploaded.Md5).ToNot(gm.BeEmpty(), "md5 should be set")

				status, err := agentSvc.GetVddkStatus()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get VDDK status")
				gm.Expect(status).ToNot(gm.BeNil())
				gm.Expect(status.Version).To(gm.Equal("8.0.3"))
				gm.Expect(status.Md5).To(gm.Equal(uploaded.Md5))
			})
		})

		ginkgo.Context("mode switching", func() {
			var agentSvc *service.AgentSvc

			ginkgo.BeforeEach(func() {
				obs = infra.NewObserver(requests)
				agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)
			})

			ginkgo.AfterEach(func() {
				obs.Close()
				if ginkgo.CurrentSpecReport().Failed() {
					ginkgo.GinkgoWriter.Println("Keeping containers running (test failed)")
					return
				}
				ginkgo.GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()
			})

			// Given an agent running in connected mode making requests to console
			// When the agent mode is switched to disconnected
			// Then no new requests should be made to the console
			ginkgo.It("should stop making requests after switching from connected to disconnected mode", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "connected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				time.Sleep(5 * time.Second)
				initialReqs := obs.Requests()
				ginkgo.GinkgoWriter.Printf("Observed %d requests before mode switch\n", len(initialReqs))
				gm.Expect(initialReqs).ToNot(gm.BeEmpty(), "gm.Expected requests in connected mode")

				// Act
				status, err := agentSvc.SetAgentMode("disconnected")
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to switch mode")
				gm.Expect(status.Mode).To(gm.Equal("disconnected"), "gm.Expected mode to be disconnected")

				time.Sleep(5 * time.Second)
				afterSwitchReqs := obs.Requests()

				// Assert
				ginkgo.GinkgoWriter.Printf("Observed %d requests after mode switch\n", len(afterSwitchReqs))
				gm.Expect(len(afterSwitchReqs)).To(gm.Equal(len(initialReqs)), "gm.Expected no new requests after switching to disconnected")
			})

			// Given an agent running in disconnected mode making no requests
			// When the agent mode is switched to connected
			// Then requests should start being made to the console
			ginkgo.It("should start making requests after switching from disconnected to connected mode", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				initialReqs := obs.Requests()
				ginkgo.GinkgoWriter.Printf("Observed %d requests before mode switch\n", len(initialReqs))
				gm.Expect(initialReqs).To(gm.BeEmpty(), "gm.Expected no requests in disconnected mode")

				// Act
				status, err := agentSvc.SetAgentMode("connected")
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to switch mode")
				gm.Expect(status.Mode).To(gm.Equal("connected"), "gm.Expected mode to be connected")

				time.Sleep(5 * time.Second)
				afterSwitchReqs := obs.Requests()

				// Assert
				ginkgo.GinkgoWriter.Printf("Observed %d requests after mode switch\n", len(afterSwitchReqs))
				gm.Expect(afterSwitchReqs).ToNot(gm.BeEmpty(), "gm.Expected requests after switching to connected mode")
			})

			// Given an agent that was switched from connected to disconnected mode
			// When the agent container is restarted
			// Then the mode should persist as disconnected
			ginkgo.It("should persist mode after agent restart", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "connected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				status, err := agentSvc.SetAgentMode("disconnected")
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to switch mode")
				gm.Expect(status.Mode).To(gm.Equal("disconnected"), "gm.Expected mode to be disconnected")

				// Act
				err = infraManager.RestartAgent()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to restart agent")

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				status, err = agentSvc.Status()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get agent status")

				// Assert
				ginkgo.GinkgoWriter.Printf("Agent mode after restart: %s\n", status.Mode)
				gm.Expect(status.Mode).To(gm.Equal("disconnected"), "gm.Expected mode to persist as disconnected after restart")
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
					resp, err := client.Get("https://localhost:8989/sdk")
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

				obs = infra.NewObserver(requests)
				agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)
			})

			ginkgo.AfterEach(func() {
				obs.Close()
				if ginkgo.CurrentSpecReport().Failed() {
					ginkgo.GinkgoWriter.Println("Keeping containers running (test failed)")
					return
				}
				ginkgo.GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()
				obs.Close()

				ginkgo.GinkgoWriter.Println("Stopping vcsim...")
				_ = infraManager.StopVcsim()
			})

			// Given an agent in disconnected mode with vcsim running
			// When valid vCenter credentials are provided to the collector
			// Then the collector should reach "collected" status and inventory should be available
			ginkgo.It("should collect inventory successfully with valid credentials", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				// Act
				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", infra.VcsimUsername, infra.VcsimPassword)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

				gm.Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					ginkgo.GinkgoWriter.Printf("Collector status: %s %s\n", status.Status, status.Error)
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(gm.Equal("collected"))

				inventory, err := agentSvc.Inventory()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get inventory")

				// Assert
				gm.Expect(inventory).ToNot(gm.BeNil(), "gm.Expected inventory to be available")
			})

			// Given an agent in disconnected mode with vcsim running
			// When invalid credentials are provided to the collector
			// Then the collector should reach "error" status
			ginkgo.It("should report error with bad credentials", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				// Act
				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", "baduser", "badpass")
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

				// Assert
				gm.Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return ""
					}
					ginkgo.GinkgoWriter.Printf("Collector status: %s\n", status.Status)
					return status.Status
				}, 30*time.Second, 2*time.Second).Should(gm.Equal("error"))
			})

			// Given an agent in disconnected mode
			// When an unreachable vCenter URL is provided to the collector
			// Then the collector should reach "error" status
			ginkgo.It("should report error with bad vCenter URL", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				// Act
				_, err = agentSvc.StartCollector("https://localhost:9999/sdk", "user", "pass")
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

				// Assert
				gm.Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return ""
					}
					ginkgo.GinkgoWriter.Printf("Collector status: %s\n", status.Status)
					return status.Status
				}, 30*time.Second, 2*time.Second).Should(gm.Equal("error"))
			})

			// Given an agent in disconnected mode that failed collection with bad credentials
			// When valid credentials are provided on retry
			// Then the collector should recover and reach "collected" status
			ginkgo.It("should recover from bad credentials to successful collection", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", "baduser", "badpass")
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

				gm.Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return ""
					}
					return status.Status
				}, 30*time.Second, 2*time.Second).Should(gm.Equal("error"))

				// Act
				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", infra.VcsimUsername, infra.VcsimPassword)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

				gm.Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					ginkgo.GinkgoWriter.Printf("Collector status: %s\n", status.Status)
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(gm.Equal("collected"))

				inventory, err := agentSvc.Inventory()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get inventory")

				// Assert
				gm.Expect(inventory).ToNot(gm.BeNil(), "gm.Expected inventory to be available after recovery")
			})

			// Given an agent that has successfully collected inventory
			// When the agent container is restarted
			// Then the collector status should persist as "collected" and inventory should still be available
			ginkgo.It("should persist collected state and inventory after agent restart", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       uuid.NewString(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.AgentProxyUrl,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", infra.VcsimUsername, infra.VcsimPassword)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

				gm.Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(gm.Equal("collected"))

				inventory, err := agentSvc.Inventory()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get inventory")
				gm.Expect(inventory).ToNot(gm.BeNil(), "gm.Expected inventory to be available before restart")

				// Act
				err = infraManager.RestartAgent()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to restart agent")

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				collectorStatus, err := agentSvc.GetCollectorStatus()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get collector status")

				inventory, err = agentSvc.Inventory()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get inventory after restart")

				// Assert
				ginkgo.GinkgoWriter.Printf("Collector status after restart: %s\n", collectorStatus.Status)
				gm.Expect(collectorStatus.Status).To(gm.Equal("collected"), "gm.Expected collector status to persist as collected after restart")
				gm.Expect(inventory).ToNot(gm.BeNil(), "gm.Expected inventory to be available after restart")
			})
		})
	})

	ginkgo.Context("connected env", func() {
		var (
			plannerSvc *service.PlannerSvc
			proxy      *infra.Proxy
			oidcProxy  *infra.Proxy
			obs        *infra.Observer
		)

		ginkgo.BeforeAll(func() {
			// Start OIDC server before the backend so JWKS URL is available
			ginkgo.GinkgoWriter.Println("Starting OIDC server...")
			err := infraManager.StartOIDC(":9090")
			gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start OIDC server")

			// Add a proxy between backend and OIDC
			oidcUrl, _ := url.Parse("http://localhost:9090")
			oidcProxy = infra.NewProxy("oidc-proxy", "oidc", oidcUrl, ":8082")

			ginkgo.GinkgoWriter.Println("Starting backend...")
			err = infraManager.StartBackend()
			gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start backend")

			// Wait for backend to be ready
			gm.Eventually(func() error {
				resp, err := http.DefaultClient.Get(cfg.BackendAgentEndpoint + "/health")
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

			plannerSvc = service.NewPlannerServiceWithOIDC(cfg.BackendUserEndpoint, infraManager.GenerateToken)

			// Start proxy between agent and backend for logging
			target, err := url.Parse(cfg.BackendAgentEndpoint)
			gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to parse backend endpoint")

			var requests chan infra.Request
			proxy, requests = infra.NewObservableProxy("agent-proxy", "backend", target, ":8081")
			obs = infra.NewObserver(requests)

			time.Sleep(100 * time.Millisecond)
			ginkgo.GinkgoWriter.Println("Proxy started on :8081")
		})

		ginkgo.AfterAll(func() {
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

		ginkgo.Context("mode at startup", func() {
			var (
				agentSvc *service.AgentSvc
				sourceID openapi_types.UUID
				userSvc  *service.PlannerSvc
			)

			ginkgo.BeforeEach(func() {
				agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)
				userSvc = plannerSvc.WithAuthUser("admin", "admin", "admin@example.com")

				source, err := userSvc.CreateSource("test-source-" + uuid.NewString()[:8])
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to create source")
				sourceID = source.Id
				ginkgo.GinkgoWriter.Printf("Created source: %s\n", sourceID)
			})

			ginkgo.AfterEach(func() {
				if ginkgo.CurrentSpecReport().Failed() {
					ginkgo.GinkgoWriter.Println("Keeping containers running (test failed)")
					return
				}
				ginkgo.GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()

				ginkgo.GinkgoWriter.Println("Deleting source...")
				_ = userSvc.RemoveSource(sourceID)
			})

			// Given an agent configured in connected mode with a valid source ID
			// When the agent starts and registers with the backend
			// Then the source should have the agent attached with status "waiting-for-credentials"
			ginkgo.It("should register agent with backend when starting in connected mode", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       sourceID.String(),
					Mode:           "connected",
					ConsoleURL:     cfg.BackendAgentEndpoint,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				// Act
				time.Sleep(10 * time.Second) // allow agent to register with backend
				source, err := userSvc.GetSource(sourceID)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get source")

				// Assert
				ginkgo.GinkgoWriter.Printf("Source agent: %+v\n", source.Agent)
				gm.Expect(source.Agent).ToNot(gm.BeNil(), "gm.Expected agent to be attached to source")
				gm.Expect(string(source.Agent.Status)).To(gm.Equal("waiting-for-credentials"), "gm.Expected agent status to be waiting-for-credentials")
			})
		})

		ginkgo.Context("mode switch", func() {
			var (
				agentSvc *service.AgentSvc
				sourceID openapi_types.UUID
				userSvc  *service.PlannerSvc
			)

			ginkgo.BeforeEach(func() {
				agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)
				userSvc = plannerSvc.WithAuthUser("admin", "admin", "admin@example.com")

				source, err := userSvc.CreateSource("test-source-" + uuid.NewString()[:8])
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to create source")
				sourceID = source.Id
				ginkgo.GinkgoWriter.Printf("Created source: %s\n", sourceID)
			})

			ginkgo.AfterEach(func() {
				if ginkgo.CurrentSpecReport().Failed() {
					ginkgo.GinkgoWriter.Println("Keeping containers running (test failed)")
					return
				}
				ginkgo.GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()

				ginkgo.GinkgoWriter.Println("Deleting source...")
				_ = userSvc.RemoveSource(sourceID)
			})

			// Given an agent started in disconnected mode with a valid source ID
			// When the agent mode is switched to connected
			// Then the source should have the agent attached
			ginkgo.It("should register agent with backend after switching from disconnected to connected", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       sourceID.String(),
					Mode:           "disconnected",
					ConsoleURL:     cfg.BackendAgentEndpoint,
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				// Act
				_, err = agentSvc.SetAgentMode("connected")
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to switch mode")

				time.Sleep(5 * time.Second) // allow agent to register with backend
				source, err := userSvc.GetSource(sourceID)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get source")

				// Assert
				ginkgo.GinkgoWriter.Printf("Source agent: %+v\n", source.Agent)
				gm.Expect(source.Agent).ToNot(gm.BeNil(), "gm.Expected agent to be attached to source after mode switch")
			})

			// Given an agent started in disconnected mode without inventory
			// When the agent mode is switched to connected
			// Then the agent should communicate with backend without errors
			ginkgo.It("should communicate with backend without errors when no inventory is collected", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       sourceID.String(),
					Mode:           "disconnected",
					ConsoleURL:     "http://localhost:8081", // Use proxy to observe requests
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				// Act - switch to connected mode without collecting inventory
				_, err = agentSvc.SetAgentMode("connected")
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to switch mode")

				// Wait for agent to make requests to backend
				time.Sleep(5 * time.Second)

				// Assert - check agent status has no error
				status, err := agentSvc.Status()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get agent status")
				ginkgo.GinkgoWriter.Printf("Agent status: mode=%s, console_connection=%s, error=%s\n",
					status.Mode, status.ConsoleConnection, status.Error)

				gm.Expect(status.Mode).To(gm.Equal("connected"), "gm.Expected mode to be connected")
				gm.Expect(status.Error).To(gm.BeEmpty(), "gm.Expected no error in agent status")

				// Assert - verify requests were made to backend via observer
				reqs := obs.Requests()
				ginkgo.GinkgoWriter.Printf("Observed %d requests to backend\n", len(reqs))
				gm.Expect(reqs).ToNot(gm.BeEmpty(), "gm.Expected requests to be made to backend")
			})
		})

		ginkgo.Context("collector", func() {
			var (
				agentSvc *service.AgentSvc
				sourceID openapi_types.UUID
				userSvc  *service.PlannerSvc
			)

			ginkgo.BeforeEach(func() {
				ginkgo.GinkgoWriter.Println("Starting vcsim...")
				err := infraManager.StartVcsim()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start vcsim")
				time.Sleep(1 * time.Second) // allow vcsim to initialize

				// Wait for vcsim to be ready
				client := &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
					},
				}
				gm.Eventually(func() error {
					resp, err := client.Get("https://localhost:8989/sdk")
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
				userSvc = plannerSvc.WithAuthUser("admin", "admin", "admin@example.com")

				source, err := userSvc.CreateSource("test-source-" + uuid.NewString()[:8])
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to create source")
				sourceID = source.Id
				ginkgo.GinkgoWriter.Printf("Created source: %s\n", sourceID)
			})

			ginkgo.AfterEach(func() {
				if ginkgo.CurrentSpecReport().Failed() {
					ginkgo.GinkgoWriter.Println("Keeping containers running (test failed)")
					return
				}
				ginkgo.GinkgoWriter.Println("Stopping agent...")
				_ = infraManager.RemoveAgent()

				ginkgo.GinkgoWriter.Println("Deleting source...")
				_ = userSvc.RemoveSource(sourceID)

				ginkgo.GinkgoWriter.Println("Stopping vcsim...")
				_ = infraManager.StopVcsim()
			})

			// Given an agent in connected mode with valid vCenter credentials
			// When the collector successfully gathers inventory
			// Then the source should have the inventory populated
			ginkgo.It("should push inventory to backend after successful collection", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       sourceID.String(),
					Mode:           "connected",
					ConsoleURL:     "http://localhost:8081",
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				// Act
				ginkgo.GinkgoWriter.Println("Starting collector with valid credentials...")
				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", infra.VcsimUsername, infra.VcsimPassword)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

				gm.Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					ginkgo.GinkgoWriter.Printf("Collector status: %s %s\n", status.Status, status.Error)
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(gm.Equal("collected"))

				// Give time for inventory to be pushed to backend
				time.Sleep(5 * time.Second)

				source, err := userSvc.GetSource(sourceID)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get source")

				// Assert
				ginkgo.GinkgoWriter.Printf("Source inventory: %+v\n", source.Inventory)
				gm.Expect(source.Inventory).ToNot(gm.BeNil(), "gm.Expected inventory to be populated")
				gm.Expect(source.Inventory.VcenterId).ToNot(gm.BeEmpty(), "gm.Expected vcenter_id to be set")
			})

			// Given an agent that switches to disconnected mode before collecting
			// When the inventory is collected and manually uploaded to the backend
			// Then the source should have the inventory populated
			ginkgo.It("should manually upload inventory from disconnected agent to backend", func() {
				// Arrange - Start agent in connected mode first
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       sourceID.String(),
					Mode:           "connected",
					ConsoleURL:     "http://localhost:8081",
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				// Switch to disconnected mode before collecting
				ginkgo.GinkgoWriter.Println("Switching agent to disconnected mode...")
				status, err := agentSvc.SetAgentMode("disconnected")
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to switch to disconnected mode")
				gm.Expect(status.Mode).To(gm.Equal("disconnected"))

				// Act - Collect inventory in disconnected mode
				ginkgo.GinkgoWriter.Println("Starting collector with valid credentials...")
				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", infra.VcsimUsername, infra.VcsimPassword)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

				gm.Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					ginkgo.GinkgoWriter.Printf("Collector status: %s %s\n", status.Status, status.Error)
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(gm.Equal("collected"))

				// Get inventory from agent
				inventory, err := agentSvc.Inventory()
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get inventory")
				gm.Expect(inventory).ToNot(gm.BeNil(), "gm.Expected inventory to be available")
				ginkgo.GinkgoWriter.Printf("Collected inventory with vcenter_id: %s\n", inventory.Inventory.VcenterId)

				// Manually upload inventory to backend
				ginkgo.GinkgoWriter.Println("Manually uploading inventory to backend...")
				err = userSvc.UpdateSource(sourceID, inventory)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to upload inventory to backend")

				// Assert - Verify inventory was uploaded
				source, err := userSvc.GetSource(sourceID)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get source")
				ginkgo.GinkgoWriter.Printf("Source inventory after upload: vcenter_id=%s\n", source.Inventory.VcenterId)
				gm.Expect(source.Inventory).ToNot(gm.BeNil(), "gm.Expected inventory to be populated")
				gm.Expect(source.Inventory.VcenterId).To(gm.Equal(inventory.Inventory.VcenterId), "Expected vcenter_id to match")
			})

			// Given an agent in connected mode with vcsim running
			// When invalid credentials are provided to the collector
			// Then the backend should have statusInfo containing the error message
			ginkgo.It("should report error with statusInfo on backend for bad credentials", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       sourceID.String(),
					Mode:           "connected",
					ConsoleURL:     "http://localhost:8081",
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				// Act
				ginkgo.GinkgoWriter.Println("Starting collector with invalid credentials...")
				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", "baduser", "badpass")
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

				// Wait for collector to reach error state
				gm.Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return ""
					}
					ginkgo.GinkgoWriter.Printf("Collector status: %s %s\n", status.Status, status.Error)
					return status.Status
				}, 30*time.Second, 2*time.Second).Should(gm.Equal("error"))

				// Give time for status to be pushed to backend
				time.Sleep(3 * time.Second)

				// Assert - check statusInfo on backend
				source, err := userSvc.GetSource(sourceID)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get source")

				ginkgo.GinkgoWriter.Printf("Source agent: %+v\n", source.Agent)
				gm.Expect(source.Agent).ToNot(gm.BeNil(), "gm.Expected agent to be present on source")
				gm.Expect(string(source.Agent.Status)).To(gm.Equal("error"), "gm.Expected agent status to be error")
				gm.Expect(source.Agent.StatusInfo).To(gm.ContainSubstring("invalid credentials"), "gm.Expected statusInfo to contain error message")
			})

			// Given an agent in connected mode that has collected and pushed inventory
			// When the agent is restarted
			// Then the inventory should still be available on the backend
			ginkgo.It("should preserve inventory on backend after agent restart", func() {
				// Arrange
				agentID := uuid.NewString()
				_, err := infraManager.StartAgent(infra.AgentConfig{
					AgentID:        agentID,
					SourceID:       sourceID.String(),
					Mode:           "connected",
					ConsoleURL:     "http://localhost:8081",
					UpdateInterval: "1s",
				})
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
				ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

				gm.Eventually(func() error {
					_, err := agentSvc.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

				// Act
				ginkgo.GinkgoWriter.Println("Starting collector with valid credentials...")
				_, err = agentSvc.StartCollector("https://localhost:8989/sdk", infra.VcsimUsername, infra.VcsimPassword)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

				gm.Eventually(func() string {
					status, err := agentSvc.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					ginkgo.GinkgoWriter.Printf("Collector status: %s %s\n", status.Status, status.Error)
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(gm.Equal("collected"))

				// Give time for inventory to be pushed to backend
				time.Sleep(5 * time.Second)

				source, err := userSvc.GetSource(sourceID)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get source")

				// Assert
				ginkgo.GinkgoWriter.Printf("Source inventory: %+v\n", source.Inventory)
				gm.Expect(source.Inventory).ToNot(gm.BeNil(), "gm.Expected inventory to be populated")
				gm.Expect(source.Inventory.VcenterId).ToNot(gm.BeEmpty(), "gm.Expected vcenter_id to be set")

				err = infraManager.RestartAgent()
				gm.Expect(err).To(gm.BeNil())
				zap.S().Info("Agent restarted")

				<-time.After(5 * time.Second)

				source, err = userSvc.GetSource(sourceID)
				gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to get source")

				// Assert
				ginkgo.GinkgoWriter.Printf("Source inventory: %+v\n", source.Inventory)
				gm.Expect(source.Inventory).ToNot(gm.BeNil(), "gm.Expected inventory to be populated")
				gm.Expect(source.Inventory.VcenterId).ToNot(gm.BeEmpty(), "gm.Expected vcenter_id to be set")
			})
		})
	})
})
