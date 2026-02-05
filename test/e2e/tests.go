package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/kubev2v/assisted-migration-agent/test/e2e/agent"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Agent e2e tests", Ordered, func() {
	var stack *Stack

	BeforeAll(func() {
		var err error
		stack, err = NewStack(cfg)
		Expect(err).ToNot(HaveOccurred(), "failed to create stack")

		GinkgoWriter.Println("Starting postgres...")
		err = stack.StartPostgres()
		Expect(err).ToNot(HaveOccurred(), "failed to start postgres")
		time.Sleep(2 * time.Second) // wait for postgres to be ready
	})

	AfterAll(func() {
		_ = stack.StopPostgres()
	})

	Context("disconnected env", func() {
		var (
			proxy       *Proxy
			requests    chan Request
			proxyServer *http.Server
			obs         *Observer
		)

		BeforeAll(func() {
			target, err := url.Parse(cfg.BackendAgentEndpoint)
			Expect(err).ToNot(HaveOccurred(), "failed to parse backend endpoint")

			proxy, requests = NewProxy(target)
			proxyServer = &http.Server{
				Addr:    ":8080",
				Handler: proxy.Handler(),
			}
			go func() {
				if err := proxyServer.ListenAndServe(); err != nil {
					GinkgoWriter.Printf("failed to start proxy: %v", err)
				}
			}()
			time.Sleep(100 * time.Millisecond)
			GinkgoWriter.Println("Proxy started on :8080")
		})

		AfterAll(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = proxyServer.Shutdown(ctx)
			proxy.Close()
		})

		Context("mode at startup", func() {
			var agentActioner *agent.AgentApi

			BeforeEach(func() {
				obs = NewObserver(requests)
				agentActioner = agent.DefaultAgentApi(cfg.AgentAPIUrl)
			})

			AfterEach(func() {
				if cfg.KeepContainers {
					GinkgoWriter.Println("Keeping agent container running (--keep-containers flag set)")
					obs.Close()
					return
				}
				GinkgoWriter.Println("Stopping agent...")
				_ = stack.Runner.StopContainer(AgentContainerName)
				_ = stack.Runner.RemoveContainer(AgentContainerName)
				_ = stack.Runner.RemoveVolume(AgentVolumeName)
				obs.Close()
			})

			// Given an agent configured to start in disconnected mode
			// When the agent starts up
			// Then no requests should be made to the console
			It("should not make requests to console when starting in disconnected mode", func() {
				// Arrange
				agentID := uuid.NewString()
				containerID, err := stack.Runner.StartContainer(
					NewContainerConfig(AgentContainerName, cfg.AgentImage).
						WithPort(8000, 8000).
						WithVolume(AgentVolumeName, "/var/lib/agent").
						WithEnvVar("AGENT_MODE", "disconnected").
						WithEnvVar("AGENT_AGENT_ID", agentID).
						WithEnvVar("AGENT_SOURCE_ID", uuid.NewString()).
						WithEnvVar("AGENT_DATA_FOLDER", "/var/lib/agent").
						WithEnvVar("AGENT_CONSOLE_URL", cfg.AgentProxyUrl).
						WithEnvVar("AGENT_CONSOLE_UPDATE_INTERVAL", "1s"),
				)
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s container ID: %s\n", agentID, containerID)

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

				Expect(err).ToNot(HaveOccurred(), "agent not ready")

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
				containerID, err := stack.Runner.StartContainer(
					NewContainerConfig(AgentContainerName, cfg.AgentImage).
						WithPort(8000, 8000).
						WithVolume(AgentVolumeName, "/var/lib/agent").
						WithEnvVar("AGENT_MODE", "connected").
						WithEnvVar("AGENT_AGENT_ID", agentID).
						WithEnvVar("AGENT_SOURCE_ID", uuid.NewString()).
						WithEnvVar("AGENT_DATA_FOLDER", "/var/lib/agent").
						WithEnvVar("AGENT_CONSOLE_URL", cfg.AgentProxyUrl).
						WithEnvVar("AGENT_CONSOLE_UPDATE_INTERVAL", "1s"),
				)
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s container ID: %s\n", agentID, containerID)

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())
				Expect(err).ToNot(HaveOccurred(), "agent not ready")

				// Act
				time.Sleep(5 * time.Second)
				reqs := obs.Requests()

				// Assert
				GinkgoWriter.Printf("Observed %d requests\n", len(reqs))
				Expect(reqs).ToNot(BeEmpty(), "expected requests in connected mode")
			})
		})

		Context("mode switching", func() {
			var agentActioner *agent.AgentApi

			BeforeEach(func() {
				obs = NewObserver(requests)
				agentActioner = agent.DefaultAgentApi(cfg.AgentAPIUrl)
			})

			AfterEach(func() {
				if cfg.KeepContainers {
					GinkgoWriter.Println("Keeping agent container running (--keep-containers flag set)")
					obs.Close()
					return
				}
				GinkgoWriter.Println("Stopping agent...")
				_ = stack.Runner.StopContainer(AgentContainerName)
				_ = stack.Runner.RemoveContainer(AgentContainerName)
				_ = stack.Runner.RemoveVolume(AgentVolumeName)
				obs.Close()
			})

			// Given an agent running in connected mode making requests to console
			// When the agent mode is switched to disconnected
			// Then no new requests should be made to the console
			It("should stop making requests after switching from connected to disconnected mode", func() {
				// Arrange
				agentID := uuid.NewString()
				containerID, err := stack.Runner.StartContainer(
					NewContainerConfig(AgentContainerName, cfg.AgentImage).
						WithPort(8000, 8000).
						WithVolume(AgentVolumeName, "/var/lib/agent").
						WithEnvVar("AGENT_MODE", "connected").
						WithEnvVar("AGENT_AGENT_ID", agentID).
						WithEnvVar("AGENT_SOURCE_ID", uuid.NewString()).
						WithEnvVar("AGENT_DATA_FOLDER", "/var/lib/agent").
						WithEnvVar("AGENT_CONSOLE_URL", cfg.AgentProxyUrl).
						WithEnvVar("AGENT_CONSOLE_UPDATE_INTERVAL", "1s"),
				)
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s container ID: %s\n", agentID, containerID)

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())
				Expect(err).ToNot(HaveOccurred(), "agent not ready")

				time.Sleep(5 * time.Second)
				initialReqs := obs.Requests()
				GinkgoWriter.Printf("Observed %d requests before mode switch\n", len(initialReqs))
				Expect(initialReqs).ToNot(BeEmpty(), "expected requests in connected mode")

				// Act
				status, err := agentActioner.SetAgentMode("disconnected")
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
				containerID, err := stack.Runner.StartContainer(
					NewContainerConfig(AgentContainerName, cfg.AgentImage).
						WithPort(8000, 8000).
						WithVolume(AgentVolumeName, "/var/lib/agent").
						WithEnvVar("AGENT_MODE", "disconnected").
						WithEnvVar("AGENT_AGENT_ID", agentID).
						WithEnvVar("AGENT_SOURCE_ID", uuid.NewString()).
						WithEnvVar("AGENT_DATA_FOLDER", "/var/lib/agent").
						WithEnvVar("AGENT_CONSOLE_URL", cfg.AgentProxyUrl).
						WithEnvVar("AGENT_CONSOLE_UPDATE_INTERVAL", "1s"),
				)
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s container ID: %s\n", agentID, containerID)

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())
				Expect(err).ToNot(HaveOccurred(), "agent not ready")

				initialReqs := obs.Requests()
				GinkgoWriter.Printf("Observed %d requests before mode switch\n", len(initialReqs))
				Expect(initialReqs).To(BeEmpty(), "expected no requests in disconnected mode")

				// Act
				status, err := agentActioner.SetAgentMode("connected")
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
				containerID, err := stack.Runner.StartContainer(
					NewContainerConfig(AgentContainerName, cfg.AgentImage).
						WithPort(8000, 8000).
						WithVolume(AgentVolumeName, "/var/lib/agent").
						WithEnvVar("AGENT_MODE", "connected").
						WithEnvVar("AGENT_AGENT_ID", agentID).
						WithEnvVar("AGENT_SOURCE_ID", uuid.NewString()).
						WithEnvVar("AGENT_DATA_FOLDER", "/var/lib/agent").
						WithEnvVar("AGENT_CONSOLE_URL", cfg.AgentProxyUrl).
						WithEnvVar("AGENT_CONSOLE_UPDATE_INTERVAL", "1s"),
				)
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s container ID: %s\n", agentID, containerID)

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())
				Expect(err).ToNot(HaveOccurred(), "agent not ready")

				status, err := agentActioner.SetAgentMode("disconnected")
				Expect(err).ToNot(HaveOccurred(), "failed to switch mode")
				Expect(status.Mode).To(Equal("disconnected"), "expected mode to be disconnected")

				// Act
				err = stack.Runner.RestartContainer(AgentContainerName)
				Expect(err).ToNot(HaveOccurred(), "failed to restart agent")

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())
				Expect(err).ToNot(HaveOccurred(), "agent not ready after restart")

				status, err = agentActioner.Status()
				Expect(err).ToNot(HaveOccurred(), "failed to get agent status")

				// Assert
				GinkgoWriter.Printf("Agent mode after restart: %s\n", status.Mode)
				Expect(status.Mode).To(Equal("disconnected"), "expected mode to persist as disconnected after restart")
			})
		})

		Context("collector", func() {
			var agentActioner *agent.AgentApi

			BeforeAll(func() {
				GinkgoWriter.Println("Starting vcsim...")
				err := stack.StartVcsim()
				Expect(err).ToNot(HaveOccurred(), "failed to start vcsim")

				client := &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
					},
				}

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())

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

				Expect(err).ToNot(HaveOccurred(), "vcsim not ready")
			})

			AfterAll(func() {
				if cfg.KeepContainers {
					GinkgoWriter.Println("Keeping vcsim container running (--keep-containers flag set)")
					return
				}
				GinkgoWriter.Println("Stopping vcsim...")
				_ = stack.StopVcsim()
			})

			BeforeEach(func() {
				obs = NewObserver(requests)
				agentActioner = agent.DefaultAgentApi(cfg.AgentAPIUrl)
			})

			AfterEach(func() {
				if cfg.KeepContainers {
					GinkgoWriter.Println("Keeping agent container running (--keep-containers flag set)")
					obs.Close()
					return
				}
				GinkgoWriter.Println("Stopping agent...")
				_ = stack.Runner.StopContainer(AgentContainerName)
				_ = stack.Runner.RemoveContainer(AgentContainerName)
				_ = stack.Runner.RemoveVolume(AgentVolumeName)
				obs.Close()
			})

			// Given an agent in disconnected mode with vcsim running
			// When valid vCenter credentials are provided to the collector
			// Then the collector should reach "collected" status and inventory should be available
			It("should collect inventory successfully with valid credentials", func() {
				// Arrange
				agentID := uuid.NewString()
				containerID, err := stack.Runner.StartContainer(
					NewContainerConfig(AgentContainerName, cfg.AgentImage).
						WithPort(8000, 8000).
						WithVolume(AgentVolumeName, "/var/lib/agent").
						WithEnvVar("AGENT_MODE", "disconnected").
						WithEnvVar("AGENT_AGENT_ID", agentID).
						WithEnvVar("AGENT_SOURCE_ID", uuid.NewString()).
						WithEnvVar("AGENT_DATA_FOLDER", "/var/lib/agent").
						WithEnvVar("AGENT_CONSOLE_URL", cfg.AgentProxyUrl).
						WithEnvVar("AGENT_CONSOLE_UPDATE_INTERVAL", "1s"),
				)
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s container ID: %s\n", agentID, containerID)

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())
				Expect(err).ToNot(HaveOccurred(), "agent not ready")

				// Act
				_, err = agentActioner.StartCollector("https://localhost:8989/sdk", VcsimUsername, VcsimPassword)
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				Eventually(func() string {
					status, err := agentActioner.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					GinkgoWriter.Printf("Collector status: %s %s\n", status.Status, status.Error)
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(Equal("collected"))

				inventory, err := agentActioner.Inventory()
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
				containerID, err := stack.Runner.StartContainer(
					NewContainerConfig(AgentContainerName, cfg.AgentImage).
						WithPort(8000, 8000).
						WithVolume(AgentVolumeName, "/var/lib/agent").
						WithEnvVar("AGENT_MODE", "disconnected").
						WithEnvVar("AGENT_AGENT_ID", agentID).
						WithEnvVar("AGENT_SOURCE_ID", uuid.NewString()).
						WithEnvVar("AGENT_DATA_FOLDER", "/var/lib/agent").
						WithEnvVar("AGENT_CONSOLE_URL", cfg.AgentProxyUrl).
						WithEnvVar("AGENT_CONSOLE_UPDATE_INTERVAL", "1s"),
				)
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s container ID: %s\n", agentID, containerID)

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())
				Expect(err).ToNot(HaveOccurred(), "agent not ready")

				// Act
				_, err = agentActioner.StartCollector("https://localhost:8989/sdk", "baduser", "badpass")
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				// Assert
				Eventually(func() string {
					status, err := agentActioner.GetCollectorStatus()
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
				containerID, err := stack.Runner.StartContainer(
					NewContainerConfig(AgentContainerName, cfg.AgentImage).
						WithPort(8000, 8000).
						WithVolume(AgentVolumeName, "/var/lib/agent").
						WithEnvVar("AGENT_MODE", "disconnected").
						WithEnvVar("AGENT_AGENT_ID", agentID).
						WithEnvVar("AGENT_SOURCE_ID", uuid.NewString()).
						WithEnvVar("AGENT_DATA_FOLDER", "/var/lib/agent").
						WithEnvVar("AGENT_CONSOLE_URL", cfg.AgentProxyUrl).
						WithEnvVar("AGENT_CONSOLE_UPDATE_INTERVAL", "1s"),
				)
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s container ID: %s\n", agentID, containerID)

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())
				Expect(err).ToNot(HaveOccurred(), "agent not ready")

				// Act
				_, err = agentActioner.StartCollector("https://localhost:9999/sdk", "user", "pass")
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				// Assert
				Eventually(func() string {
					status, err := agentActioner.GetCollectorStatus()
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
				containerID, err := stack.Runner.StartContainer(
					NewContainerConfig(AgentContainerName, cfg.AgentImage).
						WithPort(8000, 8000).
						WithVolume(AgentVolumeName, "/var/lib/agent").
						WithEnvVar("AGENT_MODE", "disconnected").
						WithEnvVar("AGENT_AGENT_ID", agentID).
						WithEnvVar("AGENT_SOURCE_ID", uuid.NewString()).
						WithEnvVar("AGENT_DATA_FOLDER", "/var/lib/agent").
						WithEnvVar("AGENT_CONSOLE_URL", cfg.AgentProxyUrl).
						WithEnvVar("AGENT_CONSOLE_UPDATE_INTERVAL", "1s"),
				)
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s container ID: %s\n", agentID, containerID)

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())
				Expect(err).ToNot(HaveOccurred(), "agent not ready")

				_, err = agentActioner.StartCollector("https://localhost:8989/sdk", "baduser", "badpass")
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				Eventually(func() string {
					status, err := agentActioner.GetCollectorStatus()
					if err != nil {
						return ""
					}
					return status.Status
				}, 30*time.Second, 2*time.Second).Should(Equal("error"))

				// Act
				_, err = agentActioner.StartCollector("https://localhost:8989/sdk", VcsimUsername, VcsimPassword)
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				Eventually(func() string {
					status, err := agentActioner.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					GinkgoWriter.Printf("Collector status: %s\n", status.Status)
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(Equal("collected"))

				inventory, err := agentActioner.Inventory()
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
				containerID, err := stack.Runner.StartContainer(
					NewContainerConfig(AgentContainerName, cfg.AgentImage).
						WithPort(8000, 8000).
						WithVolume(AgentVolumeName, "/var/lib/agent").
						WithEnvVar("AGENT_MODE", "disconnected").
						WithEnvVar("AGENT_AGENT_ID", agentID).
						WithEnvVar("AGENT_SOURCE_ID", uuid.NewString()).
						WithEnvVar("AGENT_DATA_FOLDER", "/var/lib/agent").
						WithEnvVar("AGENT_CONSOLE_URL", cfg.AgentProxyUrl).
						WithEnvVar("AGENT_CONSOLE_UPDATE_INTERVAL", "1s"),
				)
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s container ID: %s\n", agentID, containerID)

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())
				Expect(err).ToNot(HaveOccurred(), "agent not ready")

				_, err = agentActioner.StartCollector("https://localhost:8989/sdk", VcsimUsername, VcsimPassword)
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				Eventually(func() string {
					status, err := agentActioner.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(Equal("collected"))

				inventory, err := agentActioner.Inventory()
				Expect(err).ToNot(HaveOccurred(), "failed to get inventory")
				Expect(inventory).ToNot(BeNil(), "expected inventory to be available before restart")

				// Act
				err = stack.Runner.RestartContainer(AgentContainerName)
				Expect(err).ToNot(HaveOccurred(), "failed to restart agent")

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())
				Expect(err).ToNot(HaveOccurred(), "agent not ready after restart")

				collectorStatus, err := agentActioner.GetCollectorStatus()
				Expect(err).ToNot(HaveOccurred(), "failed to get collector status")

				inventory, err = agentActioner.Inventory()
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
			backendActioner *BackendActioner
			proxy           *Proxy
			proxyServer     *http.Server
			obs             *Observer
		)

		BeforeAll(func() {
			GinkgoWriter.Println("Starting backend...")
			err := stack.StartBackend()
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

			Expect(err).ToNot(HaveOccurred(), "backend not ready")

			backendActioner = NewBackendActioner(cfg.BackendUserEndpoint)

			// Start proxy between agent and backend for logging
			target, err := url.Parse(cfg.BackendAgentEndpoint)
			Expect(err).ToNot(HaveOccurred(), "failed to parse backend endpoint")

			var requests chan Request
			proxy, requests = NewProxy(target)
			obs = NewObserver(requests)
			proxyServer = &http.Server{
				Addr:    ":8081",
				Handler: proxy.Handler(),
			}
			go func() {
				if err := proxyServer.ListenAndServe(); err != nil {
					GinkgoWriter.Printf("failed to start proxy: %v", err)
				}
			}()

			time.Sleep(100 * time.Millisecond)
			GinkgoWriter.Println("Proxy started on :8081")
		})

		AfterAll(func() {
			// Shutdown proxy and observer
			if proxyServer != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = proxyServer.Shutdown(ctx)
			}
			if proxy != nil {
				proxy.Close()
			}
			if obs != nil {
				obs.Close()
			}
			_ = stack.StopBackend()
		})

		Context("mode at startup", func() {
			var (
				agentActioner *agent.AgentApi
				sourceID      string
			)

			BeforeEach(func() {
				agentActioner = agent.DefaultAgentApi(cfg.AgentAPIUrl)

				var err error
				sourceID, err = backendActioner.CreateSource("test-source-" + uuid.NewString()[:8])
				Expect(err).ToNot(HaveOccurred(), "failed to create source")
				GinkgoWriter.Printf("Created source: %s\n", sourceID)
			})

			AfterEach(func() {
				if cfg.KeepContainers {
					GinkgoWriter.Println("Keeping agent container running (--keep-containers flag set)")
					return
				}
				GinkgoWriter.Println("Stopping agent...")
				_ = stack.Runner.StopContainer(AgentContainerName)
				_ = stack.Runner.RemoveContainer(AgentContainerName)
				_ = stack.Runner.RemoveVolume(AgentVolumeName)

				GinkgoWriter.Println("Deleting source...")
				_ = backendActioner.DeleteSource(sourceID)
			})

			// Given an agent configured in connected mode with a valid source ID
			// When the agent starts and registers with the backend
			// Then the source should have the agent attached with status "waiting-for-credentials"
			It("should register agent with backend when starting in connected mode", func() {
				// Arrange
				agentID := uuid.NewString()
				containerID, err := stack.Runner.StartContainer(
					NewContainerConfig(AgentContainerName, cfg.AgentImage).
						WithPort(8000, 8000).
						WithVolume(AgentVolumeName, "/var/lib/agent").
						WithEnvVar("AGENT_MODE", "connected").
						WithEnvVar("AGENT_AGENT_ID", agentID).
						WithEnvVar("AGENT_SOURCE_ID", sourceID).
						WithEnvVar("AGENT_DATA_FOLDER", "/var/lib/agent").
						WithEnvVar("AGENT_CONSOLE_URL", cfg.BackendAgentEndpoint).
						WithEnvVar("AGENT_CONSOLE_UPDATE_INTERVAL", "1s"),
				)
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s container ID: %s\n", agentID, containerID)

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())
				Expect(err).ToNot(HaveOccurred(), "agent not ready")

				// Act
				time.Sleep(10 * time.Second) // allow agent to register with backend
				source, err := backendActioner.GetSource(sourceID)
				Expect(err).ToNot(HaveOccurred(), "failed to get source")

				// Assert
				GinkgoWriter.Printf("Source agent: %+v\n", source.Agent)
				Expect(source.Agent).ToNot(BeNil(), "expected agent to be attached to source")
				Expect(source.Agent.Status).To(Equal("waiting-for-credentials"), "expected agent status to be waiting-for-credentials")
			})
		})

		Context("mode switch", func() {
			var (
				agentActioner *agent.AgentApi
				sourceID      string
			)

			BeforeEach(func() {
				agentActioner = agent.DefaultAgentApi(cfg.AgentAPIUrl)

				var err error
				sourceID, err = backendActioner.CreateSource("test-source-" + uuid.NewString()[:8])
				Expect(err).ToNot(HaveOccurred(), "failed to create source")
				GinkgoWriter.Printf("Created source: %s\n", sourceID)
			})

			AfterEach(func() {
				if cfg.KeepContainers {
					GinkgoWriter.Println("Keeping agent container running (--keep-containers flag set)")
					return
				}
				GinkgoWriter.Println("Stopping agent...")
				_ = stack.Runner.StopContainer(AgentContainerName)
				_ = stack.Runner.RemoveContainer(AgentContainerName)
				_ = stack.Runner.RemoveVolume(AgentVolumeName)

				GinkgoWriter.Println("Deleting source...")
				_ = backendActioner.DeleteSource(sourceID)
			})

			// Given an agent started in disconnected mode with a valid source ID
			// When the agent mode is switched to connected
			// Then the source should have the agent attached
			It("should register agent with backend after switching from disconnected to connected", func() {
				// Arrange
				agentID := uuid.NewString()
				containerID, err := stack.Runner.StartContainer(
					NewContainerConfig(AgentContainerName, cfg.AgentImage).
						WithPort(8000, 8000).
						WithVolume(AgentVolumeName, "/var/lib/agent").
						WithEnvVar("AGENT_MODE", "disconnected").
						WithEnvVar("AGENT_AGENT_ID", agentID).
						WithEnvVar("AGENT_SOURCE_ID", sourceID).
						WithEnvVar("AGENT_DATA_FOLDER", "/var/lib/agent").
						WithEnvVar("AGENT_CONSOLE_URL", cfg.BackendAgentEndpoint).
						WithEnvVar("AGENT_CONSOLE_UPDATE_INTERVAL", "1s"),
				)
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s container ID: %s\n", agentID, containerID)

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())
				Expect(err).ToNot(HaveOccurred(), "agent not ready")

				// Act
				_, err = agentActioner.SetAgentMode("connected")
				Expect(err).ToNot(HaveOccurred(), "failed to switch mode")

				time.Sleep(5 * time.Second) // allow agent to register with backend
				source, err := backendActioner.GetSource(sourceID)
				Expect(err).ToNot(HaveOccurred(), "failed to get source")

				// Assert
				GinkgoWriter.Printf("Source agent: %+v\n", source.Agent)
				Expect(source.Agent).ToNot(BeNil(), "expected agent to be attached to source after mode switch")
			})
		})

		Context("collector", func() {
			var (
				agentActioner *agent.AgentApi
				sourceID      string
			)

			BeforeAll(func() {
				GinkgoWriter.Println("Starting vcsim...")
				err := stack.StartVcsim()
				Expect(err).ToNot(HaveOccurred(), "failed to start vcsim")

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
				Expect(err).ToNot(HaveOccurred(), "vcsim not ready")
			})

			AfterAll(func() {
				if cfg.KeepContainers {
					GinkgoWriter.Println("Keeping vcsim container running (--keep-containers flag set)")
					return
				}
				GinkgoWriter.Println("Stopping vcsim...")
				_ = stack.StopVcsim()
			})

			BeforeEach(func() {
				agentActioner = agent.DefaultAgentApi(cfg.AgentAPIUrl)

				var err error
				sourceID, err = backendActioner.CreateSource("test-source-" + uuid.NewString()[:8])
				Expect(err).ToNot(HaveOccurred(), "failed to create source")
				GinkgoWriter.Printf("Created source: %s\n", sourceID)
			})

			AfterEach(func() {
				if cfg.KeepContainers {
					GinkgoWriter.Println("Keeping agent container running (--keep-containers flag set)")
					return
				}
				GinkgoWriter.Println("Stopping agent...")
				_ = stack.Runner.StopContainer(AgentContainerName)
				_ = stack.Runner.RemoveContainer(AgentContainerName)
				_ = stack.Runner.RemoveVolume(AgentVolumeName)

				GinkgoWriter.Println("Deleting source...")
				_ = backendActioner.DeleteSource(sourceID)
			})

			// Given an agent in connected mode with valid vCenter credentials
			// When the collector successfully gathers inventory
			// Then the source should have the inventory populated
			It("should push inventory to backend after successful collection", func() {
				// Arrange
				agentID := uuid.NewString()
				containerID, err := stack.Runner.StartContainer(
					NewContainerConfig(AgentContainerName, cfg.AgentImage).
						WithPort(8000, 8000).
						WithVolume(AgentVolumeName, "/var/lib/agent").
						WithEnvVar("AGENT_MODE", "connected").
						WithEnvVar("AGENT_AGENT_ID", agentID).
						WithEnvVar("AGENT_SOURCE_ID", sourceID).
						WithEnvVar("AGENT_DATA_FOLDER", "/var/lib/agent").
						WithEnvVar("AGENT_CONSOLE_URL", "http://localhost:8081").
						WithEnvVar("AGENT_CONSOLE_UPDATE_INTERVAL", "1s"),
				)
				Expect(err).ToNot(HaveOccurred(), "failed to start agent")
				GinkgoWriter.Printf("Agent started with ID: %s container ID: %s\n", agentID, containerID)

				Eventually(func() error {
					_, err := agentActioner.Status()
					return err
				}, 30*time.Second, 1*time.Second).Should(BeNil())
				Expect(err).ToNot(HaveOccurred(), "agent not ready")

				// Act
				GinkgoWriter.Println("Starting collector with valid credentials...")
				_, err = agentActioner.StartCollector("https://localhost:8989/sdk", VcsimUsername, VcsimPassword)
				Expect(err).ToNot(HaveOccurred(), "failed to start collector")

				Eventually(func() string {
					status, err := agentActioner.GetCollectorStatus()
					if err != nil {
						return "error"
					}
					GinkgoWriter.Printf("Collector status: %s %s\n", status.Status, status.Error)
					return status.Status
				}, 60*time.Second, 2*time.Second).Should(Equal("collected"))

				// Give time for inventory to be pushed to backend
				time.Sleep(5 * time.Second)

				source, err := backendActioner.GetSource(sourceID)
				Expect(err).ToNot(HaveOccurred(), "failed to get source")

				// Assert
				GinkgoWriter.Printf("Source inventory: %+v\n", source.Inventory)
				Expect(source.Inventory).ToNot(BeNil(), "expected inventory to be populated")
				Expect(source.Inventory.VcenterId).ToNot(BeEmpty(), "expected vcenter_id to be set")
			})
		})
	})
})
