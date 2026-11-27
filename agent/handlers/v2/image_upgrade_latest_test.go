package v2_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	api "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	handlers "github.com/kubev2v/assisted-migration-agent/agent/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("Latest image upgrade endpoints", func() {
	var (
		router    *gin.Engine
		connected bool
		planner   func(w http.ResponseWriter, r *http.Request)
	)
	request := func(method string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(method, "/api/v2/image-upgrades/latest", nil))
		return recorder
	}
	serveStatus := func(body string) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}
	}
	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		connected = true
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { planner(w, r) }))
		DeferCleanup(server.Close)
		archive, _ := test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
		available := `{"updateAvailable":true,"latestVersion":"v0.32.0","applianceImage":{"supported":true,"imageUrl":"` + server.URL + `/image"}}`
		planner = func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/image" {
				_, _ = w.Write(archive)
				return
			}
			serveStatus(available)(w, r)
		}

		service, err := services.NewImageUpgradeService(GinkgoT().TempDir(), "v0.31.0", func(context.Context) (models.AgentMode, error) {
			if connected {
				return models.AgentModeConnected, nil
			}
			return models.AgentModeDisconnected, nil
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(service.Stop)
		client, err := console.NewConsoleClient(server.URL, "")
		Expect(err).NotTo(HaveOccurred())
		service.WithConsole(client, uuid.New())

		router = gin.New()
		var handler api.ServerInterface = handlers.NewHandler(config.Configuration{}, &imageUpgradeProvider{imageUpgrade: service})
		api.RegisterHandlersWithOptions(router, handler, api.GinServerOptions{BaseURL: "/api/v2"})
	})

	It("returns the console's answer", func() {
		recorder := request(http.MethodGet)
		Expect(recorder.Code).To(Equal(http.StatusOK), recorder.Body.String())
		var latest api.ImageUpgradeLatest
		Expect(json.Unmarshal(recorder.Body.Bytes(), &latest)).To(Succeed())
		version := "v0.32.0"
		Expect(latest).To(Equal(api.ImageUpgradeLatest{UpdateAvailable: true, LatestVersion: &version, Supported: true}))
	})

	It("starts a download", func() {
		Expect(request(http.MethodPost).Code).To(Equal(http.StatusAccepted))
	})

	DescribeTable("maps failures to status codes", func(method string, setup func(), code int) {
		setup()
		Expect(request(method).Code).To(Equal(code))
	},
		Entry("check while disconnected", http.MethodGet, func() { connected = false }, http.StatusConflict),
		Entry("download while disconnected", http.MethodPost, func() { connected = false }, http.StatusConflict),
		Entry("check with console down", http.MethodGet, func() {
			planner = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }
		}, http.StatusServiceUnavailable),
		Entry("download with no update", http.MethodPost, func() {
			planner = serveStatus(`{"updateAvailable":false}`)
		}, http.StatusConflict),
		Entry("download when a new OVA is required", http.MethodPost, func() {
			planner = serveStatus(`{"updateAvailable":true,"applianceImage":{"supported":false,"reason":"new-ova-required"}}`)
		}, http.StatusConflict),
		Entry("download before the console has the image", http.MethodPost, func() {
			planner = serveStatus(`{"updateAvailable":true,"applianceImage":{"supported":true}}`)
		}, http.StatusServiceUnavailable),
	)
})
