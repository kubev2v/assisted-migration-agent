package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/handlers"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("Inspector Handler", func() {
	var (
		mockInspector *MockInspectorService
		handler       *handlers.Handler
		router        *gin.Engine
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		mockInspector = &MockInspectorService{}
		handler = handlers.NewHandler(config.Configuration{}).WithInspectorService(mockInspector)
		router = gin.New()
		router.GET("/inspector", handler.GetInspectorStatus)
		router.POST("/inspector", handler.StartInspection)
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

		It("should return 400 for missing credentials", func() {
			reqBody := `{"VcenterCredentials":{"url":"","username":"","password":""},"vmIds":["vm-1"]}`
			req := httptest.NewRequest(http.MethodPost, "/inspector", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(ContainSubstring("Url is required"))
		})

		It("should return 400 for empty VM list", func() {
			reqBody := `{"VcenterCredentials":{"url":"https://test","username":"user","password":"pass"},"vmIds":[]}`
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
			body := `{"VcenterCredentials":{"url":"https://test","username":"user","password":"pass"},"vmIds":["vm-1"]}`
			req := httptest.NewRequest(http.MethodPost, "/inspector", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusAccepted))
			var response v1.InspectorStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.State).To(Equal(v1.InspectorStatusStateInitiating))
		})

		It("should return 500 when start fails", func() {
			mockInspector.StartError = errors.New("start failed")
			reqBody := `{"VcenterCredentials":{"url":"https://test","username":"user","password":"pass"},"vmIds":["vm-1"]}`
			req := httptest.NewRequest(http.MethodPost, "/inspector", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(Equal("failed to start inspector: start failed"))
		})
	})

	Context("StopInspection", func() {
		It("should stop inspector successfully", func() {
			mockInspector.GetStatusResult = models.InspectorStatus{
				State: models.InspectorStateCanceled,
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
