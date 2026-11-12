package v2_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	api "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	handlers "github.com/kubev2v/assisted-migration-agent/agent/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("Agent update status", func() {
	var (
		dir      string
		cfg      config.Configuration
		provider *updateProvider
		response atomic.Int64
	)
	getStatus := func(rvtools bool) (api.AgentStatus, string) {
		router := gin.New()
		var handler api.ServerInterface = handlers.NewHandler(cfg, provider)
		if rvtools {
			handler = handlers.NewRVToolsHandler(cfg, provider)
		}
		api.RegisterHandlersWithOptions(router, handler, api.GinServerOptions{BaseURL: "/api/v2"})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v2/agent", nil))
		Expect(recorder.Code).To(Equal(http.StatusOK), recorder.Body.String())
		var status api.AgentStatus
		Expect(json.Unmarshal(recorder.Body.Bytes(), &status)).To(Succeed())
		return status, recorder.Body.String()
	}
	connect := func() {
		Expect(provider.console.SetMode(context.Background(), models.AgentModeConnected)).To(Succeed())
		Eventually(func() models.ConsoleStatusType {
			return provider.console.Status().Current
		}).Should(Equal(models.ConsoleStatusConnected))
	}
	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		dir = GinkgoT().TempDir()
		pool := store.NewPool(time.Minute)
		DeferCleanup(pool.Close)
		db, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(dir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(db.Migrate(context.Background(), func(ctx context.Context, db *sql.DB) error {
			return migrations.RunMain(ctx, db)
		})).To(Succeed())
		pool.Add(db)
		st, err := db.Store()
		Expect(err).NotTo(HaveOccurred())
		manager := services.NewServiceManager(dir, services.WithPool(pool))
		response.Store(http.StatusOK)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(int(response.Load()))
		}))
		DeferCleanup(server.Close)
		client, err := console.NewConsoleClient(server.URL, "")
		Expect(err).NotTo(HaveOccurred())
		cfg = config.Configuration{Agent: config.Agent{
			ID: uuid.NewString(), SourceID: uuid.NewString(), Version: "v0.31.0",
			UpdateInterval: 20 * time.Millisecond,
		}}
		consoleService, err := services.NewConsoleService(cfg.Agent, client, manager, st)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(consoleService.Stop)
		update, err := services.NewUpdateService(dir, cfg.Agent.Version, consoleService.GetMode)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(update.Stop)
		provider = &updateProvider{update: update, console: consoleService}
	})

	It("returns updateStatus when disconnected or configured to connect but unable to do so", func() {
		for _, rvtools := range []bool{false, true} {
			status, body := getStatus(rvtools)
			Expect(status.Mode).To(Equal(api.AgentStatusModeDisconnected))
			Expect(status.UpdateStatus.CurrentVersion).To(Equal(cfg.Agent.Version))
			Expect(status.UpdateStatus.State).To(Equal(api.Idle))
			Expect(status.UpdateStatus.LastResult).To(BeNil())
			Expect(body).To(ContainSubstring(`"updateStatus"`))
		}
		response.Store(http.StatusServiceUnavailable)
		Expect(provider.console.SetMode(context.Background(), models.AgentModeConnected)).To(Succeed())
		Eventually(func() error { return provider.console.Status().Error }).Should(HaveOccurred())
		status, body := getStatus(false)
		Expect(status.Mode).To(Equal(api.AgentStatusModeConnected))
		Expect(status.ConsoleConnection.Status).To(Equal(api.ConsoleConnectionStatusDisconnected))
		Expect(status.UpdateStatus.CurrentVersion).To(Equal(cfg.Agent.Version))
		Expect(body).To(ContainSubstring(`"updateStatus"`))
	})

	It("keeps the last update result visible before connecting and after connection loss in both UI modes", func() {
		archive, _ := test.UpdateArchive("v0.32.0", "", "")
		_, err := provider.update.Upload(context.Background(), bytes.NewReader(archive), int64(len(archive)), "")
		Expect(err).NotTo(HaveOccurred())
		published, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(published).To(HaveLen(1))
		Expect(os.Remove(published[0])).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "updates", "status.json"), []byte(`{"file":"`+filepath.Base(published[0])+`","result":"rolled-back","time":"2026-11-03T00:00:00Z","error":"validation failed","error_stage":"validation","running_image":{"version":"v0.31.0","digest":"sha256:old"},"last_successful_update":{"version":"v0.31.0","digest":"sha256:old","time":"2026-11-02T00:00:00Z"}}`), 0o600)).To(Succeed())
		for _, rvtools := range []bool{false, true} {
			status, _ := getStatus(rvtools)
			Expect(status.ConsoleConnection.Status).To(Equal(api.ConsoleConnectionStatusDisconnected))
			Expect(status.UpdateStatus.LastResult.Result).To(Equal(api.RolledBack))
			Expect(status.UpdateStatus.LastResult.ErrorStage).To(HaveValue(Equal("validation")))
		}
		connect()
		for _, rvtools := range []bool{false, true} {
			status, body := getStatus(rvtools)
			Expect(status.UpdateStatus.CurrentVersion).To(Equal(cfg.Agent.Version))
			Expect(status.UpdateStatus.State).To(Equal(api.Idle))
			Expect(status.UpdateStatus.LastResult.Result).To(Equal(api.RolledBack))
			Expect(status.UpdateStatus.LastResult.Error).To(HaveValue(Equal("validation failed")))
			Expect(status.UpdateStatus.LastResult.RunningImage.Version).To(Equal("v0.31.0"))
			Expect(status.UpdateStatus.LastResult.LastSuccessfulUpdate.Digest).To(Equal("sha256:old"))
			Expect(body).To(ContainSubstring(`"lastResult"`))
			Expect(body).NotTo(ContainSubstring(`"connected":`))
		}
		response.Store(http.StatusServiceUnavailable)
		Eventually(func() models.ConsoleStatusType {
			return provider.console.Status().Current
		}).Should(Equal(models.ConsoleStatusDisconnected))
		for _, rvtools := range []bool{false, true} {
			status, _ := getStatus(rvtools)
			Expect(status.ConsoleConnection.Status).To(Equal(api.ConsoleConnectionStatusDisconnected))
			Expect(status.UpdateStatus.LastResult.Result).To(Equal(api.RolledBack))
			Expect(status.UpdateStatus.LastResult.Error).To(HaveValue(Equal("validation failed")))
		}
	})

	It("keeps updateStatus present when the update service is unavailable", func() {
		connect()
		provider.update = nil
		status, body := getStatus(false)
		Expect(body).To(ContainSubstring(`"updateStatus"`))
		Expect(status.UpdateStatus.CurrentVersion).To(Equal(cfg.Agent.Version))
		Expect(status.UpdateStatus.State).To(Equal(api.Unknown))
		Expect(status.UpdateStatus.LastError).To(HaveValue(Equal("update status is unavailable")))
	})

	It("reports an update-status error without failing agent status or exposing internal details", func() {
		Expect(os.MkdirAll(filepath.Join(dir, "updates"), 0o750)).To(Succeed())
		path := filepath.Join(dir, "updates", "status.json")
		Expect(os.WriteFile(path, []byte("bad JSON"), 0o600)).To(Succeed())
		status, body := getStatus(false)
		Expect(status.UpdateStatus.State).To(Equal(api.Unknown))
		Expect(status.UpdateStatus.LastError).To(HaveValue(Equal("update status is unavailable")))
		Expect(body).NotTo(ContainSubstring(dir))
		Expect(os.Remove(path)).To(Succeed())
		status, _ = getStatus(false)
		Expect(status.UpdateStatus.State).To(Equal(api.Idle))
		Expect(status.UpdateStatus.LastError).To(BeNil())
	})
})
