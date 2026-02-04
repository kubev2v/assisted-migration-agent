package main

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Agent disconnected env", Ordered, func() {
	var (
		stack       *Stack
		proxy       *Proxy
		requests    chan Request
		proxyServer *http.Server
		obs         *Observer
	)

	BeforeAll(func() {
		var err error
		stack, err = NewStack(cfg)
		Expect(err).ToNot(HaveOccurred(), "failed to create stack")

		GinkgoWriter.Println("Starting postgres...")
		err = stack.StartPostgres()
		Expect(err).ToNot(HaveOccurred(), "failed to start postgres")

		target, err := url.Parse(cfg.BackendAgentEndpoint)
		Expect(err).ToNot(HaveOccurred(), "failed to parse backend endpoint")

		proxy, requests = NewProxy(target)
		proxyServer = &http.Server{
			Addr:    ":8080",
			Handler: proxy.Handler(),
		}
		go proxyServer.ListenAndServe()
		time.Sleep(100 * time.Millisecond) // wait for proxy to be ready
		GinkgoWriter.Println("Proxy started on :8080")
	})

	AfterAll(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = proxyServer.Shutdown(ctx)
		proxy.Close()

		_ = stack.StopPostgres()
	})

	Context("mode starting up", func() {
		var actioner *Actioner

		BeforeEach(func() {
			obs = NewObserver(requests)
			actioner = NewActioner(cfg.AgentAPIUrl)
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

		It("should start in disconnected mode", func() {
			GinkgoWriter.Println("Starting agent in disconnected mode...")
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

			err = actioner.WaitForReady(30 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "agent not ready")

			time.Sleep(5 * time.Second) // wait to observe requests
			reqs := obs.Requests()
			GinkgoWriter.Printf("Observed %d requests\n", len(reqs))
			Expect(reqs).To(BeEmpty(), "expected no requests in disconnected mode")
		})

		It("should start in connected mode", func() {
			GinkgoWriter.Println("Starting agent in connected mode...")
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
			GinkgoWriter.Printf("Agent started with ID: %s. Container ID: %s\n", agentID, containerID)

			err = actioner.WaitForReady(30 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "agent not ready")

			time.Sleep(5 * time.Second) // wait to observe requests
			reqs := obs.Requests()
			GinkgoWriter.Printf("Observed %d requests\n", len(reqs))
			Expect(reqs).ToNot(BeEmpty(), "expected requests in connected mode")
		})
	})

	Context("mode switching", func() {
		var actioner *Actioner

		BeforeEach(func() {
			obs = NewObserver(requests)
			actioner = NewActioner(cfg.AgentAPIUrl)
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

		It("should switch from connected to disconnected mode", func() {
			GinkgoWriter.Println("Starting agent in connected mode...")
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

			err = actioner.WaitForReady(30 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "agent not ready")

			time.Sleep(5 * time.Second) // wait for agent to make requests
			initialReqs := obs.Requests()
			GinkgoWriter.Printf("Observed %d requests before mode switch\n", len(initialReqs))
			Expect(initialReqs).ToNot(BeEmpty(), "expected requests in connected mode")

			// Switch to disconnected mode
			GinkgoWriter.Println("Switching to disconnected mode...")
			status, err := actioner.SetAgentMode("disconnected")
			Expect(err).ToNot(HaveOccurred(), "failed to switch mode")
			Expect(status.Mode).To(Equal("disconnected"), "expected mode to be disconnected")

			// Wait and verify no new requests
			time.Sleep(5 * time.Second)
			afterSwitchReqs := obs.Requests()
			GinkgoWriter.Printf("Observed %d requests after mode switch\n", len(afterSwitchReqs))

			// Should have same number of requests (no new ones after switching to disconnected)
			Expect(len(afterSwitchReqs)).To(Equal(len(initialReqs)), "expected no new requests after switching to disconnected")
		})

		It("should switch from disconnected to connected mode", func() {
			GinkgoWriter.Println("Starting agent in disconnected mode...")
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

			err = actioner.WaitForReady(30 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "agent not ready")

			initialReqs := obs.Requests()
			GinkgoWriter.Printf("Observed %d requests before mode switch\n", len(initialReqs))
			Expect(initialReqs).To(BeEmpty(), "expected no requests in disconnected mode")

			// Switch to connected mode
			GinkgoWriter.Println("Switching to connected mode...")
			status, err := actioner.SetAgentMode("connected")
			Expect(err).ToNot(HaveOccurred(), "failed to switch mode")
			Expect(status.Mode).To(Equal("connected"), "expected mode to be connected")

			// Wait and verify requests are now being made
			time.Sleep(5 * time.Second)
			afterSwitchReqs := obs.Requests()
			GinkgoWriter.Printf("Observed %d requests after mode switch\n", len(afterSwitchReqs))
			Expect(afterSwitchReqs).ToNot(BeEmpty(), "expected requests after switching to connected mode")
		})

		It("should persist mode after agent restart", func() {
			GinkgoWriter.Println("Starting agent in connected mode...")
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

			err = actioner.WaitForReady(30 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "agent not ready")

			// Switch to disconnected mode
			GinkgoWriter.Println("Switching to disconnected mode...")
			status, err := actioner.SetAgentMode("disconnected")
			Expect(err).ToNot(HaveOccurred(), "failed to switch mode")
			Expect(status.Mode).To(Equal("disconnected"), "expected mode to be disconnected")

			// Restart container (stop and start)
			GinkgoWriter.Println("Restarting agent container...")
			err = stack.Runner.RestartContainer(AgentContainerName)
			Expect(err).ToNot(HaveOccurred(), "failed to restart agent")

			err = actioner.WaitForReady(30 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "agent not ready after restart")

			// Verify mode is still disconnected (persisted from before restart)
			status, err = actioner.GetAgentStatus()
			Expect(err).ToNot(HaveOccurred(), "failed to get agent status")
			GinkgoWriter.Printf("Agent mode after restart: %s\n", status.Mode)
			Expect(status.Mode).To(Equal("disconnected"), "expected mode to persist as disconnected after restart")
		})
	})

	Context("collector", func() {
		var actioner *Actioner

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
			err = WaitForReady(client, "https://localhost:8989/sdk", 30*time.Second)
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
			actioner = NewActioner(cfg.AgentAPIUrl)
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

		It("should collect inventory successfully with valid credentials", func() {
			GinkgoWriter.Println("Starting agent in disconnected mode...")
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

			err = actioner.WaitForReady(30 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "agent not ready")

			// Start collector with valid vcsim credentials
			GinkgoWriter.Println("Starting collector with valid credentials...")
			status, err := actioner.StartCollector("https://localhost:8989/sdk", VcsimUsername, VcsimPassword)
			Expect(err).ToNot(HaveOccurred(), "failed to start collector")
			GinkgoWriter.Printf("Collector status: %s\n", status.Status)

			// Poll for collector to complete
			Eventually(func() string {
				status, err := actioner.GetCollectorStatus()
				if err != nil {
					return "error"
				}
				GinkgoWriter.Printf("Collector status: %s %s\n", status.Status, status.Error)
				return status.Status
			}, 60*time.Second, 2*time.Second).Should(Equal("collected"))

			// Verify inventory is available
			inventory, err := actioner.GetInventory()
			Expect(err).ToNot(HaveOccurred(), "failed to get inventory")
			Expect(inventory).ToNot(BeNil(), "expected inventory to be available")
			GinkgoWriter.Printf("Inventory retrieved successfully\n")
		})

		It("should report error with bad credentials", func() {
			GinkgoWriter.Println("Starting agent in disconnected mode...")
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

			err = actioner.WaitForReady(30 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "agent not ready")

			// Start collector with invalid credentials
			GinkgoWriter.Println("Starting collector with bad credentials...")
			status, err := actioner.StartCollector("https://localhost:8989/sdk", "baduser", "badpass")
			Expect(err).ToNot(HaveOccurred(), "failed to start collector")
			GinkgoWriter.Printf("Collector status: %s\n", status.Status)

			// Poll for collector to report error
			Eventually(func() string {
				status, err := actioner.GetCollectorStatus()
				if err != nil {
					return ""
				}
				GinkgoWriter.Printf("Collector status: %s\n", status.Status)
				return status.Status
			}, 30*time.Second, 2*time.Second).Should(Equal("error"))
		})

		It("should report error with bad vCenter URL", func() {
			GinkgoWriter.Println("Starting agent in disconnected mode...")
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

			err = actioner.WaitForReady(30 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "agent not ready")

			// Start collector with invalid URL
			GinkgoWriter.Println("Starting collector with bad URL...")
			status, err := actioner.StartCollector("https://localhost:9999/sdk", "user", "pass")
			Expect(err).ToNot(HaveOccurred(), "failed to start collector")
			GinkgoWriter.Printf("Collector status: %s\n", status.Status)

			// Poll for collector to report error
			Eventually(func() string {
				status, err := actioner.GetCollectorStatus()
				if err != nil {
					return ""
				}
				GinkgoWriter.Printf("Collector status: %s\n", status.Status)
				return status.Status
			}, 30*time.Second, 2*time.Second).Should(Equal("error"))
		})

		It("should recover from bad credentials to successful collection", func() {
			GinkgoWriter.Println("Starting agent in disconnected mode...")
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

			err = actioner.WaitForReady(30 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "agent not ready")

			// Start collector with invalid credentials first
			GinkgoWriter.Println("Starting collector with bad credentials...")
			status, err := actioner.StartCollector("https://localhost:8989/sdk", "baduser", "badpass")
			Expect(err).ToNot(HaveOccurred(), "failed to start collector")
			GinkgoWriter.Printf("Collector status: %s\n", status.Status)

			// Wait for error state
			Eventually(func() string {
				status, err := actioner.GetCollectorStatus()
				if err != nil {
					return ""
				}
				GinkgoWriter.Printf("Collector status: %s\n", status.Status)
				return status.Status
			}, 30*time.Second, 2*time.Second).Should(Equal("error"))

			// Now retry with correct credentials
			GinkgoWriter.Println("Retrying collector with valid credentials...")
			status, err = actioner.StartCollector("https://localhost:8989/sdk", VcsimUsername, VcsimPassword)
			Expect(err).ToNot(HaveOccurred(), "failed to start collector")
			GinkgoWriter.Printf("Collector status: %s\n", status.Status)

			// Poll for collector to complete
			Eventually(func() string {
				status, err := actioner.GetCollectorStatus()
				if err != nil {
					return "error"
				}
				GinkgoWriter.Printf("Collector status: %s\n", status.Status)
				return status.Status
			}, 60*time.Second, 2*time.Second).Should(Equal("collected"))

			// Verify inventory is available
			inventory, err := actioner.GetInventory()
			Expect(err).ToNot(HaveOccurred(), "failed to get inventory")
			Expect(inventory).ToNot(BeNil(), "expected inventory to be available")
			GinkgoWriter.Printf("Inventory retrieved successfully after recovery\n")
		})

		It("should persist collected state and inventory after agent restart", func() {
			GinkgoWriter.Println("Starting agent in disconnected mode...")
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

			err = actioner.WaitForReady(30 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "agent not ready")

			// Start collector with valid credentials
			GinkgoWriter.Println("Starting collector with valid credentials...")
			status, err := actioner.StartCollector("https://localhost:8989/sdk", VcsimUsername, VcsimPassword)
			Expect(err).ToNot(HaveOccurred(), "failed to start collector")
			GinkgoWriter.Printf("Collector status: %s\n", status.Status)

			// Poll for collector to complete
			Eventually(func() string {
				status, err := actioner.GetCollectorStatus()
				if err != nil {
					return "error"
				}
				GinkgoWriter.Printf("Collector status: %s\n", status.Status)
				return status.Status
			}, 60*time.Second, 2*time.Second).Should(Equal("collected"))

			// Verify inventory is available before restart
			inventory, err := actioner.GetInventory()
			Expect(err).ToNot(HaveOccurred(), "failed to get inventory")
			Expect(inventory).ToNot(BeNil(), "expected inventory to be available")
			GinkgoWriter.Printf("Inventory retrieved successfully before restart\n")

			// Restart container (stop and start)
			GinkgoWriter.Println("Restarting agent container...")
			err = stack.Runner.RestartContainer(AgentContainerName)
			Expect(err).ToNot(HaveOccurred(), "failed to restart agent")

			err = actioner.WaitForReady(30 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "agent not ready after restart")

			// Verify collector status is still "collected"
			collectorStatus, err := actioner.GetCollectorStatus()
			Expect(err).ToNot(HaveOccurred(), "failed to get collector status")
			GinkgoWriter.Printf("Collector status after restart: %s\n", collectorStatus.Status)
			Expect(collectorStatus.Status).To(Equal("collected"), "expected collector status to persist as collected after restart")

			// Verify inventory is still available after restart
			inventory, err = actioner.GetInventory()
			Expect(err).ToNot(HaveOccurred(), "failed to get inventory after restart")
			Expect(inventory).ToNot(BeNil(), "expected inventory to be available after restart")
			GinkgoWriter.Printf("Inventory still available after restart\n")
		})
	})
})
