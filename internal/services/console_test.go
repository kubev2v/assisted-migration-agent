package services_test

import (
	"context"
	"database/sql"
	"io"
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
	status    models.CollectorStatusType
	inventory []byte
	err       error
}

func NewMockCollector(status models.CollectorStatusType) *MockCollector {
	return &MockCollector{
		status:    status,
		inventory: []byte("{}"),
	}
}

func (m *MockCollector) Status() models.CollectorStatusType {
	return m.status
}

func (m *MockCollector) Inventory() (io.Reader, error) {
	if m.err != nil {
		return nil, m.err
	}
	return strings.NewReader(string(m.inventory)), nil
}

func (m *MockCollector) SetStatus(status models.CollectorStatusType) {
	m.status = status
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
		collector = NewMockCollector(models.CollectorStatusWaitingForCredentials)

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
			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)
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
			_ = services.NewConsoleService(cfg, sched, client, collector, st)

			// Wait longer than updateInterval (50ms) to ensure no requests are fired
			Consistently(requestReceived, 150*time.Millisecond).ShouldNot(Receive())
		})
	})

	Describe("NewConsoleService with data sharing allowed in DB", func() {
		It("should create a console service with connected target status when data sharing is allowed", func() {
			// Save credentials with data sharing allowed before creating service
			creds := &models.Credentials{
				URL:                  "https://vcenter.example.com",
				Username:             "admin",
				Password:             "secret",
				IsDataSharingAllowed: true,
			}
			err := st.Credentials().Save(context.Background(), creds)
			Expect(err).NotTo(HaveOccurred())

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(consoleSrv).NotTo(BeNil())

			status := consoleSrv.Status()
			Expect(status.Current).To(Equal(models.ConsoleStatusDisconnected))
			Expect(status.Target).To(Equal(models.ConsoleStatusConnected))
		})

		It("should start sending status updates when data sharing is allowed", func() {
			// Save credentials with data sharing allowed before creating service
			creds := &models.Credentials{
				URL:                  "https://vcenter.example.com",
				Username:             "admin",
				Password:             "secret",
				IsDataSharingAllowed: true,
			}
			err := st.Credentials().Save(context.Background(), creds)
			Expect(err).NotTo(HaveOccurred())

			requestReceived := make(chan bool, 10)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestReceived <- true
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			_ = services.NewConsoleService(cfg, sched, client, collector, st)

			Eventually(requestReceived, 500*time.Millisecond).Should(Receive())
		})

		It("should remain disconnected when data sharing is not allowed", func() {
			// Save credentials with data sharing NOT allowed
			creds := &models.Credentials{
				URL:                  "https://vcenter.example.com",
				Username:             "admin",
				Password:             "secret",
				IsDataSharingAllowed: false,
			}
			err := st.Credentials().Save(context.Background(), creds)
			Expect(err).NotTo(HaveOccurred())

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)
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

			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)
			Expect(consoleSrv).NotTo(BeNil())

			consoleSrv.SetMode(models.AgentModeConnected)

			status := consoleSrv.Status()
			Expect(status.Current).To(Equal(models.ConsoleStatusDisconnected))
			Expect(status.Target).To(Equal(models.ConsoleStatusConnected))
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

			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)
			consoleSrv.SetMode(models.AgentModeConnected)

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

			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)

			consoleSrv.SetMode(models.AgentModeConnected)

			status := consoleSrv.Status()
			Expect(status.Target).To(Equal(models.ConsoleStatusConnected))

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

			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)
			consoleSrv.SetMode(models.AgentModeConnected)

			Eventually(requestReceived, 500*time.Millisecond).Should(Receive())

			consoleSrv.SetMode(models.AgentModeDisconnected)

			status := consoleSrv.Status()
			Expect(status.Target).To(Equal(models.ConsoleStatusDisconnected))

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

			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)

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
			collector.SetStatus(models.CollectorStatusCollected)
			collector.inventory = []byte(`{"test": "data"}`)

			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)
			consoleSrv.SetMode(models.AgentModeConnected)

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
			collector.SetStatus(models.CollectorStatusCollected)
			collector.inventory = []byte(`{"test": "data"}`)

			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)
			consoleSrv.SetMode(models.AgentModeConnected)

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

			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)
			consoleSrv.SetMode(models.AgentModeConnected)

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

			// Set collector to collected status with inventory
			collector.SetStatus(models.CollectorStatusCollected)
			collector.inventory = []byte(`{"vms": [{"name": "vm1"}]}`)

			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)
			consoleSrv.SetMode(models.AgentModeConnected)

			// Should receive status update
			Eventually(statusReceived, 500*time.Millisecond).Should(Receive())

			// Should receive inventory update
			Eventually(inventoryReceived, 500*time.Millisecond).Should(Receive())
		})

		It("should not send inventory when collector status is not collected", func() {
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

			// Collector status is WaitingForCredentials (set in BeforeEach)
			collector.inventory = []byte(`{"vms": [{"name": "vm1"}]}`)

			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)
			consoleSrv.SetMode(models.AgentModeConnected)

			// Should receive status update
			Eventually(statusReceived, 500*time.Millisecond).Should(Receive())

			// Should NOT receive inventory update
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

			collector.SetStatus(models.CollectorStatusCollected)
			collector.inventory = []byte(`{"vms": [{"name": "vm1"}]}`)

			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)
			consoleSrv.SetMode(models.AgentModeConnected)

			// Wait for multiple ticks
			time.Sleep(300 * time.Millisecond)

			// Inventory should only be sent once since it hasn't changed
			Expect(inventoryCount).To(Equal(1))
		})

		It("should not send more inventory after unauthorized error (401)", func() {
			inventoryCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "agents") {
					w.WriteHeader(http.StatusOK)
					return
				}
				if strings.Contains(r.URL.Path, "sources") {
					inventoryCount++
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			collector.SetStatus(models.CollectorStatusCollected)
			collector.inventory = []byte(`{"vms": [{"name": "vm1"}]}`)

			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)
			consoleSrv.SetMode(models.AgentModeConnected)

			// Wait for inventory to be sent and fail
			time.Sleep(300 * time.Millisecond)

			// Inventory should have been attempted once
			Expect(inventoryCount).To(Equal(1))

			// Change inventory to trigger a new send attempt
			collector.inventory = []byte(`{"vms": [{"name": "vm2"}]}`)

			// Wait for more ticks
			time.Sleep(300 * time.Millisecond)

			// Error should be stored in status
			status := consoleSrv.Status()
			Expect(status.Error).NotTo(BeNil())
		})

		It("should store error in status when inventory update fails", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "agents") {
					w.WriteHeader(http.StatusOK)
					return
				}
				if strings.Contains(r.URL.Path, "sources") {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := console.NewConsoleClient(server.URL, "")
			Expect(err).NotTo(HaveOccurred())

			collector.SetStatus(models.CollectorStatusCollected)
			collector.inventory = []byte(`{"vms": [{"name": "vm1"}]}`)

			consoleSrv := services.NewConsoleService(cfg, sched, client, collector, st)
			consoleSrv.SetMode(models.AgentModeConnected)

			// Wait for inventory to be sent and fail
			time.Sleep(300 * time.Millisecond)

			// Error should be stored in status
			status := consoleSrv.Status()
			Expect(status.Error).NotTo(BeNil())
			Expect(status.Error.Error()).To(ContainSubstring("failed to update source inventory"))
		})
	})
})
