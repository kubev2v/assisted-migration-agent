package v2_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	api "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	handlers "github.com/kubev2v/assisted-migration-agent/agent/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/test"
)

type updateProvider struct {
	stubServiceProvider
	update *services.UpdateService
}

func (p *updateProvider) UpdateService() *services.UpdateService { return p.update }

var _ = Describe("Update endpoints", func() {
	var (
		router    *gin.Engine
		dir       string
		connected bool
		provider  *updateProvider
		cfg       config.Configuration
	)
	setup := func(rvtools bool) {
		router = gin.New()
		var handler api.ServerInterface = handlers.NewHandler(cfg, provider)
		if rvtools {
			handler = handlers.NewRVToolsHandler(cfg, provider)
		}
		api.RegisterHandlersWithOptions(router, handler, api.GinServerOptions{BaseURL: "/api/v2"})
	}
	upload := func(field string, data []byte, extra bool) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile(field, "untrusted-name.tar")
		Expect(err).NotTo(HaveOccurred())
		_, err = part.Write(data)
		Expect(err).NotTo(HaveOccurred())
		if extra {
			Expect(writer.WriteField("unexpected", "value")).To(Succeed())
		}
		Expect(writer.Close()).To(Succeed())
		req := httptest.NewRequest(http.MethodPut, "/api/v2/update/image", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}
	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		dir = GinkgoT().TempDir()
		connected = false
		service, err := services.NewUpdateService(dir, "v0.31.0", func(context.Context) (models.AgentMode, error) {
			if connected {
				return models.AgentModeConnected, nil
			}
			return models.AgentModeDisconnected, nil
		}, func() bool { return false })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(service.Stop)
		provider = &updateProvider{update: service}
		cfg = config.Configuration{Agent: config.Agent{UpdateEnabled: true}}
		setup(false)
	})

	It("returns explicit nulls and status through generated routes", func() {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v2/update", nil))
		Expect(recorder.Code).To(Equal(http.StatusOK))
		var status api.UpdateStatus
		Expect(json.Unmarshal(recorder.Body.Bytes(), &status)).To(Succeed())
		Expect(status.State).To(Equal(api.Idle))
		Expect(recorder.Body.String()).To(ContainSubstring(`"lastResult":null`))
		Expect(recorder.Body.String()).To(ContainSubstring(`"autoUpdate":null`))
	})

	It("prepares an image in normal and RVTools modes without triggering installation", func() {
		archive, _ := test.UpdateArchive("v0.32.0", "", "")
		for _, rvtools := range []bool{false, true} {
			setup(rvtools)
			recorder := upload("file", archive, false)
			Expect(recorder.Code).To(Equal(http.StatusOK), recorder.Body.String())
			var status api.UpdateStatus
			Expect(json.Unmarshal(recorder.Body.Bytes(), &status)).To(Succeed())
			Expect(status.State).To(Equal(api.Ready))
			Expect(status.PreparedImage.Version).To(Equal("v0.32.0"))
			triggers, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
			Expect(err).NotTo(HaveOccurred())
			Expect(triggers).To(BeEmpty())
		}
	})

	It("rejects malformed or extra multipart input before committing a replacement", func() {
		archive, _ := test.UpdateArchive("v0.32.0", "", "")
		Expect(upload("file", archive, true).Code).To(Equal(http.StatusBadRequest))
		Expect(upload("other", archive, false).Code).To(Equal(http.StatusBadRequest))
		Expect(upload("file", []byte("invalid OCI"), false).Code).To(Equal(http.StatusBadRequest))
		status, err := provider.update.Status(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(status.PreparedImage).To(BeNil())
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/v2/update/image", strings.NewReader("bad")))
		Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	})

	It("rejects oversized multipart headers", func() {
		Expect(upload(strings.Repeat("x", 2<<20), []byte("test"), false).Code).To(Equal(http.StatusRequestEntityTooLarge))
	})

	It("returns a conflict in connected mode", func() {
		connected = true
		archive, _ := test.UpdateArchive("v0.32.0", "", "")
		Expect(upload("file", archive, false).Code).To(Equal(http.StatusConflict))
	})

	It("keeps both endpoints disabled by default", func() {
		cfg.Agent.UpdateEnabled = false
		setup(false)
		Expect(upload("file", []byte("ignored"), false).Code).To(Equal(http.StatusServiceUnavailable))
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v2/update", nil))
		Expect(recorder.Code).To(Equal(http.StatusServiceUnavailable))
	})

	It("reports corrupt host results as server errors", func() {
		Expect(os.MkdirAll(filepath.Join(dir, "updates"), 0o750)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "updates", "status.json"), []byte("bad JSON"), 0o600)).To(Succeed())
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v2/update", nil))
		Expect(recorder.Code).To(Equal(http.StatusInternalServerError))
		Expect(recorder.Body.String()).To(ContainSubstring("internal server error"))
	})
})
