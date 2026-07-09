package v1_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	oapimiddleware "github.com/oapi-codegen/gin-middleware"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	handlers "github.com/kubev2v/assisted-migration-agent/internal/handlers/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("VMs Handlers", func() {
	var (
		mockVM        *MockVMService
		mockInspector *MockInspectorService
		handler       *handlers.Handler
		router        *gin.Engine
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		mockVM = &MockVMService{}
		mockInspector = &MockInspectorService{}
		handler = handlers.NewHandler(config.Configuration{}).WithVMService(mockVM).WithInspectorService(mockInspector)
		router = gin.New()

		swagger, err := v1.GetSwagger()
		Expect(err).ToNot(HaveOccurred())
		swagger.Servers = nil
		router.Use(oapimiddleware.OapiRequestValidatorWithOptions(swagger, &oapimiddleware.Options{
			ErrorHandler: func(c *gin.Context, message string, statusCode int) {
				c.JSON(statusCode, gin.H{"error": message})
				c.Abort()
			},
		}))

		router.GET("/vms", func(c *gin.Context) {
			var params v1.GetVMsParams
			if err := c.ShouldBindQuery(&params); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			handler.GetVMs(c, params)
		})
		router.GET("/vms/:id", func(c *gin.Context) {
			handler.GetVM(c, c.Param("id"))
		})
		router.DELETE("/vms/:id/inspection", func(c *gin.Context) {
			handler.RemoveVMFromInspection(c, c.Param("id"))
		})
	})

	Context("GetVMs", func() {
		// Given no VMs exist in the store
		// When we request the VM list
		// Then it should return an empty list with proper pagination
		It("should return empty list when no VMs", func() {
			// Arrange
			mockVM.ListResult = []models.VirtualMachineSummary{}
			mockVM.ListTotal = 0

			req := httptest.NewRequest(http.MethodGet, "/vms", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineListResponse
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Vms).To(HaveLen(0))
			Expect(response.Total).To(Equal(0))
			Expect(response.Page).To(Equal(1))
			Expect(response.PageCount).To(Equal(1))
		})

		// Given VMs exist in the store
		// When we request the VM list
		// Then it should return all VMs with their details
		It("should return list of VMs", func() {
			// Arrange
			mockVM.ListResult = []models.VirtualMachineSummary{
				{ID: "vm-1", Name: "VM 1", Cluster: "cluster-1", DiskSize: 1024, Memory: 2048, PowerState: "poweredOn"},
				{ID: "vm-2", Name: "VM 2", Cluster: "cluster-1", DiskSize: 2048, Memory: 4096, PowerState: "poweredOff"},
			}
			mockVM.ListTotal = 2

			req := httptest.NewRequest(http.MethodGet, "/vms", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineListResponse
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Vms).To(HaveLen(2))
			Expect(response.Total).To(Equal(2))
			Expect(response.Vms[0].Id).To(Equal("vm-1"))
			Expect(response.Vms[1].Id).To(Equal("vm-2"))
		})

		// Given pagination parameters in the request
		// When we request the VM list
		// Then it should apply the correct offset and limit
		It("should handle pagination parameters", func() {
			// Arrange
			mockVM.ListResult = []models.VirtualMachineSummary{}
			mockVM.ListTotal = 50

			req := httptest.NewRequest(http.MethodGet, "/vms?page=2&pageSize=10", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockVM.LastListParams.Offset).To(Equal(uint64(10)))
			Expect(mockVM.LastListParams.Limit).To(Equal(uint64(10)))
		})

		// Given a page size larger than the maximum allowed
		// When we request the VM list
		// Then it should limit the page size to the maximum
		It("should limit page size to max", func() {
			// Arrange
			mockVM.ListResult = []models.VirtualMachineSummary{}
			mockVM.ListTotal = 0

			req := httptest.NewRequest(http.MethodGet, "/vms?pageSize=200", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockVM.LastListParams.Limit).To(Equal(uint64(100)))
		})

		// Given an invalid sort format
		// When we request the VM list
		// Then it should return 400 Bad Request
		It("should return 400 for invalid sort format", func() {
			// Arrange
			req := httptest.NewRequest(http.MethodGet, "/vms?sort=invalidformat", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("invalid sort format"))
		})

		// Given an invalid sort field
		// When we request the VM list
		// Then it should return 400 Bad Request
		It("should return 400 for invalid sort field", func() {
			// Arrange
			req := httptest.NewRequest(http.MethodGet, "/vms?sort=invalidfield:asc", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("invalid sort field"))
		})

		// Given an invalid sort direction
		// When we request the VM list
		// Then it should return 400 Bad Request
		It("should return 400 for invalid sort direction", func() {
			// Arrange
			req := httptest.NewRequest(http.MethodGet, "/vms?sort=name:invalid", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("invalid sort direction"))
		})

		// Given valid sort parameters
		// When we request the VM list
		// Then it should apply the sort parameters correctly
		It("should accept valid sort parameters", func() {
			// Arrange
			mockVM.ListResult = []models.VirtualMachineSummary{}
			mockVM.ListTotal = 0

			req := httptest.NewRequest(http.MethodGet, "/vms?sort=name:asc&sort=cluster:desc", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockVM.LastListParams.Sort).To(HaveLen(2))
			Expect(mockVM.LastListParams.Sort[0].Field).To(Equal("name"))
			Expect(mockVM.LastListParams.Sort[0].Desc).To(BeFalse())
			Expect(mockVM.LastListParams.Sort[1].Field).To(Equal("cluster"))
			Expect(mockVM.LastListParams.Sort[1].Desc).To(BeTrue())
		})

		// Given valid utilization sort fields
		// Then the handler should accept them without returning 400
		It("should accept utilization sort fields", func() {
			// Arrange
			mockVM.ListResult = []models.VirtualMachineSummary{}
			mockVM.ListTotal = 0

			for _, field := range []string{"cpuUsage", "diskUsage", "ramUsage"} {
				req := httptest.NewRequest(http.MethodGet, "/vms?sort="+field+":asc", nil)
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusOK), "field %q should be accepted", field)
			}
		})

		// Given utilization sort fields in descending direction
		// Then the handler should propagate them to the service layer correctly
		It("should accept utilization sort fields descending", func() {
			mockVM.ListResult = []models.VirtualMachineSummary{}
			mockVM.ListTotal = 0

			req := httptest.NewRequest(http.MethodGet, "/vms?sort=cpuUsage:desc&sort=diskUsage:asc&sort=ramUsage:desc", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockVM.LastListParams.Sort).To(HaveLen(3))
			Expect(mockVM.LastListParams.Sort[0].Field).To(Equal("cpuUsage"))
			Expect(mockVM.LastListParams.Sort[0].Desc).To(BeTrue())
			Expect(mockVM.LastListParams.Sort[1].Field).To(Equal("diskUsage"))
			Expect(mockVM.LastListParams.Sort[1].Desc).To(BeFalse())
			Expect(mockVM.LastListParams.Sort[2].Field).To(Equal("ramUsage"))
			Expect(mockVM.LastListParams.Sort[2].Desc).To(BeTrue())
		})

		// Given cpuAvg and memAvg sort fields
		// Then the handler should accept them without returning 400
		It("should accept cpuAvg and memAvg sort fields", func() {
			mockVM.ListResult = []models.VirtualMachineSummary{}
			mockVM.ListTotal = 0

			for _, field := range []string{"cpuAvg", "memAvg"} {
				req := httptest.NewRequest(http.MethodGet, "/vms?sort="+field+":asc", nil)
				w := httptest.NewRecorder()

				router.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK), "field %q should be accepted", field)
			}
		})

		// Given cpuAvg desc and memAvg asc
		// Then the handler should propagate both to the service layer correctly
		It("should propagate cpuAvg and memAvg sort params to service", func() {
			mockVM.ListResult = []models.VirtualMachineSummary{}
			mockVM.ListTotal = 0

			req := httptest.NewRequest(
				http.MethodGet,
				"/vms?sort=cpuAvg:desc&sort=memAvg:asc",
				nil,
			)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockVM.LastListParams.Sort).To(HaveLen(2))
			Expect(mockVM.LastListParams.Sort[0].Field).To(Equal("cpuAvg"))
			Expect(mockVM.LastListParams.Sort[0].Desc).To(BeTrue())
			Expect(mockVM.LastListParams.Sort[1].Field).To(Equal("memAvg"))
			Expect(mockVM.LastListParams.Sort[1].Desc).To(BeFalse())
		})

		// Given a service error occurs
		// When we request the VM list
		// Then it should return 500 Internal Server Error
		It("should return 500 for service errors", func() {
			// Arrange
			mockVM.ListError = errors.New("database error")

			req := httptest.NewRequest(http.MethodGet, "/vms", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(Equal("failed to list VMs: database error"))
		})

		It("should return 400 when byExpression is invalid", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?byExpression=!!!invalid", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(HavePrefix("expression filter is invalid:"))
		})

		It("should return 400 when byExpression exceeds 10KB", func() {
			longValue := strings.Repeat("x", 10240)
			expr := fmt.Sprintf("name = '%s'", longValue)
			Expect(len(expr)).To(BeNumerically(">", 10240))

			req := httptest.NewRequest(http.MethodGet, "/vms?byExpression="+url.QueryEscape(expr), nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(ContainSubstring("maximum string length is 10240"))
		})

	})

	Context("GetVM", func() {
		// Given a VM exists with the requested ID
		// When we request the VM details
		// Then it should return the full VM details
		It("should return VM details", func() {
			// Arrange
			mockVM.GetResult = &models.VM{
				ID:              "vm-1",
				Name:            "Test VM",
				PowerState:      "poweredOn",
				ConnectionState: "connected",
				CpuCount:        4,
				CoresPerSocket:  2,
				MemoryMB:        8192,
			}

			req := httptest.NewRequest(http.MethodGet, "/vms/vm-1", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineDetail
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Id).To(Equal("vm-1"))
			Expect(response.Name).To(Equal("Test VM"))
			Expect(response.CpuCount).To(Equal(int32(4)))
		})

		It("should include guest apps in VM details", func() {
			// Arrange
			mockVM.GetResult = &models.VM{
				ID:              "vm-1",
				Name:            "Test VM",
				PowerState:      "poweredOn",
				ConnectionState: "connected",
				CpuCount:        4,
				CoresPerSocket:  2,
				MemoryMB:        8192,
				GuestApps: []models.GuestApp{
					{Name: "nginx", Version: "1.25.0"},
					{Name: "postgres", Version: "15.2"},
				},
			}

			req := httptest.NewRequest(http.MethodGet, "/vms/vm-1", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineDetail
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Processes).NotTo(BeNil())
			Expect(*response.Processes).To(HaveLen(2))
			Expect((*response.Processes)[0].Name).To(Equal("nginx"))
			Expect(*(*response.Processes)[0].Version).To(Equal("1.25.0"))
			Expect((*response.Processes)[1].Name).To(Equal("postgres"))
			Expect(*(*response.Processes)[1].Version).To(Equal("15.2"))
		})

		It("should omit guest apps when VM has none", func() {
			// Arrange
			mockVM.GetResult = &models.VM{
				ID:              "vm-1",
				Name:            "Test VM",
				PowerState:      "poweredOn",
				ConnectionState: "connected",
				CpuCount:        4,
				CoresPerSocket:  2,
				MemoryMB:        8192,
			}

			req := httptest.NewRequest(http.MethodGet, "/vms/vm-1", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineDetail
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Processes).To(BeNil())
		})

		// Given a VM does not exist with the requested ID
		// When we request the VM details
		// Then it should return 404 Not Found
		It("should return 404 when VM not found", func() {
			// Arrange
			mockVM.GetError = srvErrors.NewResourceNotFoundError("vm", "vm-nonexistent")

			req := httptest.NewRequest(http.MethodGet, "/vms/vm-nonexistent", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusNotFound))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("not found"))
		})

		It("should return 500 for non-404 Get errors", func() {
			mockVM.GetError = errors.New("database connection lost")

			req := httptest.NewRequest(http.MethodGet, "/vms/vm-1", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(Equal("database connection lost"))
		})
	})

	Context("VM inspection endpoints (/vms/{id}/inspection)", func() {
		It("RemoveVMFromInspection should return 204 on success", func() {
			// Arrange
			req := httptest.NewRequest(http.MethodDelete, "/vms/vm-1/inspection", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusNoContent))
			Expect(mockInspector.CancelVmsInspectionCallCount).To(Equal(1))
		})

		// Given Cancel returns an error
		// When we remove a VM from inspection
		// Then it should return 500 Internal Server Error
		It("RemoveVMFromInspection should return 500 when cancel fails", func() {
			// Arrange
			mockInspector.CancelError = errors.New("cancel failed")

			req := httptest.NewRequest(http.MethodDelete, "/vms/vm-1/inspection", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(Equal("cancel failed"))
		})

		It("RemoveVMFromInspection should return 400 when inspector not running", func() {
			mockInspector.CancelError = srvErrors.NewInspectorNotRunningError()

			req := httptest.NewRequest(http.MethodDelete, "/vms/vm-1/inspection", nil)

			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(Equal("inspector not running"))
		})
	})

	Context("Label Operations", func() {
		BeforeEach(func() {
			// Setup routes for label operations
			router.PATCH("/vms/:id", func(c *gin.Context) {
				handler.UpdateVM(c, c.Param("id"))
			})
			router.GET("/vms/labels", func(c *gin.Context) {
				handler.GetVMLabels(c)
			})
			router.PATCH("/vms/labels/:label", func(c *gin.Context) {
				handler.UpdateLabelVMs(c, c.Param("label"))
			})
			router.DELETE("/vms/labels/:label", func(c *gin.Context) {
				handler.DeleteLabelGlobally(c, c.Param("label"))
			})
		})

		Context("UpdateVM with labels", func() {
			It("should accept valid labels", func() {
				// Arrange
				body := `{"labels": ["production", "critical"]}`
				req := httptest.NewRequest(http.MethodPatch, "/vms/vm-1", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusOK))
			})

			It("should reject labels with empty strings", func() {
				// Arrange
				body := `{"labels": ["valid", ""]}`
				req := httptest.NewRequest(http.MethodPatch, "/vms/vm-1", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})

			It("should reject labels exceeding 100 characters", func() {
				// Arrange
				longLabel := strings.Repeat("a", 101)
				body := fmt.Sprintf(`{"labels": ["%s"]}`, longLabel)
				req := httptest.NewRequest(http.MethodPatch, "/vms/vm-1", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})
		})

		Context("GetVMLabels", func() {
			It("should return all labels with counts", func() {
				// Arrange
				mockVM.GetAllLabelsResult = []string{"critical", "production", "staging"}
				mockVM.GetAllLabelsCountsResult = []int{2, 5, 3}
				req := httptest.NewRequest(http.MethodGet, "/vms/labels", nil)
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusOK))
				var response v1.VMLabelsResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				Expect(err).NotTo(HaveOccurred())
				Expect(response.Labels).To(Equal([]string{"critical", "production", "staging"}))
				Expect(response.Counts).To(Equal([]int{2, 5, 3}))
			})

			It("should return empty arrays when no labels exist", func() {
				// Arrange
				mockVM.GetAllLabelsResult = []string{}
				mockVM.GetAllLabelsCountsResult = []int{}
				req := httptest.NewRequest(http.MethodGet, "/vms/labels", nil)
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusOK))
				var response v1.VMLabelsResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				Expect(err).NotTo(HaveOccurred())
				Expect(response.Labels).To(BeEmpty())
				Expect(response.Counts).To(BeEmpty())
			})

			It("should ensure labels and counts have same length", func() {
				// Arrange
				mockVM.GetAllLabelsResult = []string{"label1", "label2", "label3", "label4", "label5"}
				mockVM.GetAllLabelsCountsResult = []int{10, 20, 5, 1, 15}
				req := httptest.NewRequest(http.MethodGet, "/vms/labels", nil)
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusOK))
				var response v1.VMLabelsResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				Expect(err).NotTo(HaveOccurred())
				Expect(response.Labels).To(HaveLen(5))
				Expect(response.Counts).To(HaveLen(5))
				Expect(len(response.Labels)).To(Equal(len(response.Counts)))
			})

			It("should handle single label with high count", func() {
				// Arrange
				mockVM.GetAllLabelsResult = []string{"production"}
				mockVM.GetAllLabelsCountsResult = []int{100}
				req := httptest.NewRequest(http.MethodGet, "/vms/labels", nil)
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusOK))
				var response v1.VMLabelsResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				Expect(err).NotTo(HaveOccurred())
				Expect(response.Labels).To(Equal([]string{"production"}))
				Expect(response.Counts).To(Equal([]int{100}))
			})

			It("should handle service error gracefully", func() {
				// Arrange
				mockVM.GetAllLabelsResult = nil
				mockVM.GetAllLabelsCountsResult = nil
				mockVM.GetAllLabelsError = errors.New("database error")
				req := httptest.NewRequest(http.MethodGet, "/vms/labels", nil)
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusInternalServerError))
			})

			It("should return counts in same order as labels", func() {
				// Arrange - alphabetically sorted labels with corresponding counts
				mockVM.GetAllLabelsResult = []string{"alpha", "beta", "gamma", "omega"}
				mockVM.GetAllLabelsCountsResult = []int{5, 10, 15, 20}
				req := httptest.NewRequest(http.MethodGet, "/vms/labels", nil)
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusOK))
				var response v1.VMLabelsResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				Expect(err).NotTo(HaveOccurred())

				// Verify order is maintained
				for i, label := range response.Labels {
					expectedCount := mockVM.GetAllLabelsCountsResult[i]
					Expect(response.Counts[i]).To(Equal(expectedCount),
						"Count for label %s at index %d should be %d", label, i, expectedCount)
				}
			})
		})

		Context("UpdateLabelVMs", func() {
			It("should accept valid label parameter and execute atomically", func() {
				// Arrange
				mockVM.UpdateLabelVMsError = nil
				body := `{"add": ["vm-1", "vm-2"]}`
				req := httptest.NewRequest(http.MethodPatch, "/vms/labels/production", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusOK))
				Expect(mockVM.LastUpdateLabelVMsAdd).To(Equal([]string{"vm-1", "vm-2"}))
				Expect(mockVM.LastUpdateLabelVMsRem).To(BeEmpty())
				Expect(mockVM.LastUpdateLabelVMsLabel).To(Equal("production"))
			})

			It("should reject empty label parameter", func() {
				// Arrange
				body := `{"add": ["vm-1"]}`
				req := httptest.NewRequest(http.MethodPatch, "/vms/labels/%20", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusBadRequest))
				Expect(w.Body.String()).To(ContainSubstring("label cannot be empty"))
			})

			It("should reject whitespace-only label parameter", func() {
				// Arrange
				body := `{"add": ["vm-1"]}`
				req := httptest.NewRequest(http.MethodPatch, "/vms/labels/%20%20%20", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusBadRequest))
				Expect(w.Body.String()).To(ContainSubstring("label cannot be empty"))
			})

			It("should reject label parameter exceeding 100 characters", func() {
				// Arrange
				longLabel := strings.Repeat("a", 101)
				body := `{"add": ["vm-1"]}`
				req := httptest.NewRequest(http.MethodPatch, "/vms/labels/"+longLabel, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusBadRequest))
				Expect(w.Body.String()).To(ContainSubstring("maximum string length is 100"))
			})

			It("should reject request with neither add nor remove", func() {
				// Arrange
				body := `{}`
				req := httptest.NewRequest(http.MethodPatch, "/vms/labels/production", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusBadRequest))
				Expect(w.Body.String()).To(ContainSubstring("at least one"))
			})
		})

		Context("DeleteLabelGlobally", func() {
			It("should accept valid label parameter", func() {
				// Arrange
				mockVM.RemoveLabelFromAllVMsResult = 5
				req := httptest.NewRequest(http.MethodDelete, "/vms/labels/production", nil)
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusOK))
				var response v1.DeleteLabelGloballyResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				Expect(err).NotTo(HaveOccurred())
				Expect(response.Affected).To(Equal(5))
				Expect(response.Label).To(Equal("production"))
			})

			It("should reject empty label parameter", func() {
				// Arrange
				req := httptest.NewRequest(http.MethodDelete, "/vms/labels/%20", nil)
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusBadRequest))
				Expect(w.Body.String()).To(ContainSubstring("label cannot be empty"))
			})

			It("should reject whitespace-only label parameter", func() {
				// Arrange
				req := httptest.NewRequest(http.MethodDelete, "/vms/labels/%20%20%20", nil)
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusBadRequest))
				Expect(w.Body.String()).To(ContainSubstring("label cannot be empty"))
			})

			It("should reject label parameter exceeding 100 characters", func() {
				// Arrange
				longLabel := strings.Repeat("a", 101)
				req := httptest.NewRequest(http.MethodDelete, "/vms/labels/"+longLabel, nil)
				w := httptest.NewRecorder()

				// Act
				router.ServeHTTP(w, req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusBadRequest))
				Expect(w.Body.String()).To(ContainSubstring("maximum string length is 100"))
			})
		})
	})

})

