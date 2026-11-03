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
	"time"

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
	update  *services.UpdateService
	console *services.Console
}

func (p *updateProvider) UpdateService() *services.UpdateService { return p.update }

func (p *updateProvider) ConsoleService() *services.Console { return p.console }

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
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(service.Stop)
		provider = &updateProvider{update: service}
		cfg = config.Configuration{}
		setup(false)
	})

	It("does not register separate update status or apply routes", func() {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(method, "/api/v2/update", nil))
			Expect(recorder.Code).To(Equal(http.StatusNotFound))
		}
	})

	It("publishes a successful upload and returns accepted in normal and RVTools modes", func() {
		archive, _ := test.UpdateArchive("v0.32.0", "", "")
		for _, rvtools := range []bool{false, true} {
			setup(rvtools)
			recorder := upload("file", archive, false)
			Expect(recorder.Code).To(Equal(http.StatusAccepted), recorder.Body.String())
			Expect(recorder.Body.Len()).To(BeZero())
			triggers, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
			Expect(err).NotTo(HaveOccurred())
			Expect(triggers).To(HaveLen(1))
			published, err := os.ReadFile(triggers[0])
			Expect(err).NotTo(HaveOccurred())
			Expect(published).To(Equal(archive))
			status, err := provider.update.Status(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(status.State).To(Equal("idle"))
			result := models.UpdateResult{File: filepath.Base(triggers[0]), Result: "failed", Time: time.Now().UTC()}
			data, err := json.Marshal(result)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(dir, "updates", "status.json"), data, 0o600)).To(Succeed())
			Expect(os.Remove(triggers[0])).To(Succeed())
		}
	})

	It("rejects malformed or extra multipart input before committing a replacement", func() {
		archive, _ := test.UpdateArchive("v0.32.0", "", "")
		Expect(upload("file", archive, true).Code).To(Equal(http.StatusBadRequest))
		Expect(upload("other", archive, false).Code).To(Equal(http.StatusBadRequest))
		Expect(upload("file", []byte("invalid OCI"), false).Code).To(Equal(http.StatusBadRequest))
		status, err := provider.update.Status(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(status.State).To(Equal("idle"))
		Expect(status.LastError).NotTo(BeNil())
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/v2/update/image", strings.NewReader("bad")))
		Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		triggers, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(triggers).To(BeEmpty())
	})

	It("rejects oversized multipart headers", func() {
		Expect(upload(strings.Repeat("x", 2<<20), []byte("test"), false).Code).To(Equal(http.StatusRequestEntityTooLarge))
	})

	It("does not publish an image when the multipart upload is truncated", func() {
		archive, _ := test.UpdateArchive("v0.32.0", "", "")
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "image.tar")
		Expect(err).NotTo(HaveOccurred())
		_, err = part.Write(archive)
		Expect(err).NotTo(HaveOccurred())
		Expect(writer.Close()).To(Succeed())
		req := httptest.NewRequest(http.MethodPut, "/api/v2/update/image", bytes.NewReader(body.Bytes()[:body.Len()-10]))
		req.Header.Set("Content-Type", writer.FormDataContentType())
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		Expect(recorder.Code).To(Equal(http.StatusBadRequest), recorder.Body.String())
		triggers, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(triggers).To(BeEmpty())
	})

	It("returns a conflict in connected mode", func() {
		connected = true
		archive, _ := test.UpdateArchive("v0.32.0", "", "")
		Expect(upload("file", archive, false).Code).To(Equal(http.StatusConflict))
	})

	It("reports an unavailable update service", func() {
		provider.update = nil
		setup(false)
		Expect(upload("file", []byte("ignored"), false).Code).To(Equal(http.StatusServiceUnavailable))
	})
})
