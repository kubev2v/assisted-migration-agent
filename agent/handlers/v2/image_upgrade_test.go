package v2_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

type imageUpgradeProvider struct {
	stubServiceProvider
	imageUpgrade *services.ImageUpgradeService
	console      *services.Console
}

func (p *imageUpgradeProvider) ImageUpgradeService() *services.ImageUpgradeService {
	return p.imageUpgrade
}

func (p *imageUpgradeProvider) ConsoleService() *services.Console { return p.console }

var _ = Describe("Image upgrade endpoints", func() {
	var (
		router    *gin.Engine
		dir       string
		connected bool
		provider  *imageUpgradeProvider
		cfg       config.Configuration
	)
	setup := func() {
		router = gin.New()
		var handler api.ServerInterface = handlers.NewHandler(cfg, provider)
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
		req := httptest.NewRequest(http.MethodPost, "/api/v2/image-upgrades", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}
	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		dir = GinkgoT().TempDir()
		connected = false
		service, err := services.NewImageUpgradeService(dir, "v0.31.0", func(context.Context) (models.AgentMode, error) {
			if connected {
				return models.AgentModeConnected, nil
			}
			return models.AgentModeDisconnected, nil
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(service.Stop)
		provider = &imageUpgradeProvider{imageUpgrade: service}
		cfg = config.Configuration{}
		setup()
	})

	It("does not register separate image upgrade status or apply routes", func() {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(method, "/api/v2/image-upgrade", nil))
			Expect(recorder.Code).To(Equal(http.StatusNotFound))
		}
	})

	DescribeTable("rejects obsolete upload routes and methods", func(method, path string) {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
		Expect(recorder.Code).To(Equal(http.StatusNotFound))
	},
		Entry("PUT on the current route", http.MethodPut, "/api/v2/image-upgrades"),
		Entry("PUT on the previous route", http.MethodPut, "/api/v2/image-upgrade/image"),
		Entry("POST on the previous route", http.MethodPost, "/api/v2/image-upgrade/image"),
	)

	It("publishes a successful upload and returns accepted", func() {
		archive, _ := test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
		setup()
		recorder := upload("file", archive, false)
		Expect(recorder.Code).To(Equal(http.StatusAccepted), recorder.Body.String())
		Expect(recorder.Body.Len()).To(BeZero())
		triggers, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(triggers).To(HaveLen(1))
		published, err := os.ReadFile(triggers[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(published).To(Equal(archive))
		status, err := provider.imageUpgrade.Status(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Upload.State).To(Equal("idle"))
		result := models.ImageUpgradeResult{File: filepath.Base(triggers[0]), Result: "failed", Time: time.Now().UTC()}
		data, err := json.Marshal(result)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(dir, "updates", "status.json"), data, 0o600)).To(Succeed())
		Expect(os.Remove(triggers[0])).To(Succeed())
	})

	It("returns a conflict while a published archive is pending and accepts a retry after removal", func() {
		archive, _ := test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
		Expect(upload("file", archive, false).Code).To(Equal(http.StatusAccepted))
		pending, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(pending).To(HaveLen(1))
		recorder := upload("file", archive, false)
		Expect(recorder.Code).To(Equal(http.StatusConflict), recorder.Body.String())
		published, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(published).To(Equal(pending))
		data, err := os.ReadFile(pending[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(Equal(archive))
		Expect(os.Remove(pending[0])).To(Succeed())
		Expect(upload("file", archive, false).Code).To(Equal(http.StatusAccepted))
	})

	It("rejects malformed or extra multipart input before committing a replacement", func() {
		archive, _ := test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
		Expect(upload("file", archive, true).Code).To(Equal(http.StatusBadRequest))
		Expect(upload("other", archive, false).Code).To(Equal(http.StatusBadRequest))
		Expect(upload("file", []byte("invalid OCI"), false).Code).To(Equal(http.StatusBadRequest))
		status, err := provider.imageUpgrade.Status(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Upload.State).To(Equal("idle"))
		Expect(status.Upload.LastError).NotTo(BeNil())
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v2/image-upgrades", strings.NewReader("bad")))
		Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		triggers, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(triggers).To(BeEmpty())
	})

	DescribeTable("publishes images with compatible minimum versions from another channel", func(minimum string) {
		archive, _ := test.ImageUpgradeArchive("v0.32.0", minimum, "")
		recorder := upload("file", archive, false)
		Expect(recorder.Code).To(Equal(http.StatusAccepted), recorder.Body.String())
		triggers, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(triggers).To(HaveLen(1))
		published, err := os.ReadFile(triggers[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(published).To(Equal(archive))
	},
		Entry("older main release", "main-release-0.30-212e974"),
		Entry("older stable release", "stable-release-0.30-212e974"),
		Entry("equal main release", "main-release-0.31-212e974"),
		Entry("equal stable release", "stable-release-0.31-212e974"),
	)

	DescribeTable("rejects incompatible minimum versions before publishing", func(minimum string) {
		archive, _ := test.ImageUpgradeArchive("v0.32.0", minimum, "")
		recorder := upload("file", archive, false)
		Expect(recorder.Code).To(Equal(http.StatusBadRequest), recorder.Body.String())
		triggers, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(triggers).To(BeEmpty())
	},
		Entry("missing minimum", ""),
		Entry("unknown minimum", "unknown"),
		Entry("legacy minimum", "v0.0.0"),
		Entry("PR minimum", "on-pr-212e974"),
		Entry("running version below main minimum", "main-release-0.32-212e974"),
		Entry("running version below stable minimum", "stable-release-0.32-212e974"),
		Entry("running version below minimum", "v0.32.0"),
	)

	It("rejects oversized multipart headers", func() {
		Expect(upload(strings.Repeat("x", 2<<20), []byte("test"), false).Code).To(Equal(http.StatusRequestEntityTooLarge))
	})

	It("does not publish an image when the multipart upload is truncated", func() {
		archive, _ := test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "image.tar")
		Expect(err).NotTo(HaveOccurred())
		_, err = part.Write(archive)
		Expect(err).NotTo(HaveOccurred())
		Expect(writer.Close()).To(Succeed())
		req := httptest.NewRequest(http.MethodPost, "/api/v2/image-upgrades", bytes.NewReader(body.Bytes()[:body.Len()-10]))
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
		archive, _ := test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
		Expect(upload("file", archive, false).Code).To(Equal(http.StatusConflict))
	})

	It("reports an unavailable image upgrade service", func() {
		provider.imageUpgrade = nil
		setup()
		Expect(upload("file", []byte("ignored"), false).Code).To(Equal(http.StatusServiceUnavailable))
	})

	It("returns service unavailable when the image upgrade service has stopped", func() {
		provider.imageUpgrade.Stop()
		archive, _ := test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
		recorder := upload("file", archive, false)
		Expect(recorder.Code).To(Equal(http.StatusServiceUnavailable), recorder.Body.String())
		Expect(recorder.Body.String()).To(ContainSubstring("image upload was interrupted"))
	})

	DescribeTable("reports interrupted multipart reads without publishing an image", func(headers bool, interruption error) {
		archive, _ := test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "image.tar")
		Expect(err).NotTo(HaveOccurred())
		_, err = part.Write(archive)
		Expect(err).NotTo(HaveOccurred())
		Expect(writer.Close()).To(Succeed())
		cut := body.Len() / 2
		if headers {
			cut = 1
		}
		reader := &interruptedImageUpgradeBody{
			Reader: bytes.NewReader(body.Bytes()[:cut]),
			err:    fmt.Errorf("reading request body: %w", interruption),
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v2/image-upgrades", reader)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		Expect(recorder.Code).To(Equal(http.StatusServiceUnavailable), recorder.Body.String())
		Expect(recorder.Body.String()).To(ContainSubstring("image upload was interrupted"))
		published, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(published).To(BeEmpty())
		staged, err := filepath.Glob(filepath.Join(dir, "updates", ".staging", "*"))
		Expect(err).NotTo(HaveOccurred())
		Expect(staged).To(BeEmpty())
		status, err := provider.imageUpgrade.Status(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Upload.State).To(Equal("idle"))
	},
		Entry("cancelled during headers", true, context.Canceled),
		Entry("deadline during headers", true, context.DeadlineExceeded),
		Entry("cancelled during file data", false, context.Canceled),
		Entry("deadline during file data", false, context.DeadlineExceeded),
	)
})

type interruptedImageUpgradeBody struct {
	io.Reader
	err error
}

func (r *interruptedImageUpgradeBody) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		return n, r.err
	}
	return n, err
}
