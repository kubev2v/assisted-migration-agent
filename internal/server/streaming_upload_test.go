package server_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	api "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	handlers "github.com/kubev2v/assisted-migration-agent/agent/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/server"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/test"
)

type unreadUploadBody struct{ reads int }

func (r *unreadUploadBody) Read([]byte) (int, error) {
	r.reads++
	return 0, fmt.Errorf("body must not be read before checking service availability")
}

var _ = Describe("Streaming image upgrade upload validation", func() {
	newHandler := func(serviceAvailable bool) http.Handler {
		dataDir := GinkgoT().TempDir()
		imageUpgrade, err := services.NewImageUpgradeService(dataDir, "v0.31.0", func(context.Context) (models.AgentMode, error) {
			return models.AgentModeDisconnected, nil
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(imageUpgrade.Stop)
		manager := services.NewServiceManager(dataDir)
		if serviceAvailable {
			services.WithImageUpgradeService(imageUpgrade)(manager)
		}
		cfg := config.Configuration{}
		handler := handlers.NewHandler(cfg, manager)
		spec, err := api.GetSwagger()
		Expect(err).NotTo(HaveOccurred())
		srv, err := server.NewServer(config.Server{}, map[string]server.APIGroup{
			"/api/v2": {Swagger: spec, StreamingUploadPaths: []string{"/image-upgrades"},
				RegisterFn: func(router *gin.RouterGroup) { api.RegisterHandlers(router, handler) }},
		})
		Expect(err).NotTo(HaveOccurred())
		return server.HandlerForTest(srv)
	}

	It("does not let OpenAPI middleware consume the upload body", func() {
		handler := newHandler(false)
		body := &unreadUploadBody{}
		req := httptest.NewRequest(http.MethodPost, "/api/v2/image-upgrades", nil)
		req.Body = io.NopCloser(body)
		req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		Expect(recorder.Code).To(Equal(http.StatusServiceUnavailable), recorder.Body.String())
		Expect(body.reads).To(BeZero())
	})

	It("streams valid uploads through the real middleware and retains normal JSON validation", func() {
		handler := newHandler(true)
		archive, _ := test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "image.tar")
		Expect(err).NotTo(HaveOccurred())
		_, err = part.Write(archive)
		Expect(err).NotTo(HaveOccurred())
		Expect(writer.Close()).To(Succeed())
		req := httptest.NewRequest(http.MethodPost, "/api/v2/image-upgrades", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		Expect(recorder.Code).To(Equal(http.StatusAccepted), recorder.Body.String())
		req = httptest.NewRequest(http.MethodPost, "/api/v2/agent", strings.NewReader(`{"mode":"invalid"}`))
		req.Header.Set("Content-Type", "application/json")
		recorder = httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	})
})
