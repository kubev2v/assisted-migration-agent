package v1_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	v1 "github.com/kubev2v/assisted-migration-agent/internal/services/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("ServiceManager", func() {
	var (
		db            *sql.DB
		st            *store.Store
		cfg           *config.Configuration
		consoleClient *console.Client
		server        *httptest.Server
		tmpDir        string
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "manager-test-*")
		Expect(err).NotTo(HaveOccurred())

		db, err = store.NewConnection(nil, filepath.Join(tmpDir, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())
		Expect(st.Migrate(context.Background(), "")).To(Succeed())
		Expect(st.InitCollection(context.Background())).To(Succeed())

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		consoleClient, err = console.NewConsoleClient(server.URL, "")
		Expect(err).NotTo(HaveOccurred())

		cfg = config.NewConfigurationWithOptionsAndDefaults(
			config.WithAgent(config.Agent{
				ID:       uuid.New().String(),
				SourceID: uuid.New().String(),
				Mode:     "disconnected",
			}),
		)
	})

	AfterEach(func() {
		server.Close()
		_ = db.Close()
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	Describe("NewServiceManager", func() {
		It("creates a service manager with all options", func() {
			mgr := v1.NewServiceManager(
				v1.WithConfig(cfg),
				v1.WithStore(st),
				v1.WithConsoleClient(consoleClient),
			)
			Expect(mgr).NotTo(BeNil())
		})
	})

	Describe("Initialize", func() {
		It("fails when config is nil", func() {
			mgr := v1.NewServiceManager(
				v1.WithStore(st),
				v1.WithConsoleClient(consoleClient),
			)
			err := mgr.Initialize()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("config is required"))
		})

		It("fails when store is nil", func() {
			mgr := v1.NewServiceManager(
				v1.WithConfig(cfg),
				v1.WithConsoleClient(consoleClient),
			)
			err := mgr.Initialize()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("store is required"))
		})

		It("fails when console client is nil", func() {
			mgr := v1.NewServiceManager(
				v1.WithConfig(cfg),
				v1.WithStore(st),
			)
			err := mgr.Initialize()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("console client is required"))
		})

		It("initializes all services successfully", func() {
			mgr := v1.NewServiceManager(
				v1.WithConfig(cfg),
				v1.WithStore(st),
				v1.WithConsoleClient(consoleClient),
			)
			err := mgr.Initialize()
			Expect(err).NotTo(HaveOccurred())

			Expect(mgr.ConsoleService()).NotTo(BeNil())
			Expect(mgr.CollectorService()).NotTo(BeNil())
			Expect(mgr.InspectorService()).NotTo(BeNil())
			Expect(mgr.VddkService()).NotTo(BeNil())
			Expect(mgr.InventoryService()).NotTo(BeNil())
			Expect(mgr.VirtualMachineService()).NotTo(BeNil())
			Expect(mgr.GroupService()).NotTo(BeNil())

			mgr.Stop(context.Background())
		})
	})

	Describe("Stop", func() {
		It("stops all services gracefully", func() {
			mgr := v1.NewServiceManager(
				v1.WithConfig(cfg),
				v1.WithStore(st),
				v1.WithConsoleClient(consoleClient),
			)
			err := mgr.Initialize()
			Expect(err).NotTo(HaveOccurred())

			Expect(func() { mgr.Stop(context.Background()) }).NotTo(Panic())
		})
	})
})
