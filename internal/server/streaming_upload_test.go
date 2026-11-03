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
	return 0, fmt.Errorf("body must not be read before the disabled-feature check")
}

var _ = Describe("Streaming update upload validation", func() {
	newHandler := func(enabled bool) http.Handler {
		dataDir := GinkgoT().TempDir()
		update, err := services.NewUpdateService(dataDir, "v0.31.0", func(context.Context) (models.AgentMode, error) {
			return models.AgentModeDisconnected, nil
		}, func() bool { return false })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(update.Stop)
		manager := services.NewServiceManager(dataDir, services.WithUpdateService(update))
		cfg := config.Configuration{Agent: config.Agent{UpdateEnabled: enabled}}
		handler := handlers.NewHandler(cfg, manager)
		spec, err := api.GetSwagger()
		Expect(err).NotTo(HaveOccurred())
		srv, err := server.NewServer(config.Server{}, map[string]server.APIGroup{
			"/api/v2": {Swagger: spec, StreamingUploadPaths: []string{"/update/image"},
				RegisterFn: func(router *gin.RouterGroup) { api.RegisterHandlers(router, handler) }},
		})
		Expect(err).NotTo(HaveOccurred())
		return server.HandlerForTest(srv)
	}

	It("does not let OpenAPI middleware consume the upload body", func() {
		handler := newHandler(false)
		body := &unreadUploadBody{}
		req := httptest.NewRequest(http.MethodPut, "/api/v2/update/image", nil)
		req.Body = io.NopCloser(body)
		req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		Expect(recorder.Code).To(Equal(http.StatusServiceUnavailable), recorder.Body.String())
		Expect(body.reads).To(BeZero())
	})

	It("streams valid uploads through the real middleware and retains normal JSON validation", func() {
		handler := newHandler(true)
		archive, _ := test.UpdateArchive("v0.32.0", "", "")
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "image.tar")
		Expect(err).NotTo(HaveOccurred())
		_, err = part.Write(archive)
		Expect(err).NotTo(HaveOccurred())
		Expect(writer.Close()).To(Succeed())
		req := httptest.NewRequest(http.MethodPut, "/api/v2/update/image", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		Expect(recorder.Code).To(Equal(http.StatusOK), recorder.Body.String())
		req = httptest.NewRequest(http.MethodPost, "/api/v2/agent", strings.NewReader(`{"mode":"invalid"}`))
		req.Header.Set("Content-Type", "application/json")
		recorder = httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	})
})
