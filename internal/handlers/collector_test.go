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
		handler = handlers.New("", nil, mockCollector, nil, nil, nil)
		router = gin.New()
		router.GET("/collector", handler.GetCollectorStatus)
		router.POST("/collector", handler.StartCollector)
		router.DELETE("/collector", handler.StopCollector)
	})

	Describe("GetCollectorStatus", func() {
		// Given a collector in ready state
		// When we request the collector status
		// Then it should return ready status with 200 OK
		It("should return ready status", func() {
			// Arrange
			req := httptest.NewRequest(http.MethodGet, "/collector", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			var response v1.CollectorStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Status).To(Equal(v1.CollectorStatusStatusReady))
		})

		// Given a collector in collected state
		// When we request the collector status
		// Then it should return collected status with 200 OK
		It("should return collected status", func() {
			// Arrange
			mockCollector.StatusResult = models.CollectorStatus{State: models.CollectorStateCollected}
			req := httptest.NewRequest(http.MethodGet, "/collector", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			var response v1.CollectorStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Status).To(Equal(v1.CollectorStatusStatusCollected))
		})

		// Given a collector in error state with an error message
		// When we request the collector status
		// Then it should return error status with the error message
		It("should return error status with message", func() {
			// Arrange
			mockCollector.StatusResult = models.CollectorStatus{
				State: models.CollectorStateError,
				Error: errors.New("connection failed"),
			}
			req := httptest.NewRequest(http.MethodGet, "/collector", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
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
		// Given a request with invalid JSON body
		// When we try to start the collector
		// Then it should return 400 Bad Request
		It("should return 400 for invalid JSON body", func() {
			// Arrange
			req := httptest.NewRequest(http.MethodPost, "/collector", bytes.NewReader([]byte("invalid json")))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("invalid request body"))
		})

		// Given a request missing the URL field
		// When we try to start the collector
		// Then it should return 400 Bad Request
		It("should return 400 when url is missing", func() {
			// Arrange
			body := v1.CollectorStartRequest{
				Username: "admin",
				Password: "secret",
			}
			bodyBytes, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPost, "/collector", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("url, username, and password are required"))
		})

		// Given a request missing the username field
		// When we try to start the collector
		// Then it should return 400 Bad Request
		It("should return 400 when username is missing", func() {
			// Arrange
			body := v1.CollectorStartRequest{
				Url:      "https://vcenter.example.com",
				Password: "secret",
			}
			bodyBytes, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPost, "/collector", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		// Given a request missing the password field
		// When we try to start the collector
		// Then it should return 400 Bad Request
		It("should return 400 when password is missing", func() {
			// Arrange
			body := v1.CollectorStartRequest{
				Url:      "https://vcenter.example.com",
				Username: "admin",
			}
			bodyBytes, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPost, "/collector", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		// Given a request with an invalid URL format
		// When we try to start the collector
		// Then it should return 400 Bad Request with invalid url format error
		It("should return 400 for invalid URL format", func() {
			// Arrange
			body := v1.CollectorStartRequest{
				Url:      "not-a-valid-url",
				Username: "admin",
				Password: "secret",
			}
			bodyBytes, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPost, "/collector", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("invalid url format"))
		})

		// Given a request with valid credentials
		// When we start the collector
		// Then it should return 202 Accepted and call the collector service
		It("should start collector with valid credentials", func() {
			// Arrange
			body := v1.CollectorStartRequest{
				Url:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}
			bodyBytes, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPost, "/collector", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusAccepted))
			Expect(mockCollector.StartCallCount).To(Equal(1))
		})

		// Given a collector that is already running
		// When we try to start it again
		// Then it should return 409 Conflict
		It("should return 409 when collection already in progress", func() {
			// Arrange
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

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusConflict))
		})

		// Given a collector service that returns an unexpected error
		// When we try to start the collector
		// Then it should return 500 Internal Server Error
		It("should return 500 for other errors", func() {
			// Arrange
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

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Describe("StopCollector", func() {
		// Given a running collector
		// When we stop the collector
		// Then it should return 200 OK and call the stop method
		It("should stop collector and return status", func() {
			// Arrange
			req := httptest.NewRequest(http.MethodDelete, "/collector", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockCollector.StopCallCount).To(Equal(1))
			var response v1.CollectorStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
