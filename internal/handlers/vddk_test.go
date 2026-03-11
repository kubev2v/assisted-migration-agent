package handlers_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/handlers"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("VDDK Handlers", func() {
	var (
		mockVddk      *MockVddkService
		mockInspector *MockInspectorService
		handler       *handlers.Handler
		router        *gin.Engine
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		mockVddk = &MockVddkService{}
		mockInspector = &MockInspectorService{}
		handler = handlers.NewHandler(config.Configuration{}).
			WithVddkService(mockVddk).
			WithInspectorService(mockInspector)
		router = gin.New()
		wrapper := v1.ServerInterfaceWrapper{
			Handler:      handler,
			ErrorHandler: func(c *gin.Context, err error, statusCode int) { c.JSON(statusCode, gin.H{"msg": err.Error()}) },
		}
		router.GET("/vddk", wrapper.GetVddkStatus)
		router.POST("/vddk", wrapper.PostVddk)
	})

	Context("GetVddkStatus", func() {
		It("should return 200 and vddk properties when vddk is present", func() {
			mockVddk.StatusResult = &models.VddkStatus{
				Version: "8.0.3",
				Bytes:   1024,
				Md5:     "d41d8cd98f00b204e9800998ecf8427e",
			}

			req := httptest.NewRequest(http.MethodGet, "/vddk", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Header().Get("Content-Type")).To(Equal("application/json; charset=utf-8"))

			var result v1.VddkProperties
			Expect(json.Unmarshal(w.Body.Bytes(), &result)).To(Succeed())
			Expect(result.Version).To(Equal("8.0.3"))
			Expect(result.Bytes).To(Equal(int64(1024)))
			Expect(result.Md5).To(Equal("d41d8cd98f00b204e9800998ecf8427e"))
			Expect(mockVddk.StatusCount).To(Equal(1))
		})

		It("should return 404 when vddk not found", func() {
			mockVddk.StatusError = srvErrors.NewVddkNotFoundError()

			req := httptest.NewRequest(http.MethodGet, "/vddk", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusNotFound))
			var response map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response["error"]).To(ContainSubstring("vddk not found"))
		})

		It("should return 500 for other errors", func() {
			mockVddk.StatusError = http.ErrNotSupported

			req := httptest.NewRequest(http.MethodGet, "/vddk", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Context("PostVddk", func() {
		buildMultipartRequest := func(filename string, body []byte) *http.Request {
			var buf bytes.Buffer
			w := multipart.NewWriter(&buf)
			part, err := w.CreateFormFile("file", filename)
			Expect(err).NotTo(HaveOccurred())
			_, err = part.Write(body)
			Expect(err).NotTo(HaveOccurred())
			Expect(w.Close()).To(Succeed())

			req := httptest.NewRequest(http.MethodPost, "/vddk", &buf)
			req.Header.Set("Content-Type", w.FormDataContentType())
			return req
		}

		It("should return 200 and vddk properties on successful upload", func() {
			mockVddk.UploadResult = &models.VddkStatus{
				Version: "8.0.3",
				Bytes:   256,
				Md5:     "abc123",
			}

			req := buildMultipartRequest("VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", []byte("fake-tar-gz-content"))
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var result v1.VddkProperties
			Expect(json.Unmarshal(w.Body.Bytes(), &result)).To(Succeed())
			Expect(result.Version).To(Equal("8.0.3"))
			Expect(result.Bytes).To(Equal(int64(256)))
			Expect(result.Md5).To(Equal("abc123"))
			Expect(mockVddk.UploadCount).To(Equal(1))
		})

		It("should return 400 when inspector is busy", func() {
			mockInspector.IsBusyResult = true

			req := buildMultipartRequest("VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", []byte("content"))
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var response map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response["error"]).To(ContainSubstring("inspector is running"))
			Expect(mockVddk.UploadCount).To(Equal(0))
		})

		It("should return 409 when vddk upload already in progress", func() {
			mockVddk.UploadError = srvErrors.NewVddkUploadInProgressError()

			req := buildMultipartRequest("VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", []byte("content"))
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusConflict))
			var response map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response["error"]).To(ContainSubstring("already in progress"))
		})

		It("should return 500 when upload fails", func() {
			mockVddk.UploadError = http.ErrAbortHandler

			req := buildMultipartRequest("VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", []byte("content"))
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})

		It("should return 500 when no file is in the request", func() {
			req := httptest.NewRequest(http.MethodPost, "/vddk", bytes.NewReader(nil))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})

		It("should return 413 when request body exceeds max VDDK file size", func() {
			var buf bytes.Buffer
			w := multipart.NewWriter(&buf)
			part, err := w.CreateFormFile("file", "VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz")
			Expect(err).NotTo(HaveOccurred())
			_, err = part.Write(make([]byte, handlers.MaxVDDKSize+1)) // write a multipart body larger than MaxVDDKSize
			Expect(err).NotTo(HaveOccurred())
			Expect(w.Close()).To(Succeed())

			req := httptest.NewRequest(http.MethodPost, "/vddk", &buf)
			req.Header.Set("Content-Type", w.FormDataContentType())
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusRequestEntityTooLarge))
			var response map[string]any
			Expect(json.Unmarshal(rec.Body.Bytes(), &response)).To(Succeed())
			Expect(response["error"]).To(ContainSubstring("request body too large"))
			Expect(mockVddk.UploadCount).To(Equal(0))
		})
	})
})
