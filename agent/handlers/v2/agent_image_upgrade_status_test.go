package v2_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
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

var _ = Describe("Agent image upgrade status", func() {
	var (
		dir      string
		cfg      config.Configuration
		provider *imageUpgradeProvider
		response atomic.Int64
	)
	requestStatus := func(request *http.Request) (api.AgentStatus, string) {
		router := gin.New()
		var handler api.ServerInterface = handlers.NewHandler(cfg, provider)
		api.RegisterHandlersWithOptions(router, handler, api.GinServerOptions{BaseURL: "/api/v2"})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		Expect(recorder.Code).To(Equal(http.StatusOK), recorder.Body.String())
		var status api.AgentStatus
		Expect(json.Unmarshal(recorder.Body.Bytes(), &status)).To(Succeed())
		return status, recorder.Body.String()
	}
	getStatus := func() (api.AgentStatus, string) {
		return requestStatus(httptest.NewRequest(http.MethodGet, "/api/v2/agent", nil))
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
		db, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(dir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, store.ReadWriteDatabase)
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
		imageUpgrade, err := services.NewImageUpgradeService(dir, cfg.Agent.Version, consoleService.GetMode)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(imageUpgrade.Stop)
		provider = &imageUpgradeProvider{imageUpgrade: imageUpgrade, console: consoleService}
	})

	It("returns imageUpgradeStatus when disconnected or configured to connect but unable to do so", func() {
		status, body := getStatus()
		Expect(status.Mode).To(Equal(api.AgentStatusModeDisconnected))
		Expect(status.ImageUpgradeStatus.CurrentVersion).To(Equal(cfg.Agent.Version))
		Expect(status.ImageUpgradeStatus.Upload.State).To(Equal(api.ImageUploadStatusStateIdle))
		Expect(status.ImageUpgradeStatus.Upgrade).To(BeNil())
		Expect(body).To(ContainSubstring(`"imageUpgradeStatus"`))
		response.Store(http.StatusServiceUnavailable)
		Expect(provider.console.SetMode(context.Background(), models.AgentModeConnected)).To(Succeed())
		Eventually(func() error { return provider.console.Status().Error }).Should(HaveOccurred())
		status, body = getStatus()
		Expect(status.Mode).To(Equal(api.AgentStatusModeConnected))
		Expect(status.ConsoleConnection.Status).To(Equal(api.ConsoleConnectionStatusDisconnected))
		Expect(status.ImageUpgradeStatus.CurrentVersion).To(Equal(cfg.Agent.Version))
		Expect(body).To(ContainSubstring(`"imageUpgradeStatus"`))
	})

	DescribeTable("returns imageUpgradeStatus after changing agent mode", func(mode api.AgentModeRequestMode, imageUpgradeState string) {
		expected := api.ImageUpgradeStatus{CurrentVersion: cfg.Agent.Version, Upload: api.ImageUploadStatus{State: api.ImageUploadStatusStateIdle}}
		switch imageUpgradeState {
		case "unavailable":
			provider.imageUpgrade = nil
		case "malformed", "host result":
			Expect(os.MkdirAll(filepath.Join(dir, "updates"), 0o750)).To(Succeed())
			data := []byte("bad JSON")
			if imageUpgradeState == "host result" {
				result := models.ImageUpgradeResult{File: "image.tar", Result: "applied", Time: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)}
				var err error
				data, err = json.Marshal(result)
				Expect(err).NotTo(HaveOccurred())
				expected.Upgrade = &api.ImageUpgradeResult{File: result.File, Outcome: api.Applied, Time: result.Time, Error: &result.Error, ErrorStage: &result.ErrorStage}
			}
			Expect(os.WriteFile(filepath.Join(dir, "updates", "status.json"), data, 0o600)).To(Succeed())
		}
		if imageUpgradeState == "unavailable" {
			message := "image upgrade status is unavailable"
			expected.Upload.State = api.ImageUploadStatusStateUnknown
			expected.Upload.LastError = &message
			expected.UpgradeError = &message
		}
		if imageUpgradeState == "malformed" {
			message := "host image upgrade result is unavailable"
			expected.UpgradeError = &message
		}

		data, err := json.Marshal(api.AgentModeRequest{Mode: mode})
		Expect(err).NotTo(HaveOccurred())
		request := httptest.NewRequest(http.MethodPost, "/api/v2/agent", bytes.NewReader(data))
		request.Header.Set("Content-Type", "application/json")
		status, body := requestStatus(request)
		Expect(status.Mode).To(Equal(api.AgentStatusMode(mode)))
		Expect(status.ImageUpgradeStatus).To(Equal(expected))
		Expect(body).NotTo(ContainSubstring(dir))
		getResponse, _ := getStatus()
		Expect(status.ImageUpgradeStatus).To(Equal(getResponse.ImageUpgradeStatus))
	},
		Entry("connected", api.Connected, "idle"),
		Entry("disconnected", api.Disconnected, "idle"),
		Entry("connected without image upgrade service", api.Connected, "unavailable"),
		Entry("disconnected without image upgrade service", api.Disconnected, "unavailable"),
		Entry("connected with unreadable status", api.Connected, "malformed"),
		Entry("disconnected with unreadable status", api.Disconnected, "malformed"),
		Entry("connected with host result", api.Connected, "host result"),
		Entry("disconnected with host result", api.Disconnected, "host result"),
	)

	It("keeps the last image upgrade result visible before connecting and after connection loss", func() {
		archive, _ := test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
		_, err := provider.imageUpgrade.Upload(context.Background(), bytes.NewReader(archive))
		Expect(err).NotTo(HaveOccurred())
		published, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(published).To(HaveLen(1))
		Expect(os.Remove(published[0])).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "updates", "status.json"), []byte(`{"file":"`+filepath.Base(published[0])+`","result":"rolled-back","time":"2026-11-03T00:00:00Z","error":"validation failed","error_stage":"validation","running_image":{"version":"v0.31.0","digest":"sha256:old"},"last_successful_update":{"version":"v0.31.0","digest":"sha256:old","time":"2026-11-02T00:00:00Z"}}`), 0o600)).To(Succeed())
		status, _ := getStatus()
		Expect(status.ConsoleConnection.Status).To(Equal(api.ConsoleConnectionStatusDisconnected))
		Expect(status.ImageUpgradeStatus.Upgrade.Outcome).To(Equal(api.RolledBack))
		Expect(status.ImageUpgradeStatus.Upgrade.ErrorStage).To(HaveValue(Equal("validation")))
		connect()
		status, body := getStatus()
		Expect(status.ImageUpgradeStatus.CurrentVersion).To(Equal(cfg.Agent.Version))
		Expect(status.ImageUpgradeStatus.Upload.State).To(Equal(api.ImageUploadStatusStateIdle))
		Expect(status.ImageUpgradeStatus.Upgrade.Outcome).To(Equal(api.RolledBack))
		Expect(status.ImageUpgradeStatus.Upgrade.Error).To(HaveValue(Equal("validation failed")))
		Expect(status.ImageUpgradeStatus.Upgrade.RunningImage.Version).To(Equal("v0.31.0"))
		Expect(status.ImageUpgradeStatus.Upgrade.LastSuccessfulUpdate.Digest).To(Equal("sha256:old"))
		Expect(body).To(ContainSubstring(`"upgrade"`))
		Expect(body).NotTo(ContainSubstring(`"connected":`))
		response.Store(http.StatusServiceUnavailable)
		Eventually(func() models.ConsoleStatusType {
			return provider.console.Status().Current
		}).Should(Equal(models.ConsoleStatusDisconnected))
		status, _ = getStatus()
		Expect(status.ConsoleConnection.Status).To(Equal(api.ConsoleConnectionStatusDisconnected))
		Expect(status.ImageUpgradeStatus.Upgrade.Outcome).To(Equal(api.RolledBack))
		Expect(status.ImageUpgradeStatus.Upgrade.Error).To(HaveValue(Equal("validation failed")))
	})

	It("keeps imageUpgradeStatus present when the image upgrade service is unavailable", func() {
		connect()
		provider.imageUpgrade = nil
		status, body := getStatus()
		Expect(body).To(ContainSubstring(`"imageUpgradeStatus"`))
		Expect(status.ImageUpgradeStatus.CurrentVersion).To(Equal(cfg.Agent.Version))
		Expect(status.ImageUpgradeStatus.Upload.State).To(Equal(api.ImageUploadStatusStateUnknown))
		Expect(status.ImageUpgradeStatus.Upload.LastError).To(HaveValue(Equal("image upgrade status is unavailable")))
		Expect(status.ImageUpgradeStatus.UpgradeError).To(HaveValue(Equal("image upgrade status is unavailable")))
	})

	DescribeTable("reports an unreadable host result without losing the upload state or exposing internal details", func(kind string) {
		Expect(os.MkdirAll(filepath.Join(dir, "updates"), 0o750)).To(Succeed())
		path := filepath.Join(dir, "updates", "status.json")
		switch kind {
		case "directory":
			Expect(os.Mkdir(path, 0o750)).To(Succeed())
		case "oversized":
			Expect(os.WriteFile(path, bytes.Repeat([]byte("x"), (1<<20)+1), 0o600)).To(Succeed())
		case "invalid schema":
			Expect(os.WriteFile(path, []byte(`{"file":"previous.tar","result":"applied"}`), 0o600)).To(Succeed())
		default:
			Expect(os.WriteFile(path, []byte("bad JSON"), 0o600)).To(Succeed())
		}
		status, body := getStatus()
		Expect(status.ImageUpgradeStatus.CurrentVersion).To(Equal(cfg.Agent.Version))
		Expect(status.ImageUpgradeStatus.Upload.State).To(Equal(api.ImageUploadStatusStateIdle))
		Expect(status.ImageUpgradeStatus.Upgrade).To(BeNil())
		Expect(status.ImageUpgradeStatus.Upload.LastError).To(BeNil())
		Expect(status.ImageUpgradeStatus.UpgradeError).To(HaveValue(Equal("host image upgrade result is unavailable")))
		Expect(body).NotTo(ContainSubstring(dir))
		Expect(os.Remove(path)).To(Succeed())
		status, _ = getStatus()
		Expect(status.ImageUpgradeStatus.Upload.State).To(Equal(api.ImageUploadStatusStateIdle))
		Expect(status.ImageUpgradeStatus.Upload.LastError).To(BeNil())
		Expect(status.ImageUpgradeStatus.UpgradeError).To(BeNil())
	},
		Entry("invalid JSON", "invalid JSON"),
		Entry("read failure", "directory"),
		Entry("oversized result", "oversized"),
		Entry("invalid result schema", "invalid schema"),
	)

	DescribeTable("preserves an active upload when the host result is corrupt and recovers on the next read", func(method string) {
		reader, writer := io.Pipe()
		DeferCleanup(func() { _ = reader.Close(); _ = writer.Close() })
		done := make(chan error, 1)
		go func() {
			_, err := provider.imageUpgrade.Upload(context.Background(), reader)
			done <- err
		}()
		Eventually(func() string {
			status, err := provider.imageUpgrade.Status(context.Background())
			Expect(err).NotTo(HaveOccurred())
			return status.Upload.State
		}).Should(Equal("uploading"))
		path := filepath.Join(dir, "updates", "status.json")
		Expect(os.MkdirAll(filepath.Dir(path), 0o750)).To(Succeed())
		Expect(os.WriteFile(path, []byte("bad JSON"), 0o600)).To(Succeed())

		request := httptest.NewRequest(method, "/api/v2/agent", nil)
		if method == http.MethodPost {
			request = httptest.NewRequest(method, "/api/v2/agent", bytes.NewBufferString(`{"mode":"disconnected"}`))
			request.Header.Set("Content-Type", "application/json")
		}
		status, body := requestStatus(request)
		Expect(status.ImageUpgradeStatus.CurrentVersion).To(Equal(cfg.Agent.Version))
		Expect(status.ImageUpgradeStatus.Upload.State).To(Equal(api.ImageUploadStatusStateUploading))
		Expect(status.ImageUpgradeStatus.Upgrade).To(BeNil())
		Expect(status.ImageUpgradeStatus.Upload.LastError).To(BeNil())
		Expect(status.ImageUpgradeStatus.UpgradeError).To(HaveValue(Equal("host image upgrade result is unavailable")))
		Expect(body).To(ContainSubstring(`"upgrade":null`))
		Expect(body).NotTo(ContainSubstring(dir))

		Expect(os.WriteFile(path, []byte(`{"file":"previous.tar","result":"applied","time":"2026-11-03T00:00:00Z"}`), 0o600)).To(Succeed())
		status, _ = getStatus()
		Expect(status.ImageUpgradeStatus.Upload.State).To(Equal(api.ImageUploadStatusStateUploading))
		Expect(status.ImageUpgradeStatus.Upload.LastError).To(BeNil())
		Expect(status.ImageUpgradeStatus.UpgradeError).To(BeNil())
		Expect(status.ImageUpgradeStatus.Upgrade).NotTo(BeNil())
		Expect(status.ImageUpgradeStatus.Upgrade.Outcome).To(Equal(api.Applied))

		archive, _ := test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
		_, err := writer.Write(archive)
		Expect(err).NotTo(HaveOccurred())
		Expect(writer.Close()).To(Succeed())
		Eventually(done).Should(Receive(BeNil()))
		status, _ = getStatus()
		Expect(status.ImageUpgradeStatus.Upload.State).To(Equal(api.ImageUploadStatusStateIdle))
		Expect(status.ImageUpgradeStatus.Upload.LastError).To(BeNil())
	},
		Entry("GET /agent", http.MethodGet),
		Entry("POST /agent", http.MethodPost),
	)

	It("preserves an upload error when the host result also cannot be read", func() {
		_, err := provider.imageUpgrade.Upload(context.Background(), bytes.NewBufferString("invalid OCI"))
		Expect(err).To(HaveOccurred())
		uploadError := err.Error()
		Expect(os.WriteFile(filepath.Join(dir, "updates", "status.json"), []byte("bad JSON"), 0o600)).To(Succeed())
		status, _ := getStatus()
		Expect(status.ImageUpgradeStatus.Upload.State).To(Equal(api.ImageUploadStatusStateIdle))
		Expect(status.ImageUpgradeStatus.Upgrade).To(BeNil())
		Expect(status.ImageUpgradeStatus.Upload.LastError).To(HaveValue(Equal(uploadError)))
		Expect(status.ImageUpgradeStatus.UpgradeError).To(HaveValue(Equal("host image upgrade result is unavailable")))
	})

	It("keeps the unknown fallback when a cancelled context prevents reading the upload snapshot", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		request := httptest.NewRequest(http.MethodGet, "/api/v2/agent", nil).WithContext(ctx)
		status, _ := requestStatus(request)
		Expect(status.ImageUpgradeStatus.CurrentVersion).To(Equal(cfg.Agent.Version))
		Expect(status.ImageUpgradeStatus.Upload.State).To(Equal(api.ImageUploadStatusStateUnknown))
		Expect(status.ImageUpgradeStatus.Upload.LastError).To(HaveValue(Equal("image upgrade status is unavailable")))
	})
})
