package v1_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	handlers "github.com/kubev2v/assisted-migration-agent/internal/handlers/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("GetCredentials Handler", func() {
	var (
		mockCreds *MockCredentialsService
		handler   *handlers.Handler
		router    *gin.Engine
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		mockCreds = &MockCredentialsService{}
		handler = handlers.NewHandler(config.Configuration{}).WithCredentialsService(mockCreds)
		router = gin.New()
		router.GET("/credentials", handler.GetCredentials)
	})

	It("should return 404 when no credentials stored", func() {
		mockCreds.StatusErr = srvErrors.NewResourceNotFoundError("credentials", "id")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/credentials", nil))

		Expect(w.Code).To(Equal(http.StatusNotFound))
	})

	It("should return 500 for unexpected errors", func() {
		mockCreds.StatusErr = fmt.Errorf("database connection lost")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/credentials", nil))

		Expect(w.Code).To(Equal(http.StatusInternalServerError))
	})
})

var _ = Describe("Credential Capabilities Handler", func() {
	var (
		mockCreds *MockCredentialsService
		handler   *handlers.Handler
		router    *gin.Engine
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		mockCreds = &MockCredentialsService{}
		handler = handlers.NewHandler(config.Configuration{}).WithCredentialsService(mockCreds)
		router = gin.New()
		router.GET("/credentials/capabilities", handler.GetCredentialCapabilities)
	})

	It("should return capabilities when vCenter is reachable", func() {
		mockCreds.CapabilitiesResult = &models.CapabilityStatus{
			Collector:  models.OperationCapability{Enabled: true},
			Inspector:  models.OperationCapability{Enabled: false, MissingPrivileges: []string{"VirtualMachine.Interact.PowerOff"}},
			Forecaster: models.OperationCapability{Enabled: true},
		}

		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/credentials/capabilities", nil))

		Expect(w.Code).To(Equal(http.StatusOK))
		var resp v1.CapabilityStatus
		Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
		Expect(resp.Capabilities.Collector.Enabled).To(BeTrue())
		Expect(resp.Capabilities.Inspector.Enabled).To(BeFalse())
		Expect(resp.Capabilities.Forecaster.Enabled).To(BeTrue())
	})

	It("should return 404 when no credentials are stored", func() {
		mockCreds.CapabilitiesErr = srvErrors.NewResourceNotFoundError("credentials", "id")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/credentials/capabilities", nil))

		Expect(w.Code).To(Equal(http.StatusNotFound))
	})

	It("should return 404 when vCenter is unreachable", func() {
		mockCreds.CapabilitiesErr = srvErrors.NewVCenterError(fmt.Errorf("dial tcp: no such host"))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/credentials/capabilities", nil))

		Expect(w.Code).To(Equal(http.StatusNotFound))
	})

	It("should return 500 for unexpected errors", func() {
		mockCreds.CapabilitiesErr = fmt.Errorf("database connection lost")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/credentials/capabilities", nil))

		Expect(w.Code).To(Equal(http.StatusInternalServerError))
	})
})
