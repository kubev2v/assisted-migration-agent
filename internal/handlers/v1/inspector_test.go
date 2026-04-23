package v1_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	handlers "github.com/kubev2v/assisted-migration-agent/internal/handlers/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("Inspector Handler", func() {
	var (
		mockInspector *MockInspectorService
		mockVddk      *MockVddkService
		handler       *handlers.Handler
		router        *gin.Engine
		apiWrapper    v1.ServerInterfaceWrapper
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		mockInspector = &MockInspectorService{}
		mockVddk = &MockVddkService{}
		handler = handlers.NewHandler(config.Configuration{}).
			WithInspectorService(mockInspector).
			WithVddkService(mockVddk)
		apiWrapper = v1.ServerInterfaceWrapper{
			Handler: handler,
			ErrorHandler: func(c *gin.Context, err error, statusCode int) {
				c.JSON(statusCode, gin.H{"msg": err.Error()})
			},
		}
		router = gin.New()
		router.GET("/inspector", apiWrapper.GetInspectorStatus)
		router.POST("/inspector", handler.StartInspection)
		router.PUT("/inspector/credentials", handler.PutInspectorCredentials)
		router.DELETE("/inspector", handler.StopInspection)
	})

	Context("GetInspectorStatus", func() {
		It("should return status", func() {
			mockInspector.GetStatusResult = models.InspectorStatus{
				State: models.InspectorStateReady,
			}

			req := httptest.NewRequest(http.MethodGet, "/inspector", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var response v1.InspectorStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.State).To(Equal(v1.InspectorStatusStateReady))
		})

		It("should include vddk when includeVddk=true", func() {
			mockInspector.GetStatusResult = models.InspectorStatus{State: models.InspectorStateReady}
			mockVddk.StatusResult = &models.VddkStatus{Version: "8.0.3", Md5: "deadbeef"}

			req := httptest.NewRequest(http.MethodGet, "/inspector?includeVddk=true", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var response v1.InspectorStatus
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Vddk).NotTo(BeNil())
			Expect(response.Vddk.Version).To(Equal("8.0.3"))
			Expect(response.Vddk.Md5).To(Equal("deadbeef"))
		})
	})

	Context("StartInspection", func() {
		It("should return 400 for invalid request body", func() {
			req := httptest.NewRequest(http.MethodPost, "/inspector", strings.NewReader("invalid json"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(Equal("invalid request body"))
		})

		It("should return 400 for empty VM list", func() {
			reqBody := `{"vmIds":[],"credentials":{"url":"https://vc/sdk","username":"u","password":"p"}}`
			req := httptest.NewRequest(http.MethodPost, "/inspector", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(Equal("vmIds is required"))
		})

		It("should start inspection successfully", func() {
			body := `{"vmIds":["vm-1"],"credentials":{"url":"https://vc/sdk","username":"u","password":"p"}}`
			req := httptest.NewRequest(http.MethodPost, "/inspector", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusAccepted))
			var response v1.InspectorStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.State).To(Equal(v1.InspectorStatusStateRunning))
		})

		It("should return 500 when start fails", func() {
			mockInspector.StartError = errors.New("start failed")
			reqBody := `{"vmIds":["vm-1"],"credentials":{"url":"https://vc/sdk","username":"u","password":"p"}}`
			req := httptest.NewRequest(http.MethodPost, "/inspector", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(Equal("failed to start inspector: start failed"))
		})

		It("should return 400 when inspection limit is reached", func() {
			mockInspector.StartError = srvErrors.NewInspectionLimitReachedError(10)
			reqBody := `{"vmIds":["vm-1","vm-2","vm-3"],"credentials":{"url":"https://vc/sdk","username":"u","password":"p"}}`
			req := httptest.NewRequest(http.MethodPost, "/inspector", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(Equal(srvErrors.NewInspectionLimitReachedError(10).Error()))
		})
	})

	Context("SetInspectorCredentials", func() {
		It("should return 400 for invalid request body", func() {
			req := httptest.NewRequest(http.MethodPut, "/inspector/credentials", strings.NewReader("invalid json"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(Equal("invalid request body"))
		})

		It("should return 400 when vCenter rejects credentials", func() {
			mockInspector.CredentialsError = srvErrors.NewVCenterError(errors.New("login failed"))
			reqBody := `{"url":"https://vcenter.example/sdk","username":"u","password":"p"}`
			req := httptest.NewRequest(http.MethodPut, "/inspector/credentials", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(ContainSubstring("login failed"))
		})

		It("should return 200 on success", func() {
			reqBody := `{"url":"https://vcenter.example/sdk","username":"u","password":"p"}`
			req := httptest.NewRequest(http.MethodPut, "/inspector/credentials", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
		})
	})

	Context("StopInspection", func() {
		It("should stop inspector successfully", func() {
			mockInspector.GetStatusResult = models.InspectorStatus{
				State: models.InspectorStateReady,
			}
			req := httptest.NewRequest(http.MethodDelete, "/inspector", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusAccepted))
		})

		It("should return 404 when inspector not running", func() {
			mockInspector.StopError = srvErrors.NewInspectorNotRunningError()

			req := httptest.NewRequest(http.MethodDelete, "/inspector", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusNotFound))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(Equal("inspector not running"))
		})
	})
})

var _ = Describe("VDDK", func() {
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
		router.GET("/inspector/vddk", wrapper.GetInspectorVddkStatus)
		router.PUT("/inspector/vddk", wrapper.PutInspectorVddk)
	})

	Context("GetVddkStatus", func() {
		It("should return 200 and vddk properties when vddk is present", func() {
			mockVddk.StatusResult = &models.VddkStatus{
				Version: "8.0.3",
				Md5:     "d41d8cd98f00b204e9800998ecf8427e",
			}

			req := httptest.NewRequest(http.MethodGet, "/inspector/vddk", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Header().Get("Content-Type")).To(Equal("application/json; charset=utf-8"))

			var result v1.VddkProperties
			Expect(json.Unmarshal(w.Body.Bytes(), &result)).To(Succeed())
			Expect(result.Version).To(Equal("8.0.3"))
			Expect(result.Md5).To(Equal("d41d8cd98f00b204e9800998ecf8427e"))
			Expect(mockVddk.StatusCount).To(Equal(1))
		})

		It("should return 404 when vddk not found", func() {
			mockVddk.StatusError = srvErrors.NewVddkNotFoundError()

			req := httptest.NewRequest(http.MethodGet, "/inspector/vddk", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusNotFound))
			var response map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response["error"]).To(ContainSubstring("vddk not found"))
		})

		It("should return 500 for other errors", func() {
			mockVddk.StatusError = http.ErrNotSupported

			req := httptest.NewRequest(http.MethodGet, "/inspector/vddk", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Context("PutInspectorVddk", func() {
		buildMultipartRequest := func(filename string, body []byte) *http.Request {
			var buf bytes.Buffer
			w := multipart.NewWriter(&buf)
			part, err := w.CreateFormFile("file", filename)
			Expect(err).NotTo(HaveOccurred())
			_, err = part.Write(body)
			Expect(err).NotTo(HaveOccurred())
			Expect(w.Close()).To(Succeed())

			req := httptest.NewRequest(http.MethodPut, "/inspector/vddk", &buf)
			req.Header.Set("Content-Type", w.FormDataContentType())
			return req
		}

		It("should return 200 and vddk properties on successful upload", func() {
			mockVddk.UploadResult = &models.VddkStatus{
				Version: "8.0.3",
				Md5:     "abc123",
			}

			req := buildMultipartRequest("VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", []byte("fake-tar-gz-content"))
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var result v1.VddkProperties
			Expect(json.Unmarshal(w.Body.Bytes(), &result)).To(Succeed())
			Expect(result.Version).To(Equal("8.0.3"))
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
			req := httptest.NewRequest(http.MethodPut, "/inspector/vddk", bytes.NewReader(nil))
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
			_, err = part.Write(make([]byte, handlers.MaxVDDKSize+1))
			Expect(err).NotTo(HaveOccurred())
			Expect(w.Close()).To(Succeed())

			req := httptest.NewRequest(http.MethodPut, "/inspector/vddk", &buf)
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
