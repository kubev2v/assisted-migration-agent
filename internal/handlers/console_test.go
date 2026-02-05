package handlers_test

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/handlers"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("Console Handlers", func() {
	var (
		mockConsole *MockConsoleService
		handler     *handlers.Handler
		router      *gin.Engine
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		mockConsole = &MockConsoleService{
			StatusResult: models.ConsoleStatus{
				Current: models.ConsoleStatusDisconnected,
				Target:  models.ConsoleStatusDisconnected,
			},
		}
		handler = handlers.New("", mockConsole, nil, nil, nil, nil)
		router = gin.New()
		router.GET("/agent", handler.GetAgentStatus)
		router.POST("/agent", handler.SetAgentMode)
	})

	Describe("GetAgentStatus", func() {
		// Given a console service in disconnected mode
		// When we request the agent status
		// Then it should return disconnected status
		It("should return disconnected status", func() {
			// Arrange
			req := httptest.NewRequest(http.MethodGet, "/agent", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.AgentStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.ConsoleConnection).To(Equal(v1.AgentStatusConsoleConnectionDisconnected))
			Expect(response.Mode).To(Equal(v1.AgentStatusModeDisconnected))
		})

		// Given a console service in connected mode
		// When we request the agent status
		// Then it should return connected status
		It("should return connected status", func() {
			// Arrange
			mockConsole.StatusResult = models.ConsoleStatus{
				Current: models.ConsoleStatusConnected,
				Target:  models.ConsoleStatusConnected,
			}

			req := httptest.NewRequest(http.MethodGet, "/agent", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.AgentStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.ConsoleConnection).To(Equal(v1.AgentStatusConsoleConnectionConnected))
			Expect(response.Mode).To(Equal(v1.AgentStatusModeConnected))
		})

		// Given a console service with an error
		// When we request the agent status
		// Then it should include the error in the response
		It("should include error when present", func() {
			// Arrange
			mockConsole.StatusResult = models.ConsoleStatus{
				Current: models.ConsoleStatusDisconnected,
				Target:  models.ConsoleStatusConnected,
				Error:   errors.NewConsoleClientError(401, "unauthorized"),
			}

			req := httptest.NewRequest(http.MethodGet, "/agent", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.AgentStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Error).NotTo(BeNil())
		})
	})

	Describe("SetAgentMode", func() {
		// Given an invalid JSON request body
		// When we try to set the agent mode
		// Then it should return 400 Bad Request
		It("should return 400 for invalid JSON body", func() {
			// Arrange
			req := httptest.NewRequest(http.MethodPost, "/agent", bytes.NewReader([]byte("invalid json")))
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

		// Given a request with an invalid mode value
		// When we try to set the agent mode
		// Then it should return 400 Bad Request
		It("should return 400 for invalid mode", func() {
			// Arrange
			body := map[string]string{"mode": "invalid"}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/agent", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("invalid mode"))
		})

		// Given a valid request to set mode to connected
		// When we set the agent mode
		// Then the console service should be set to connected mode
		It("should set mode to connected", func() {
			// Arrange
			body := v1.AgentModeRequest{Mode: v1.AgentModeRequestModeConnected}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/agent", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockConsole.SetModeCallCount).To(Equal(1))
			Expect(mockConsole.LastModeSet).To(Equal(models.AgentModeConnected))
		})

		// Given a valid request to set mode to disconnected
		// When we set the agent mode
		// Then the console service should be set to disconnected mode
		It("should set mode to disconnected", func() {
			// Arrange
			body := v1.AgentModeRequest{Mode: v1.AgentModeRequestModeDisconnected}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/agent", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockConsole.SetModeCallCount).To(Equal(1))
			Expect(mockConsole.LastModeSet).To(Equal(models.AgentModeDisconnected))
		})

		// Given a console service that returns a mode conflict error
		// When we try to set the agent mode
		// Then it should return 409 Conflict
		It("should return 409 for mode conflict error", func() {
			// Arrange
			mockConsole.SetModeError = errors.NewModeConflictError("console stopped")

			body := v1.AgentModeRequest{Mode: v1.AgentModeRequestModeConnected}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/agent", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusConflict))
		})

		// Given a console service that returns an internal error
		// When we try to set the agent mode
		// Then it should return 500 Internal Server Error
		It("should return 500 for other errors", func() {
			// Arrange
			mockConsole.SetModeError = stderrors.New("database error")

			body := v1.AgentModeRequest{Mode: v1.AgentModeRequestModeConnected}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/agent", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})
	})
})
