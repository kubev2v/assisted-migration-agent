package handlers_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/handlers"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("Collector Handlers", func() {
	var (
		mockCollector *MockCollectorService
		handler       *handlers.Handler
		router        *gin.Engine
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		mockCollector = &MockCollectorService{
			StatusResult: models.CollectorStatus{State: models.CollectorStateReady},
		}
		handler = handlers.New("", nil, mockCollector, nil, nil)
		router = gin.New()
		router.GET("/collector", handler.GetCollectorStatus)
		router.POST("/collector", handler.StartCollector)
		router.DELETE("/collector", handler.StopCollector)
	})

	Describe("GetCollectorStatus", func() {
		It("should return ready status", func() {
			req := httptest.NewRequest(http.MethodGet, "/collector", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.CollectorStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Status).To(Equal(v1.CollectorStatusStatusReady))
		})

		It("should return collected status", func() {
			mockCollector.StatusResult = models.CollectorStatus{State: models.CollectorStateCollected}

			req := httptest.NewRequest(http.MethodGet, "/collector", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.CollectorStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Status).To(Equal(v1.CollectorStatusStatusCollected))
		})

		It("should return error status with message", func() {
			mockCollector.StatusResult = models.CollectorStatus{
				State: models.CollectorStateError,
				Error: errors.New("connection failed"),
			}

			req := httptest.NewRequest(http.MethodGet, "/collector", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.CollectorStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Status).To(Equal(v1.CollectorStatusStatusError))
			Expect(response.Error).NotTo(BeNil())
			Expect(*response.Error).To(Equal("connection failed"))
		})
	})

	Describe("StartCollector", func() {
		It("should return 400 for invalid JSON body", func() {
			req := httptest.NewRequest(http.MethodPost, "/collector", bytes.NewReader([]byte("invalid json")))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("invalid request body"))
		})

		It("should return 400 when url is missing", func() {
			body := v1.CollectorStartRequest{
				Username: "admin",
				Password: "secret",
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/collector", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("url, username, and password are required"))
		})

		It("should return 400 when username is missing", func() {
			body := v1.CollectorStartRequest{
				Url:      "https://vcenter.example.com",
				Password: "secret",
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/collector", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return 400 when password is missing", func() {
			body := v1.CollectorStartRequest{
				Url:      "https://vcenter.example.com",
				Username: "admin",
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/collector", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return 400 for invalid URL format", func() {
			body := v1.CollectorStartRequest{
				Url:      "not-a-valid-url",
				Username: "admin",
				Password: "secret",
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/collector", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("invalid url format"))
		})

		It("should start collector with valid credentials", func() {
			body := v1.CollectorStartRequest{
				Url:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/collector", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusAccepted))
			Expect(mockCollector.StartCallCount).To(Equal(1))
		})

		It("should return 409 when collection already in progress", func() {
			mockCollector.StartError = srvErrors.NewCollectionInProgressError()

			body := v1.CollectorStartRequest{
				Url:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/collector", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusConflict))
		})

		It("should return 500 for other errors", func() {
			mockCollector.StartError = errors.New("unexpected error")

			body := v1.CollectorStartRequest{
				Url:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/collector", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Describe("StopCollector", func() {
		It("should stop collector and return status", func() {
			req := httptest.NewRequest(http.MethodDelete, "/collector", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockCollector.StopCallCount).To(Equal(1))

			var response v1.CollectorStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
