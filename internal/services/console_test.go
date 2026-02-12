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
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
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

	Context("NewConsoleService", func() {
		// Given a console service configuration in disconnected mode
		// When we create a new console service
		// Then it should have disconnected status for both current and target
		It("should create a console service with disconnected status", func() {
			// Arrange
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())
			cfg.Mode = "disconnected"

			// Act
			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv).NotTo(BeNil())
			status := consoleSrv.Status()
			Expect(status.Current).To(Equal(models.ConsoleStatusDisconnected))
			Expect(status.Target).To(Equal(models.ConsoleStatusDisconnected))
		})

		// Given a console service in disconnected mode
		// When we wait for the update interval
		// Then no status updates should be sent to the server
		It("should not send status updates in disconnected mode", func() {
			// Arrange
			requestReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestReceived <- true
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())
			cfg.Mode = "disconnected"

			// Act
			_, err = services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			// Assert - Wait longer than updateInterval (50ms) to ensure no requests are fired
			Consistently(requestReceived, 150*time.Millisecond).ShouldNot(Receive())
		})
	})

	Context("NewConsoleService with connected mode in DB", func() {
		// Given a configuration with connected agent mode saved in DB
		// When we create a new console service
		// Then it should have connected target status
		It("should create a console service with connected target status when agent mode is connected", func() {
			// Arrange
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

			// Act
			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv).NotTo(BeNil())
			status := consoleSrv.Status()
			Expect(status.Current).To(Equal(models.ConsoleStatusDisconnected))
			Expect(status.Target).To(Equal(models.ConsoleStatusConnected))
		})

		// Given a configuration with connected agent mode saved in DB
		// When we create a new console service
		// Then it should start sending status updates
		It("should start sending status updates when agent mode is connected", func() {
			// Arrange
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

			// Act
			_, err = services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			// Assert
			Eventually(requestReceived, 1500*time.Millisecond).Should(Receive())
		})

		// Given a configuration with disconnected agent mode saved in DB
		// When we create a new console service
		// Then it should remain in disconnected status
		It("should remain disconnected when agent mode is disconnected", func() {
			// Arrange
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

			// Act
			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv).NotTo(BeNil())
			status := consoleSrv.Status()
			Expect(status.Current).To(Equal(models.ConsoleStatusDisconnected))
			Expect(status.Target).To(Equal(models.ConsoleStatusDisconnected))
		})
	})

	Context("Connected mode via SetMode", func() {
		// Given a console service in disconnected mode
		// When we call SetMode with connected mode
		// Then the target status should switch to connected
		It("should switch to connected target status via SetMode", func() {
			// Arrange
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv).NotTo(BeNil())

			// Act
			err = consoleSrv.SetMode(context.Background(), models.AgentModeConnected)
			Expect(err).NotTo(HaveOccurred())

			// Assert
			Eventually(func() models.ConsoleStatusType {
				return consoleSrv.Status().Target
			}, 500*time.Millisecond).Should(Equal(models.ConsoleStatusConnected))
		})

		// Given a console service in disconnected mode
		// When we switch to connected mode via SetMode
		// Then it should start sending status updates
		It("should start sending status updates when switched to connected mode", func() {
			// Arrange
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

			// Act
			err = consoleSrv.SetMode(context.Background(), models.AgentModeConnected)
			Expect(err).NotTo(HaveOccurred())

			// Assert
			Eventually(requestReceived, 500*time.Millisecond).Should(Receive())
		})
	})

	Context("SetMode", func() {
		// Given a console service in disconnected mode
		// When we switch to connected mode
		// Then the target should change and requests should be sent
		It("should switch from disconnected to connected mode", func() {
			// Arrange
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

			// Act
			err = consoleSrv.SetMode(context.Background(), models.AgentModeConnected)
			Expect(err).NotTo(HaveOccurred())

			// Assert
			Eventually(func() models.ConsoleStatusType {
				return consoleSrv.Status().Target
			}, 500*time.Millisecond).Should(Equal(models.ConsoleStatusConnected))

			Eventually(requestReceived, 500*time.Millisecond).Should(Receive())
		})

		// Given a console service in connected mode
		// When we switch to disconnected mode
		// Then the target should change and no more requests should be sent
		It("should switch from connected to disconnected mode", func() {
			// Arrange
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

			// Act
			Expect(consoleSrv.SetMode(context.Background(), models.AgentMode(models.ConsoleStatusDisconnected))).To(BeNil())

			// Assert
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

		// Given a console service in disconnected mode
		// When we get the status
		// Then it should return current console status
		It("should return current console status", func() {
			// Arrange
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			// Act
			status := consoleSrv.Status()

			// Assert
			Expect(status.Current).To(Equal(models.ConsoleStatusDisconnected))
			Expect(status.Target).To(Equal(models.ConsoleStatusDisconnected))
		})
	})

	Context("Error handling", func() {
		// Given a console service in connected mode receiving 410 Gone responses
		// When the server responds with 410 Gone
		// Then it should stop sending all requests
		It("should stop sending requests when source is gone (410)", func() {
			// Arrange
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

			collector.SetState(models.CollectorStateCollected)

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Act
			Eventually(statusReceived, 500*time.Millisecond).Should(Receive())
			time.Sleep(200 * time.Millisecond)

			// Assert
			Consistently(inventoryReceived, 300*time.Millisecond).ShouldNot(Receive())
			Consistently(statusReceived, 300*time.Millisecond).ShouldNot(Receive())
		})

		// Given a console service in connected mode receiving 401 Unauthorized responses
		// When the server responds with 401 Unauthorized
		// Then it should stop sending all requests
		It("should stop sending requests when agent is unauthorized (401)", func() {
			// Arrange
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

			collector.SetState(models.CollectorStateCollected)

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Act
			Eventually(statusReceived, 500*time.Millisecond).Should(Receive())
			time.Sleep(200 * time.Millisecond)

			// Assert
			Consistently(inventoryReceived, 300*time.Millisecond).ShouldNot(Receive())
			Consistently(statusReceived, 300*time.Millisecond).ShouldNot(Receive())
		})

		// Given a console service in connected mode receiving transient errors
		// When the server responds with 500 Internal Server Error
		// Then it should continue sending requests
		It("should continue sending requests on transient errors", func() {
			// Arrange
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

			// Act
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Assert
			Eventually(requestReceived, 500*time.Millisecond).Should(Receive())
		})
	})

	Context("Inventory", func() {
		// Given a collector in collected state with inventory in store
		// When the console service is in connected mode
		// Then it should send the inventory to the server
		It("should send inventory when collector status is collected", func() {
			// Arrange
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

			collector.SetState(models.CollectorStateCollected)
			err = st.Inventory().Save(context.Background(), []byte(`{"vms": [{"name": "vm1"}]}`))
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			// Act
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Assert
			Eventually(statusReceived, 500*time.Millisecond).Should(Receive())
			Eventually(inventoryReceived, 500*time.Millisecond).Should(Receive())
		})

		// Given a console service in connected mode with no inventory in store
		// When the update loop runs
		// Then it should not send inventory requests
		It("should not send inventory when no inventory exists in store", func() {
			// Arrange
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

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			// Act
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

			// Assert
			Eventually(statusReceived, 500*time.Millisecond).Should(Receive())
			Consistently(inventoryReceived, 300*time.Millisecond).ShouldNot(Receive())
		})

		// Given inventory that has not changed since last send
		// When the update loop runs multiple times
		// Then inventory should only be sent once
		It("should not resend inventory if unchanged", func() {
			// Arrange
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

			// Act
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())
			time.Sleep(300 * time.Millisecond)

			// Assert
			Expect(inventoryCount).To(Equal(1))
		})

		// Given a console service that receives 410 Gone after first successful request
		// When the inventory changes
		// Then no more inventory requests should be sent
		It("should not send more inventory after source gone error (410)", func() {
			// Arrange
			statusCount := 0
			inventoryCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "agents") {
					statusCount++
					if statusCount == 1 {
						w.WriteHeader(http.StatusOK)
						return
					}
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

			// Act
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())
			time.Sleep(300 * time.Millisecond)
			Expect(inventoryCount).To(Equal(1))

			err = st.Inventory().Save(context.Background(), []byte(`{"vms": [{"name": "vm2"}]}`))
			Expect(err).NotTo(HaveOccurred())
			time.Sleep(300 * time.Millisecond)

			// Assert
			Expect(inventoryCount).To(Equal(1))
			status := consoleSrv.Status()
			Expect(status.Error).NotTo(BeNil())
		})

		// Given an inventory update that fails with a bad request error
		// When the error occurs
		// Then the error should be stored in the service status
		It("should store error in status when inventory update fails", func() {
			// Arrange
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

			// Act
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())
			Eventually(inventoryReceived, 200*time.Millisecond).Should(Receive())

			// Assert
			Eventually(func() error {
				return consoleSrv.Status().Error
			}, 100*time.Millisecond).ShouldNot(BeNil())
		})
	})

	Context("Stop", func() {
		// Given a console service with an active run loop
		// When Stop is called
		// Then the run loop should stop and no more requests should be sent
		It("should stop the run loop when called", func() {
			// Arrange
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

			// Act
			consoleSrv.Stop()

			for len(requestReceived) > 0 {
				<-requestReceived
			}

			// Assert
			Consistently(requestReceived, 200*time.Millisecond).ShouldNot(Receive())
		})

		// Given a console service that has already been stopped
		// When Stop is called again
		// Then it should not block
		It("should not block when called twice", func() {
			// Arrange
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

			time.Sleep(100 * time.Millisecond)

			done := make(chan struct{})
			go func() {
				consoleSrv.Stop()
				close(done)
			}()
			Eventually(done, 500*time.Millisecond).Should(BeClosed())

			time.Sleep(100 * time.Millisecond)

			// Act
			done2 := make(chan struct{})
			go func() {
				consoleSrv.Stop()
				close(done2)
			}()

			// Assert
			Eventually(done2, 500*time.Millisecond).Should(BeClosed())
		})

		// Given a console service that never started its run loop
		// When Stop is called
		// Then it should not block
		It("should not block when called without starting the loop", func() {
			// Arrange
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			// Act
			done := make(chan struct{})
			go func() {
				consoleSrv.Stop()
				close(done)
			}()

			// Assert
			Eventually(done, 500*time.Millisecond).Should(BeClosed())
		})
	})

	Context("Backoff", func() {
		// Given a console service receiving transient errors
		// When multiple requests fail
		// Then exponential backoff should reduce request frequency
		It("should apply exponential backoff on transient errors", func() {
			// Arrange
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

			// Act
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())

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

			// Assert
			Expect(len(times)).To(BeNumerically(">=", 1))
			Expect(len(times)).To(BeNumerically("<", 10))
		})

		// Given a console service that has experienced transient errors
		// When a successful request is made
		// Then the backoff should be reset and errors cleared
		It("should reset backoff after successful request", func() {
			// Arrange
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

			err = st.Inventory().Save(context.Background(), []byte(`{"vms": [{"name": "vm1"}]}`))
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			// Act
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())
			time.Sleep(400 * time.Millisecond)

			// Assert
			Eventually(func() error {
				return consoleSrv.Status().Error
			}, 500*time.Millisecond).Should(BeNil())
		})

		// Given a console service with active backoff due to errors
		// When the update interval passes
		// Then requests should be skipped during backoff period
		It("should skip requests while backoff is active", func() {
			// Arrange
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

			// Act
			Expect(consoleSrv.SetMode(context.Background(), models.AgentModeConnected)).To(BeNil())
			time.Sleep(300 * time.Millisecond)

			// Assert
			Expect(statusCount).To(BeNumerically("<", 6))
		})
	})

	Context("SetMode no-op and fatal", func() {
		// Given a console service already in disconnected mode
		// When we call SetMode with disconnected mode
		// Then it should be a no-op and not start the run loop
		It("should be a no-op when setting the same mode", func() {
			// Arrange
			requestReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestReceived <- true
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())
			cfg.Mode = "disconnected"

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			// Act - set the same mode (disconnected -> disconnected)
			err = consoleSrv.SetMode(context.Background(), models.AgentModeDisconnected)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Consistently(requestReceived, 150*time.Millisecond).ShouldNot(Receive())
		})

		// Given a console service that has been fatally stopped (410 response)
		// When we try to set a new mode
		// Then it should return a ModeConflictError
		It("should return ModeConflictError when fatally stopped", func() {
			// Arrange
			statusReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				statusReceived <- true
				w.WriteHeader(http.StatusGone)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			// First connect so that the run loop starts and receives the 410
			err = consoleSrv.SetMode(context.Background(), models.AgentModeConnected)
			Expect(err).NotTo(HaveOccurred())
			Eventually(statusReceived, 500*time.Millisecond).Should(Receive())
			// Wait for the fatal stop to be processed
			time.Sleep(200 * time.Millisecond)

			// Act - try to change mode again
			err = consoleSrv.SetMode(context.Background(), models.AgentModeDisconnected)

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsModeConflictError(err)).To(BeTrue())
		})
	})

	Context("GetMode", func() {
		// Given a console service with disconnected mode saved in store
		// When we call GetMode
		// Then it should return the mode from the store
		It("should return the mode from the store", func() {
			// Arrange
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())
			cfg.Mode = "disconnected"

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			// Act
			mode, err := consoleSrv.GetMode(context.Background())

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(mode).To(Equal(models.AgentModeDisconnected))
		})

		// Given a console service that was switched to connected mode
		// When we call GetMode
		// Then it should return connected
		It("should reflect mode change from SetMode", func() {
			// Arrange
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

			// Act
			mode, err := consoleSrv.GetMode(context.Background())

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(mode).To(Equal(models.AgentModeConnected))
		})
	})

	Context("Legacy status", func() {
		// Given a console service with legacy status enabled
		// When the collector is in ready state
		// Then it should map to legacy WaitingForCredentials status
		It("should map collector states to legacy statuses", func() {
			// Arrange
			var receivedPath string
			requestReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedPath = r.URL.Path
				requestReceived <- true
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())
			cfg.LegacyStatusEnabled = true

			consoleSrv, err := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = consoleSrv.SetMode(context.Background(), models.AgentModeConnected)
			Expect(err).NotTo(HaveOccurred())

			// Assert - at least one request was sent
			Eventually(requestReceived, 500*time.Millisecond).Should(Receive())
			Expect(receivedPath).To(ContainSubstring("agents"))
		})
	})
})
