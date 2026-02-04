package services_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

// MockCollector implements Collector interface for testing
type MockCollector struct {
	state models.CollectorState
}

func NewMockCollector(state models.CollectorState) *MockCollector {
	return &MockCollector{
		state: state,
	}
}

func (m *MockCollector) GetStatus() models.CollectorStatus {
	return models.CollectorStatus{
		State: m.state,
	}
}

func (m *MockCollector) SetState(state models.CollectorState) {
	m.state = state
}

var _ = Describe("Console Service", func() {
	var (
		sched     *scheduler.Scheduler
		collector *MockCollector
		agentID   string
		sourceID  string
		cfg       config.Agent
		db        *sql.DB
		st        *store.Store
	)

	BeforeEach(func() {
		agentID = uuid.New().String()
		sourceID = uuid.New().String()

		sched = scheduler.NewScheduler(1)
		collector = NewMockCollector(models.CollectorStateReady)

		var err error
		db, err = store.NewDB(":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(context.Background(), db)
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db)

		cfg = config.Agent{
			ID:             agentID,
			SourceID:       sourceID,
			UpdateInterval: 50 * time.Millisecond,
		}
	})

	AfterEach(func() {
		if sched != nil {
			sched.Close()
		}
		if db != nil {
			db.Close()
		}
	})

	Describe("NewConsoleService", func() {
		It("should create a console service with disconnected status", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			cfg.Mode = "disconnected"
			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv).NotTo(BeNil())

			status := consoleSrv.Status()
			Expect(status.Current).To(Equal(models.ConsoleStatusDisconnected))
			Expect(status.Target).To(Equal(models.ConsoleStatusDisconnected))
		})

		It("should not send status updates in disconnected mode", func() {
			requestReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestReceived <- true
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			cfg.Mode = "disconnected"
			_, err = services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			// Wait longer than updateInterval (50ms) to ensure no requests are fired
			Consistently(requestReceived, 150*time.Millisecond).ShouldNot(Receive())
		})
	})

	Describe("NewConsoleService with connected mode in DB", func() {
		It("should create a console service with connected target status when agent mode is connected", func() {
			// Save configuration with connected mode before creating service
			config := &models.Configuration{
				AgentMode: models.AgentModeConnected,
			}
			err := st.Configuration().Save(context.Background(), config)
			Expect(err).NotTo(HaveOccurred())

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv).NotTo(BeNil())

			status := consoleSrv.Status()
			Expect(status.Current).To(Equal(models.ConsoleStatusDisconnected))
			Expect(status.Target).To(Equal(models.ConsoleStatusConnected))
		})

		It("should start sending status updates when agent mode is connected", func() {
			// Save configuration with connected mode before creating service
			config := &models.Configuration{
				AgentMode: models.AgentModeConnected,
			}
			err := st.Configuration().Save(context.Background(), config)
			Expect(err).NotTo(HaveOccurred())

			requestReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestReceived <- true
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			_, err = services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			Eventually(requestReceived, 1500*time.Millisecond).Should(Receive())
		})

		It("should remain disconnected when agent mode is disconnected", func() {
			// Save configuration with disconnected mode
			config := &models.Configuration{
				AgentMode: models.AgentModeDisconnected,
			}
			err := st.Configuration().Save(context.Background(), config)
			Expect(err).NotTo(HaveOccurred())

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv).NotTo(BeNil())

			status := consoleSrv.Status()
			Expect(status.Current).To(Equal(models.ConsoleStatusDisconnected))
			Expect(status.Target).To(Equal(models.ConsoleStatusDisconnected))
		})
	})

	Describe("Connected mode via SetMode", func() {
		It("should switch to connected target status via SetMode", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv).NotTo(BeNil())

			err = consoleSrv.SetMode(context.Background(), models.AgentModeConnected)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.ConsoleStatusType {
				return consoleSrv.Status().Target
			}, 500*time.Millisecond).Should(Equal(models.ConsoleStatusConnected))

			status := consoleSrv.Status()
			Expect(status.Current).To(Equal(models.ConsoleStatusDisconnected))
		})

		It("should start sending status updates when switched to connected mode", func() {
			requestReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestReceived <- true
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			err = consoleSrv.SetMode(context.Background(), models.AgentModeConnected)
			Expect(err).NotTo(HaveOccurred())

			Eventually(requestReceived, 500*time.Millisecond).Should(Receive())
		})
	})

	Describe("SetMode", func() {
		It("should switch from disconnected to connected mode", func() {
			requestReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestReceived <- true
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			err = consoleSrv.SetMode(context.Background(), models.AgentModeConnected)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.ConsoleStatusType {
				return consoleSrv.Status().Target
			}, 500*time.Millisecond).Should(Equal(models.ConsoleStatusConnected))

			Eventually(requestReceived, 500*time.Millisecond).Should(Receive())
		})

		It("should switch from connected to disconnected mode", func() {
			requestReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestReceived <- true
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			Eventually(requestReceived, 500*time.Millisecond).Should(Receive())

			Expect(consoleSrv.SetMode(context.Background(), models.AgentMode(models.ConsoleStatusDisconnected))).To(BeNil())

			Eventually(func() models.ConsoleStatusType {
				return consoleSrv.Status().Target
			}, 500*time.Millisecond).Should(Equal(models.ConsoleStatusDisconnected))

			// Drain any pending requests
			for len(requestReceived) > 0 {
				<-requestReceived
			}

			// Wait longer than updateInterval (50ms) to ensure no more requests are sent
			Consistently(requestReceived, 150*time.Millisecond).ShouldNot(Receive())
		})

		It("should return current console status", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			status := consoleSrv.Status()

			Expect(status.Current).To(Equal(models.ConsoleStatusDisconnected))
			Expect(status.Target).To(Equal(models.ConsoleStatusDisconnected))
		})
	})

	Describe("Error handling", func() {
		It("should stop sending requests when source is gone (410)", func() {
			statusReceived := make(chan bool, 10)
			inventoryReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "agents") {
					statusReceived <- true
				} else if strings.Contains(r.URL.Path, "sources") {
					inventoryReceived <- true
				}
				w.WriteHeader(http.StatusGone)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			// Set collector to collected status so inventory would be sent if not blocked
			collector.SetState(models.CollectorStateCollected)

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			Eventually(statusReceived, 500*time.Millisecond).Should(Receive())

			// Give time for the error to be processed and loop to stop
			time.Sleep(200 * time.Millisecond)

			// Should not receive any inventory requests after status failed
			Consistently(inventoryReceived, 300*time.Millisecond).ShouldNot(Receive())

			// Should not receive more status requests either
			Consistently(statusReceived, 300*time.Millisecond).ShouldNot(Receive())
		})

		It("should stop sending requests when agent is unauthorized (401)", func() {
			statusReceived := make(chan bool, 10)
			inventoryReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "agents") {
					statusReceived <- true
				} else if strings.Contains(r.URL.Path, "sources") {
					inventoryReceived <- true
				}
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			// Set collector to collected status so inventory would be sent if not blocked
			collector.SetState(models.CollectorStateCollected)

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			Eventually(statusReceived, 500*time.Millisecond).Should(Receive())

			// Give time for the error to be processed and loop to stop
			time.Sleep(200 * time.Millisecond)

			// Should not receive any inventory requests after status failed
			Consistently(inventoryReceived, 300*time.Millisecond).ShouldNot(Receive())

			// Should not receive more status requests either
			Consistently(statusReceived, 300*time.Millisecond).ShouldNot(Receive())
		})

		It("should continue sending requests on transient errors", func() {
			requestReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestReceived <- true
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Should receive multiple requests despite errors
			Eventually(requestReceived, 500*time.Millisecond).Should(Receive())
		})
	})

	Describe("Inventory", func() {
		It("should send inventory when collector status is collected", func() {
			statusReceived := make(chan bool, 10)
			inventoryReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "agents") {
					statusReceived <- true
				} else if strings.Contains(r.URL.Path, "sources") {
					inventoryReceived <- true
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			// Set collector to collected status and save inventory to store
			collector.SetState(models.CollectorStateCollected)
			err = st.Inventory().Save(context.Background(), []byte(`{"vms": [{"name": "vm1"}]}`))
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Should receive status update
			Eventually(statusReceived, 500*time.Millisecond).Should(Receive())

			// Should receive inventory update
			Eventually(inventoryReceived, 500*time.Millisecond).Should(Receive())
		})

		It("should not send inventory when no inventory exists in store", func() {
			statusReceived := make(chan bool, 10)
			inventoryReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "agents") {
					statusReceived <- true
				} else if strings.Contains(r.URL.Path, "sources") {
					inventoryReceived <- true
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			// No inventory saved to store

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Should receive status update
			Eventually(statusReceived, 500*time.Millisecond).Should(Receive())

			// Should NOT receive inventory update since store is empty
			Consistently(inventoryReceived, 300*time.Millisecond).ShouldNot(Receive())
		})

		It("should not resend inventory if unchanged", func() {
			inventoryCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "sources") {
					inventoryCount++
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			collector.SetState(models.CollectorStateCollected)
			err = st.Inventory().Save(context.Background(), []byte(`{"vms": [{"name": "vm1"}]}`))
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Wait for multiple ticks
			time.Sleep(300 * time.Millisecond)

			// Inventory should only be sent once since it hasn't changed
			Expect(inventoryCount).To(Equal(1))
		})

		It("should not send more inventory after source gone error (410)", func() {
			statusCount := 0
			inventoryCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "agents") {
					statusCount++
					if statusCount == 1 {
						w.WriteHeader(http.StatusOK)
						return
					}
					// Second status call returns 410, loop should exit
					w.WriteHeader(http.StatusGone)
					return
				}
				if strings.Contains(r.URL.Path, "sources") {
					inventoryCount++
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			collector.SetState(models.CollectorStateCollected)
			err = st.Inventory().Save(context.Background(), []byte(`{"vms": [{"name": "vm1"}]}`))
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Wait for first tick (status OK, inventory OK) and second tick (status 410, loop exits)
			time.Sleep(300 * time.Millisecond)

			// Inventory should have been sent once (first tick only)
			Expect(inventoryCount).To(Equal(1))

			// Change inventory to trigger a new send attempt if loop were still running
			err = st.Inventory().Save(context.Background(), []byte(`{"vms": [{"name": "vm2"}]}`))
			Expect(err).NotTo(HaveOccurred())

			// Wait for more ticks
			time.Sleep(300 * time.Millisecond)

			// Inventory count should still be 1 since loop exited on 410
			Expect(inventoryCount).To(Equal(1))

			// Error should be stored in status
			status := consoleSrv.Status()
			Expect(status.Error).NotTo(BeNil())
		})

		It("should store error in status when inventory update fails", func() {
			inventoryReceived := make(chan bool, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "agents") {
					w.WriteHeader(http.StatusOK)
					return
				}
				if strings.Contains(r.URL.Path, "sources") {
					inventoryReceived <- true
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			collector.SetState(models.CollectorStateCollected)
			err = st.Inventory().Save(context.Background(), []byte(`{"vms": [{"name": "vm1"}]}`))
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Wait for first inventory request to complete
			Eventually(inventoryReceived, 200*time.Millisecond).Should(Receive())

			// Check error immediately after first tick before next status clears it
			Eventually(func() error {
				return consoleSrv.Status().Error
			}, 100*time.Millisecond).ShouldNot(BeNil())
		})

		It("should not clear status error when inventory is unchanged", func() {
			requestCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				if strings.Contains(r.URL.Path, "agents") {
					// First status request fails, subsequent ones succeed
					if requestCount == 1 {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusOK)
					return
				}
				if strings.Contains(r.URL.Path, "sources") {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			// No inventory in store - inventory dispatch will return sentinel error
			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Wait for first tick to complete (status fails, inventory unchanged)
			time.Sleep(150 * time.Millisecond)

			// Error should still be set because inventory unchanged doesn't clear it
			status := consoleSrv.Status()
			Expect(status.Error).NotTo(BeNil())
		})
	})

	Describe("Stop", func() {
		It("should stop the run loop when called", func() {
			requestReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestReceived <- true
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			err = consoleSrv.SetMode(context.Background(), models.AgentModeConnected)
			Expect(err).NotTo(HaveOccurred())

			// Wait for first request to ensure loop is running
			Eventually(requestReceived, 500*time.Millisecond).Should(Receive())

			// Stop the service
			consoleSrv.Stop()

			// Drain any pending requests
			for len(requestReceived) > 0 {
				<-requestReceived
			}

			// Should not receive more requests after stop
			Consistently(requestReceived, 200*time.Millisecond).ShouldNot(Receive())
		})

		It("should not block when called twice", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			err = consoleSrv.SetMode(context.Background(), models.AgentModeConnected)
			Expect(err).NotTo(HaveOccurred())

			// Give time for loop to start
			time.Sleep(100 * time.Millisecond)

			// First stop
			done := make(chan struct{})
			go func() {
				consoleSrv.Stop()
				close(done)
			}()
			Eventually(done, 500*time.Millisecond).Should(BeClosed())

			// Give time for the run loop to process stop and exit
			time.Sleep(100 * time.Millisecond)

			// Second stop should not block (close channel is nil now)
			done2 := make(chan struct{})
			go func() {
				consoleSrv.Stop()
				close(done2)
			}()
			Eventually(done2, 500*time.Millisecond).Should(BeClosed())
		})

		It("should not block when called without starting the loop", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			// Create service but don't start the loop (stay in disconnected mode)
			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			// Stop should not block even though loop was never started
			done := make(chan struct{})
			go func() {
				consoleSrv.Stop()
				close(done)
			}()
			Eventually(done, 500*time.Millisecond).Should(BeClosed())
		})

	})

	Describe("Backoff", func() {
		It("should apply exponential backoff on transient errors", func() {
			requestTimes := make(chan time.Time, 20)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "agents") {
					requestTimes <- time.Now()
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Collect request times
			var times []time.Time
			timeout := time.After(500 * time.Millisecond)
		collectLoop:
			for {
				select {
				case t := <-requestTimes:
					times = append(times, t)
				case <-timeout:
					break collectLoop
				}
			}

			// With backoff, we should have fewer requests than without
			// Without backoff at 50ms interval over 500ms we'd have ~10 requests
			// With backoff we should have fewer due to increasing delays
			Expect(len(times)).To(BeNumerically(">=", 1))
			Expect(len(times)).To(BeNumerically("<", 10))
		})

		It("should reset backoff after successful request", func() {
			failCount := 2
			requestCount := 0
			requestTimes := make(chan time.Time, 20)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "agents") {
					requestTimes <- time.Now()
					requestCount++
					if requestCount <= failCount {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			// Save inventory so it gets sent and clears error
			err = st.Inventory().Save(context.Background(), []byte(`{"vms": [{"name": "vm1"}]}`))
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Wait for recovery
			time.Sleep(400 * time.Millisecond)

			// After success, error should be cleared
			Eventually(func() error {
				return consoleSrv.Status().Error
			}, 500*time.Millisecond).Should(BeNil())
		})

		It("should skip requests while backoff is active", func() {
			statusCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "agents") {
					statusCount++
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Wait for multiple tick intervals
			time.Sleep(300 * time.Millisecond)

			// With 50ms update interval and 300ms wait, without backoff we'd have ~6 requests
			// With backoff active, requests should be skipped
			Expect(statusCount).To(BeNumerically("<", 6))
		})
	})
})
