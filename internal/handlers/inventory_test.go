package handlers_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"

	"github.com/google/uuid"
	"github.com/kubev2v/migration-planner/api/v1alpha1"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/handlers"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("Inventory Handlers", func() {
	var (
		mockInventory *MockInventoryService
		handler       *handlers.Handler
		router        *gin.Engine
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		mockInventory = &MockInventoryService{}
		handler = handlers.New(config.Configuration{
			Agent: config.Agent{
				ID: uuid.Nil.String(),
			},
		}, nil, nil, mockInventory, nil, nil)
		router = gin.New()
		wrapper := v1.ServerInterfaceWrapper{
			Handler:      handler,
			ErrorHandler: func(c *gin.Context, err error, statusCode int) { c.JSON(statusCode, gin.H{"msg": err.Error()}) },
		}
		router.GET("/inventory", wrapper.GetInventory)
	})

	Context("GetInventory", func() {
		// Given inventory data exists in the store
		// When we request the inventory
		// Then it should return the inventory data as JSON
		It("Schema1: should return inventory data", func() {
			vcenterID := "502d878c-af91-4a6f-93e9-61c4a1986172"
			// Arrange
			inventoryData := []byte(fmt.Sprintf(`{"clusters": {}, "vcenter": {}, "vcenter_id": "%s"}`, vcenterID))
			mockInventory.InventoryResult = &models.Inventory{Data: inventoryData}

			req := httptest.NewRequest(http.MethodGet, "/inventory", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Header().Get("Content-Type")).To(Equal("application/json; charset=utf-8"))

			var result v1alpha1.Inventory
			resBody := w.Body.Bytes()
			err := json.Unmarshal(resBody, &result)
			Expect(err).To(BeNil())

			Expect(result.VcenterId).To(Equal(vcenterID))
		})

		It("Schema2: should return inventory with agent id", func() {
			vcenterID := "502d878c-af91-4a6f-93e9-61c4a1986172"
			// Arrange
			inventoryData := []byte(fmt.Sprintf(`{"clusters": {}, "vcenter": {}, "vcenter_id": "%s"}`, vcenterID))
			mockInventory.InventoryResult = &models.Inventory{Data: inventoryData}

			req := httptest.NewRequest(http.MethodGet, "/inventory?withAgentId=true", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Header().Get("Content-Type")).To(Equal("application/json; charset=utf-8"))

			var result v1alpha1.UpdateInventory
			resBody := w.Body.Bytes()
			err := json.Unmarshal(resBody, &result)
			Expect(err).To(BeNil())

			Expect(result.Inventory.VcenterId).To(Equal(vcenterID))
		})

		// Given no inventory has been collected yet
		// When we request the inventory
		// Then it should return 404 Not Found
		It("should return 404 when inventory not found", func() {
			// Arrange
			mockInventory.InventoryError = srvErrors.NewResourceNotFoundError("inventory", "")

			req := httptest.NewRequest(http.MethodGet, "/inventory", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusNotFound))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("inventory not found"))
		})

		// Given an internal error occurs when fetching inventory
		// When we request the inventory
		// Then it should return 500 Internal Server Error
		It("should return 500 for other errors", func() {
			// Arrange
			mockInventory.InventoryError = errors.New("database error")

			req := httptest.NewRequest(http.MethodGet, "/inventory", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusInternalServerError))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("database error"))
		})
	})
})
