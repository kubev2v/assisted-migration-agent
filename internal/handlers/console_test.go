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
		handler = handlers.New("", mockConsole, nil, nil, nil)
		router = gin.New()
		router.GET("/agent", handler.GetAgentStatus)
		router.POST("/agent", handler.SetAgentMode)
	})

	Describe("GetAgentStatus", func() {
		It("should return disconnected status", func() {
			req := httptest.NewRequest(http.MethodGet, "/agent", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.AgentStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.ConsoleConnection).To(Equal(v1.AgentStatusConsoleConnectionDisconnected))
			Expect(response.Mode).To(Equal(v1.AgentStatusModeDisconnected))
		})

		It("should return connected status", func() {
			mockConsole.StatusResult = models.ConsoleStatus{
				Current: models.ConsoleStatusConnected,
				Target:  models.ConsoleStatusConnected,
			}

			req := httptest.NewRequest(http.MethodGet, "/agent", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.AgentStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.ConsoleConnection).To(Equal(v1.AgentStatusConsoleConnectionConnected))
			Expect(response.Mode).To(Equal(v1.AgentStatusModeConnected))
		})

		It("should include error when present", func() {
			mockConsole.StatusResult = models.ConsoleStatus{
				Current: models.ConsoleStatusDisconnected,
				Target:  models.ConsoleStatusConnected,
				Error:   errors.NewConsoleClientError(401, "unauthorized"),
			}

			req := httptest.NewRequest(http.MethodGet, "/agent", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.AgentStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Error).NotTo(BeNil())
		})
	})

	Describe("SetAgentMode", func() {
		It("should return 400 for invalid JSON body", func() {
			req := httptest.NewRequest(http.MethodPost, "/agent", bytes.NewReader([]byte("invalid json")))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("invalid request body"))
		})

		It("should return 400 for invalid mode", func() {
			body := map[string]string{"mode": "invalid"}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/agent", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("invalid mode"))
		})

		It("should set mode to connected", func() {
			body := v1.AgentModeRequest{Mode: v1.AgentModeRequestModeConnected}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/agent", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockConsole.SetModeCallCount).To(Equal(1))
			Expect(mockConsole.LastModeSet).To(Equal(models.AgentModeConnected))
		})

		It("should set mode to disconnected", func() {
			body := v1.AgentModeRequest{Mode: v1.AgentModeRequestModeDisconnected}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/agent", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockConsole.SetModeCallCount).To(Equal(1))
			Expect(mockConsole.LastModeSet).To(Equal(models.AgentModeDisconnected))
		})

		It("should return 409 for mode conflict error", func() {
			mockConsole.SetModeError = errors.NewModeConflictError("console stopped")

			body := v1.AgentModeRequest{Mode: v1.AgentModeRequestModeConnected}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/agent", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusConflict))
		})

		It("should return 500 for other errors", func() {
			mockConsole.SetModeError = stderrors.New("database error")

			body := v1.AgentModeRequest{Mode: v1.AgentModeRequestModeConnected}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/agent", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})
	})
})
