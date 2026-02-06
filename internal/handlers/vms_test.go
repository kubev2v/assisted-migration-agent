package handlers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/handlers"
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
		handler = handlers.New("", nil, nil, nil, mockVM, mockInspector)
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
		router.GET("/vms/inspector", handler.GetInspectorStatus)
		router.POST("/vms/inspector", handler.StartInspection)
		router.PATCH("/vms/inspector", handler.AddVMsToInspection)
		router.DELETE("/vms/inspector", handler.StopInspection)
		router.GET("/vms/:id/inspector", func(c *gin.Context) {
			handler.GetVMInspectionStatus(c, c.Param("id"))
		})
		router.DELETE("/vms/:id/inspector", func(c *gin.Context) {
			handler.RemoveVMFromInspection(c, c.Param("id"))
		})
	})

	Context("GetVMs", func() {
		// Given no VMs exist in the store
		// When we request the VM list
		// Then it should return an empty list with proper pagination
		It("should return empty list when no VMs", func() {
			// Arrange
			mockVM.ListResult = []models.VMSummary{}
			mockVM.ListTotal = 0

			req := httptest.NewRequest(http.MethodGet, "/vms", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VMListResponse
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
			mockVM.ListResult = []models.VMSummary{
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

			var response v1.VMListResponse
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
			mockVM.ListResult = []models.VMSummary{}
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
			mockVM.ListResult = []models.VMSummary{}
			mockVM.ListTotal = 0

			req := httptest.NewRequest(http.MethodGet, "/vms?pageSize=200", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockVM.LastListParams.Limit).To(Equal(uint64(100)))
		})

		// Given a disk size range where min is greater than max
		// When we request the VM list
		// Then it should return 400 Bad Request
		It("should return 400 for invalid disk size range", func() {
			// Arrange
			req := httptest.NewRequest(http.MethodGet, "/vms?diskSizeMin=1000&diskSizeMax=500", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("diskSizeMin cannot be greater than diskSizeMax"))
		})

		// Given a memory size range where min is greater than max
		// When we request the VM list
		// Then it should return 400 Bad Request
		It("should return 400 for invalid memory size range", func() {
			// Arrange
			req := httptest.NewRequest(http.MethodGet, "/vms?memorySizeMin=8000&memorySizeMax=4000", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("memorySizeMin cannot be greater than memorySizeMax"))
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
			mockVM.ListResult = []models.VMSummary{}
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

			var response v1.VMDetails
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Id).To(Equal("vm-1"))
			Expect(response.Name).To(Equal("Test VM"))
			Expect(response.CpuCount).To(Equal(int32(4)))
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
	})

	Context("Inspector endpoints", func() {
		// Given an inspector service
		// When we request the inspector status
		// Then it should return the current status
		It("GetInspectorStatus should return status", func() {
			// Arrange
			mockInspector.GetStatusResult = models.InspectorStatus{
				State: models.InspectorStateReady,
			}

			req := httptest.NewRequest(http.MethodGet, "/vms/inspector", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			var response v1.InspectorStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.State).To(Equal(v1.InspectorStatusStateReady))
		})

		// Given an invalid JSON request body
		// When we try to start an inspection
		// Then it should return 400 Bad Request
		It("StartInspection should return 400 for invalid request body", func() {
			// Arrange
			req := httptest.NewRequest(http.MethodPost, "/vms/inspector", strings.NewReader("invalid json"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		// Given missing vCenter credentials
		// When we try to start an inspection
		// Then it should return 400 Bad Request
		It("StartInspection should return 400 for missing credentials", func() {
			// Arrange
			body := `{"vcenterCredentials":{"url":"","username":"","password":""},"vmIds":["vm-1"]}`
			req := httptest.NewRequest(http.MethodPost, "/vms/inspector", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		// Given an empty VM list in the request
		// When we try to start an inspection
		// Then it should return 400 Bad Request
		It("StartInspection should return 400 for empty VM list", func() {
			// Arrange
			body := `{"vcenterCredentials":{"url":"https://test","username":"user","password":"pass"},"vmIds":[]}`
			req := httptest.NewRequest(http.MethodPost, "/vms/inspector", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		// Given valid credentials and VM list
		// When we start an inspection
		// Then it should return 202 Accepted with initiating status
		It("StartInspection should start inspection successfully", func() {
			// Arrange
			body := `{"vcenterCredentials":{"url":"https://test","username":"user","password":"pass"},"vmIds":["vm-1"]}`
			req := httptest.NewRequest(http.MethodPost, "/vms/inspector", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusAccepted))
			var response v1.InspectorStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.State).To(Equal(v1.InspectorStatusStateInitiating))
		})

		// Given an invalid JSON request body
		// When we try to add VMs to inspection
		// Then it should return 400 Bad Request
		It("AddVMsToInspection should return 400 for invalid JSON", func() {
			// Arrange
			req := httptest.NewRequest(http.MethodPatch, "/vms/inspector", strings.NewReader("invalid json"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		// Given an empty VM list in the request
		// When we try to add VMs to inspection
		// Then it should return 400 Bad Request
		It("AddVMsToInspection should return 400 for empty VM list", func() {
			// Arrange
			body := `[]`
			req := httptest.NewRequest(http.MethodPatch, "/vms/inspector", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		// Given a running inspector and a valid VM list
		// When we add VMs to inspection
		// Then it should return 202 Accepted
		It("AddVMsToInspection should add VMs successfully", func() {
			// Arrange
			mockInspector.GetStatusResult = models.InspectorStatus{
				State: models.InspectorStateRunning,
			}
			body := `["vm-1","vm-2"]`
			req := httptest.NewRequest(http.MethodPatch, "/vms/inspector", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusAccepted))
		})

		// Given a running inspector
		// When we stop the inspection
		// Then it should return 202 Accepted
		It("StopInspection should stop inspector successfully", func() {
			// Arrange
			mockInspector.GetStatusResult = models.InspectorStatus{
				State: models.InspectorStateCanceled,
			}
			req := httptest.NewRequest(http.MethodDelete, "/vms/inspector", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusAccepted))
		})

		// Given a VM that has not been inspected
		// When we request its inspection status
		// Then it should return 404 Not Found
		It("GetVMInspectionStatus should return 404 for non-existent VM", func() {
			// Arrange
			mockInspector.GetVmStatusError = srvErrors.NewResourceNotFoundError("vm inspection status", "123")

			req := httptest.NewRequest(http.MethodGet, "/vms/123/inspector", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusNotFound))
			var response v1.VmInspectionStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.State).To(Equal(v1.VmInspectionStatusStateNotFound))
		})

		// Given a VM that has been inspected
		// When we request its inspection status
		// Then it should return the inspection status
		It("GetVMInspectionStatus should return VM status", func() {
			// Arrange
			mockInspector.GetVmStatusResult = models.InspectionStatus{
				State: models.InspectionStatePending,
			}

			req := httptest.NewRequest(http.MethodGet, "/vms/123/inspector", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			var response v1.VmInspectionStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.State).To(Equal(v1.VmInspectionStatusStatePending))
		})

		// Given a GetVmStatus internal error (not ResourceNotFoundError)
		// When we request inspection status
		// Then it should return 500 Internal Server Error
		It("GetVMInspectionStatus should return 500 for internal error", func() {
			// Arrange
			mockInspector.GetVmStatusError = errors.New("database error")

			req := httptest.NewRequest(http.MethodGet, "/vms/123/inspector", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})

		// Given a VM that has been cancelled
		// When we remove it from inspection
		// Then it should return 200 with the VM status
		It("RemoveVMFromInspection should return status on success", func() {
			// Arrange
			mockInspector.GetVmStatusResult = models.InspectionStatus{
				State: models.InspectionStateCanceled,
			}

			req := httptest.NewRequest(http.MethodDelete, "/vms/vm-1/inspector", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockInspector.CancelVmsInspectionCallCount).To(Equal(1))

			var response v1.VmInspectionStatus
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.State).To(Equal(v1.VmInspectionStatusStateCanceled))
		})

		// Given CancelVmsInspection returns an error
		// When we remove a VM from inspection
		// Then it should return 500 Internal Server Error
		It("RemoveVMFromInspection should return 500 when cancel fails", func() {
			// Arrange
			mockInspector.CancelVmsInspectionError = errors.New("cancel failed")

			req := httptest.NewRequest(http.MethodDelete, "/vms/vm-1/inspector", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})

		// Given cancel succeeds but GetVmStatus returns ResourceNotFoundError
		// When we remove a VM from inspection
		// Then it should return 404 Not Found
		It("RemoveVMFromInspection should return 404 when VM not found after cancel", func() {
			// Arrange
			mockInspector.GetVmStatusError = srvErrors.NewResourceNotFoundError("vm inspection status", "vm-1")

			req := httptest.NewRequest(http.MethodDelete, "/vms/vm-1/inspector", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusNotFound))
		})

		// Given cancel succeeds but GetVmStatus returns an internal error
		// When we remove a VM from inspection
		// Then it should return 500 Internal Server Error
		It("RemoveVMFromInspection should return 500 when GetVmStatus fails", func() {
			// Arrange
			mockInspector.GetVmStatusError = errors.New("database error")

			req := httptest.NewRequest(http.MethodDelete, "/vms/vm-1/inspector", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})

		// Given the inspector service returns an error on Stop
		// When we stop the inspection
		// Then it should return 500 Internal Server Error
		It("StopInspection should return 500 when stop fails", func() {
			// Arrange
			mockInspector.StopError = errors.New("stop failed")

			req := httptest.NewRequest(http.MethodDelete, "/vms/inspector", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})

		// Given the inspector service returns an error on Start
		// When we start the inspection with valid input
		// Then it should return 500 Internal Server Error
		It("StartInspection should return 500 when start fails", func() {
			// Arrange
			mockInspector.StartError = errors.New("start failed")
			body := `{"vcenterCredentials":{"url":"https://test","username":"user","password":"pass"},"vmIds":["vm-1"]}`
			req := httptest.NewRequest(http.MethodPost, "/vms/inspector", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})

		// Given the inspector service returns an error on Add
		// When we add VMs to inspection
		// Then it should return 400 Bad Request
		It("AddVMsToInspection should return 400 when add fails", func() {
			// Arrange
			mockInspector.AddError = errors.New("inspector not running")
			body := `["vm-1","vm-2"]`
			req := httptest.NewRequest(http.MethodPatch, "/vms/inspector", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})
	})
})

var _ = Describe("VMs Handlers Integration", func() {
	var (
		ctx     context.Context
		db      *sql.DB
		st      *store.Store
		vmSrv   *services.VMService
		handler *handlers.Handler
		router  *gin.Engine
	)

	BeforeEach(func() {
		ctx = context.Background()
		gin.SetMode(gin.TestMode)

		var err error
		db, err = store.NewDB(":memory:")
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db)

		// Migrate the store (creates vinfo, vdisk, concerns tables via parser.Init())
		err = st.Migrate(ctx)
		Expect(err).NotTo(HaveOccurred())

		// Insert test data
		err = test.InsertVMs(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		vmSrv = services.NewVMService(st)
		handler = handlers.New("", nil, nil, nil, vmSrv, nil)
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
			db.Close()
		}
	})

	Context("GetVMs with real data", func() {
		It("should return all 10 VMs", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?pageSize=50", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VMListResponse
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

			var page1 v1.VMListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &page1)).To(Succeed())
			Expect(page1.Page).To(Equal(1))
			Expect(page1.PageCount).To(Equal(4)) // 10 VMs / 3 per page = 4 pages
			Expect(page1.Total).To(Equal(10))
			Expect(page1.Vms).To(HaveLen(3))

			// Second page
			req = httptest.NewRequest(http.MethodGet, "/vms?page=2&pageSize=3", nil)
			w = httptest.NewRecorder()
			router.ServeHTTP(w, req)

			var page2 v1.VMListResponse
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

		It("should filter by cluster", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?clusters=production", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VMListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Total).To(Equal(4))
			for _, vm := range response.Vms {
				Expect(vm.Cluster).To(Equal("production"))
			}
		})

		It("should filter by multiple clusters", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?clusters=production&clusters=staging", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VMListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Total).To(Equal(7)) // 4 production + 3 staging
			for _, vm := range response.Vms {
				Expect(vm.Cluster).To(BeElementOf("production", "staging"))
			}
		})

		It("should filter by power state", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?status=poweredOff", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VMListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Total).To(Equal(2)) // vm-004 and vm-009
			for _, vm := range response.Vms {
				Expect(vm.VCenterState).To(Equal("poweredOff"))
			}
		})

		It("should filter by minimum issues", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?minIssues=2", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VMListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Total).To(Equal(2)) // vm-003 (2 issues) and vm-007 (3 issues)
			for _, vm := range response.Vms {
				Expect(vm.IssueCount).To(BeNumerically(">=", 2))
			}
		})

		It("should filter by disk size range", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?diskSizeMin=100&diskSizeMax=250", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VMListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			for _, vm := range response.Vms {
				Expect(vm.DiskSize).To(BeNumerically(">=", 100))
				Expect(vm.DiskSize).To(BeNumerically("<", 250))
			}
		})

		It("should filter by memory size range", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?memorySizeMin=8000&memorySizeMax=20000", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VMListResponse
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

			var response v1.VMListResponse
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

			var response v1.VMListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Vms).To(HaveLen(10))
			Expect(response.Vms[0].Memory).To(Equal(int64(16384)))
		})

		It("should sort by issues descending", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?sort=issues:desc&pageSize=50", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VMListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Vms[0].IssueCount).To(Equal(3)) // vm-007 has 3 issues
		})

		It("should combine cluster filter with pagination", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?clusters=production&page=1&pageSize=2", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VMListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Total).To(Equal(4))
			Expect(response.Vms).To(HaveLen(2))
			Expect(response.PageCount).To(Equal(2))
			for _, vm := range response.Vms {
				Expect(vm.Cluster).To(Equal("production"))
			}
		})

		It("should combine multiple filters", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms?clusters=production&status=poweredOn", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VMListResponse
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

			var response v1.VMListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())

			// Find vm-003 which has 2 disks of 500 MiB each
			var vm003 *v1.VM
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

			var response v1.VMListResponse
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

			var response v1.VMDetails
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

			var response v1.VMDetails
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Disks).To(HaveLen(2))
			Expect(*response.Disks[0].Capacity).To(Equal(int64(500 * 1024 * 1024 * 1024 * 1024)))
		})

		It("should return VM with NICs", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms/vm-003", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VMDetails
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Nics).To(HaveLen(2))
		})

		It("should return VM with issues", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms/vm-007", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))

			var response v1.VMDetails
			Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Issues).NotTo(BeNil())
			Expect(*response.Issues).To(HaveLen(3))
		})
	})
})