var _ = Describe("Version Handler", func() {
	It("should return version info", func() {
		gin.SetMode(gin.TestMode)
		handler := handlers.NewHandler(config.Configuration{
			Agent: config.Agent{
				Version:   "1.2.3",
				GitCommit: "abc123",
			},
		})
		router := gin.New()
		router.GET("/version", handler.GetVersion)

		req := httptest.NewRequest(http.MethodGet, "/version", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		Expect(w.Code).To(Equal(http.StatusOK))
		var resp v1.VersionInfo
		Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
		Expect(resp.Version).To(Equal("1.2.3"))
		Expect(resp.GitCommit).To(Equal("abc123"))
	})
})

var _ = Describe("VMs Handlers Integration", func() {
	var (
		ctx           context.Context
		db            *sql.DB
		st            *store.Store
		vmSrv         *services.VMService
		mockInspector *MockInspectorService
		handler       *handlers.Handler
		router        *gin.Engine
	)

	BeforeEach(func() {
		ctx = context.Background()
		gin.SetMode(gin.TestMode)

		var err error
		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())

		// Migrate the store (creates vinfo, vdisk, concerns tables via parser.Init())
		err = st.Migrate(ctx)
		Expect(err).NotTo(HaveOccurred())

		// Insert test data
		err = test.InsertVMs(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		vmSrv = services.NewVMService(st)
		mockInspector = &MockInspectorService{}
		handler = handlers.NewHandler(config.Configuration{}).
			WithVMService(vmSrv).
			WithInspectorService(mockInspector)
		router = gin.New()
		router.GET("/vms", func(c *gin.Context) {
			var params v1.GetVMsParams
			if err := c.ShouldBindQuery(&params); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			handler.GetVMs(c, params)
		})
		router.GET("/vms/:id", func(c *gin.Context) {
			handler.GetVM(c, c.Param("id"))
		})
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	Context("GetVMs with real data", func() {
		It("should return all 10 VMs", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?pageSize=50", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineListResponse
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Total).To(Equal(10))
			Expect(response.Vms).To(HaveLen(10))
		})

		It("should paginate correctly", func() {
			// First page
			req := httptest.NewRequest(http.MethodGet, "/vms?page=1&pageSize=3", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			var page1 v1.VirtualMachineListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &page1)).To(Succeed())
			Expect(page1.Page).To(Equal(1))
			Expect(page1.PageCount).To(Equal(4)) // 10 VMs / 3 per page = 4 pages
			Expect(page1.Total).To(Equal(10))
			Expect(page1.Vms).To(HaveLen(3))

			// Second page
			req = httptest.NewRequest(http.MethodGet, "/vms?page=2&pageSize=3", nil)
			w = httptest.NewRecorder()
			router.ServeHTTP(w, req)

			var page2 v1.VirtualMachineListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &page2)).To(Succeed())
			Expect(page2.Page).To(Equal(2))
			Expect(page2.Vms).To(HaveLen(3))

			// Ensure different VMs on each page
			page1IDs := make(map[string]bool)
			for _, vm := range page1.Vms {
				page1IDs[vm.Id] = true
			}
			for _, vm := range page2.Vms {
				Expect(page1IDs[vm.Id]).To(BeFalse(), "VM %s should not appear on both pages", vm.Id)
			}
		})

		It("should filter by cluster using byExpression", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?byExpression=cluster+%3D+%27production%27", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Total).To(Equal(4))
			for _, vm := range response.Vms {
				Expect(vm.Cluster).To(Equal("production"))
			}
		})

		It("should filter by multiple clusters using byExpression", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?byExpression=cluster+%3D+%27production%27+or+cluster+%3D+%27staging%27", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Total).To(Equal(7)) // 4 production + 3 staging
			for _, vm := range response.Vms {
				Expect(vm.Cluster).To(BeElementOf("production", "staging"))
			}
		})

		It("should filter by power state using byExpression", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?byExpression=powerstate+%3D+%27poweredOff%27", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Total).To(Equal(2)) // vm-004 and vm-009
			for _, vm := range response.Vms {
				Expect(vm.VCenterState).To(Equal("poweredOff"))
			}
		})

		It("should filter by disk size range using byExpression", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?byExpression=disk.capacity+%3E%3D+100+and+disk.capacity+%3C+250", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			for _, vm := range response.Vms {
				Expect(vm.DiskSize).To(BeNumerically(">=", 100))
				Expect(vm.DiskSize).To(BeNumerically("<", 250))
			}
		})

		It("should filter by memory size range using byExpression", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?byExpression=memory+%3E%3D+8000+and+memory+%3C+20000", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Total).To(Equal(4)) // db-server-1, db-server-2, app-server-1, app-server-2
			for _, vm := range response.Vms {
				Expect(vm.Memory).To(BeNumerically(">=", 8000))
				Expect(vm.Memory).To(BeNumerically("<", 20000))
			}
		})

		It("should sort by name ascending", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?sort=name:asc&pageSize=50", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Vms).To(HaveLen(10))
			Expect(response.Vms[0].Name).To(Equal("app-server-1"))
			Expect(response.Vms[1].Name).To(Equal("app-server-2"))
		})

		It("should sort by memory descending", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?sort=memory:desc&pageSize=50", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Vms).To(HaveLen(10))
			Expect(response.Vms[0].Memory).To(Equal(int64(16384)))
		})

		It("should sort by issues descending", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?sort=issues:desc&pageSize=50", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Vms[0].IssueCount).To(Equal(3)) // vm-007 has 3 issues
		})

		It("should combine byExpression filter with pagination", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?byExpression=cluster+%3D+%27production%27&page=1&pageSize=2", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Total).To(Equal(4))
			Expect(response.Vms).To(HaveLen(2))
			Expect(response.PageCount).To(Equal(2))
			for _, vm := range response.Vms {
				Expect(vm.Cluster).To(Equal("production"))
			}
		})

		It("should combine multiple conditions in byExpression", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?byExpression=cluster+%3D+%27production%27+and+powerstate+%3D+%27poweredOn%27", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Total).To(Equal(3)) // web-server-1, web-server-2, db-server-1
			for _, vm := range response.Vms {
				Expect(vm.Cluster).To(Equal("production"))
				Expect(vm.VCenterState).To(Equal("poweredOn"))
			}
		})

		It("should return correct disk size aggregation", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?pageSize=50", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())

			// Find vm-003 which has 2 disks of 500 MiB each
			var vm003 *v1.VirtualMachine
			for i := range response.Vms {
				if response.Vms[i].Id == "vm-003" {
					vm003 = &response.Vms[i]
					break
				}
			}
			Expect(vm003).NotTo(BeNil())
			Expect(vm003.DiskSize).To(Equal(int64(1000))) // 500 + 500
		})

		It("should return correct issue count", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?pageSize=50", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())

			// Find VMs and check their issue counts
			issueMap := make(map[string]int)
			for _, vm := range response.Vms {
				issueMap[vm.Id] = vm.IssueCount
			}

			Expect(issueMap["vm-003"]).To(Equal(2))
			Expect(issueMap["vm-004"]).To(Equal(1))
			Expect(issueMap["vm-007"]).To(Equal(3))
			Expect(issueMap["vm-001"]).To(Equal(0))
		})
	})

	Context("GetVM with real data", func() {
		It("should return VM details by ID", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms/vm-003", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineDetail
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Id).To(Equal("vm-003"))
			Expect(response.Name).To(Equal("db-server-1"))
			Expect(response.PowerState).To(Equal("poweredOn"))
			Expect(response.ConnectionState).To(Equal("connected"))
			Expect(response.MemoryMB).To(Equal(int32(16384)))
			Expect(response.CpuCount).To(Equal(int32(8)))
			Expect(response.CoresPerSocket).To(Equal(int32(8)))
		})

		It("should return 404 for non-existent VM", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms/vm-nonexistent", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusNotFound))

			var response map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response["error"]).To(ContainSubstring("not found"))
		})

		It("should return VM with disk details", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms/vm-003", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineDetail
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Disks).To(HaveLen(2))
			Expect(*response.Disks[0].Capacity).To(Equal(int64(500 * 1024 * 1024)))
		})

		It("should return VM with NICs", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms/vm-003", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineDetail
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Nics).To(HaveLen(2))
		})

		It("should return VM with issues including descriptions and categories", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms/vm-007", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineDetail
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Issues).NotTo(BeNil())
			Expect(*response.Issues).To(HaveLen(3))

			// Validate the structure of issues
			issues := *response.Issues

			// Find the Critical issue (RDM disk)
			var criticalIssue *v1.VMIssue
			var warnIssue *v1.VMIssue
			var infoIssue *v1.VMIssue

			for i := range issues {
				switch issues[i].Label {
				case "RDM disk detected":
					criticalIssue = &issues[i]
				case "Suspended state":
					warnIssue = &issues[i]
				case "Storage warning":
					infoIssue = &issues[i]
				}
			}

			// Validate Critical issue
			Expect(criticalIssue).NotTo(BeNil())
			Expect(criticalIssue.Label).To(Equal("RDM disk detected"))
			Expect(criticalIssue.Category).To(Equal(v1.VMIssueCategoryCritical))
			Expect(criticalIssue.Description).To(ContainSubstring("Raw Device Mapping"))

			// Validate Warning issue
			Expect(warnIssue).NotTo(BeNil())
			Expect(warnIssue.Label).To(Equal("Suspended state"))
			Expect(warnIssue.Category).To(Equal(v1.VMIssueCategoryWarning))
			Expect(warnIssue.Description).To(ContainSubstring("suspended state"))

			// Validate Information issue
			Expect(infoIssue).NotTo(BeNil())
			Expect(infoIssue.Label).To(Equal("Storage warning"))
			Expect(infoIssue.Category).To(Equal(v1.VMIssueCategoryInformation))
			Expect(infoIssue.Description).To(ContainSubstring("storage usage"))
		})

		It("should normalize unknown category to Other", func() {
			// Insert an issue with an unknown category
			_, err := db.ExecContext(ctx, `
				INSERT INTO concerns ("VM_ID", "Concern_ID", "Label", "Category", "Assessment")
				VALUES ('vm-001', 'test.unknown-cat', 'Test Unknown Category', 'UnknownCategory', 'This category should be normalized to Other')
			`)
			Expect(err).NotTo(HaveOccurred())

			req := httptest.NewRequest(http.MethodGet, "/vms/vm-001", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineDetail
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Issues).NotTo(BeNil())
			Expect(*response.Issues).To(HaveLen(1))

			issue := (*response.Issues)[0]
			Expect(issue.Category).To(Equal(v1.VMIssueCategoryOther))
			Expect(issue.Label).To(Equal("Test Unknown Category"))
		})

		It("should normalize category case variants", func() {
			// Insert issues with various case formats
			_, err := db.ExecContext(ctx, `
				INSERT INTO concerns ("VM_ID", "Concern_ID", "Label", "Category", "Assessment")
				VALUES
					('vm-002', 'test.lowercase', 'Lowercase Test', 'critical', 'Test critical'),
					('vm-002', 'test.uppercase', 'Uppercase Test', 'WARNING', 'Test warning'),
					('vm-002', 'test.mixedcase', 'Mixed Test', 'InFoRmAtIoN', 'Test info')
			`)
			Expect(err).NotTo(HaveOccurred())

			req := httptest.NewRequest(http.MethodGet, "/vms/vm-002", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VirtualMachineDetail
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Issues).NotTo(BeNil())
			Expect(*response.Issues).To(HaveLen(3))

			issues := *response.Issues
			for _, issue := range issues {
				switch issue.Label {
				case "Lowercase Test":
					Expect(issue.Category).To(Equal(v1.VMIssueCategoryCritical))
				case "Uppercase Test":
					Expect(issue.Category).To(Equal(v1.VMIssueCategoryWarning))
				case "Mixed Test":
					Expect(issue.Category).To(Equal(v1.VMIssueCategoryInformation))
				}
			}
		})
	})

})
