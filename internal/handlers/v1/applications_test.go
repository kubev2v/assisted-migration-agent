package v1_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	handlers "github.com/kubev2v/assisted-migration-agent/internal/handlers/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

var _ = Describe("Applications Handlers", func() {
	var (
		mockApp *MockApplicationService
		handler *handlers.Handler
		router  *gin.Engine
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		mockApp = &MockApplicationService{}
		handler = handlers.NewHandler(config.Configuration{}).WithApplicationService(mockApp)
		router = gin.New()
		router.GET("/applications", func(c *gin.Context) {
			handler.GetApplications(c)
		})
	})

	Context("GetApplications", func() {
		It("should return empty list when no applications match", func() {
			mockApp.ListResult = []models.ApplicationOverview{}

			req := httptest.NewRequest(http.MethodGet, "/applications", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.ApplicationListResponse
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Applications).To(HaveLen(0))
		})

		It("should return applications with matching VMs including names", func() {
			mockApp.ListResult = []models.ApplicationOverview{
				{
					Name:        "Oracle Automatic Storage Management",
					Description: "Hosts identified as Oracle ASM Servers",
					VMCount:     2,
					VMs: []models.ApplicationVM{
						{ID: "vm-1", Name: "prod-db-01"},
						{ID: "vm-2", Name: "prod-db-02"},
					},
				},
			}

			req := httptest.NewRequest(http.MethodGet, "/applications", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.ApplicationListResponse
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Applications).To(HaveLen(1))
			Expect(response.Applications[0].Name).To(Equal("Oracle Automatic Storage Management"))
			Expect(response.Applications[0].Description).To(Equal("Hosts identified as Oracle ASM Servers"))
			Expect(response.Applications[0].VmCount).To(Equal(2))
			Expect(response.Applications[0].Vms).To(HaveLen(2))
			Expect(response.Applications[0].Vms[0].Id).To(Equal("vm-1"))
			Expect(response.Applications[0].Vms[0].Name).To(Equal("prod-db-01"))
		})

		It("should return 500 on service error", func() {
			mockApp.ListError = errors.New("database error")

			req := httptest.NewRequest(http.MethodGet, "/applications", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})

	})
})
