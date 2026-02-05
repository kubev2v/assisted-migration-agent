package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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
		handler = handlers.New("", nil, nil, mockInventory, nil, nil)
		router = gin.New()
		router.GET("/inventory", handler.GetInventory)
	})

	Describe("GetInventory", func() {
		// Given inventory data exists in the store
		// When we request the inventory
		// Then it should return the inventory data as JSON
		It("should return inventory data", func() {
			// Arrange
			inventoryData := []byte(`{"vms":[{"id":"vm-1","name":"Test VM"}]}`)
			mockInventory.InventoryResult = &models.Inventory{Data: inventoryData}

			req := httptest.NewRequest(http.MethodGet, "/inventory", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Header().Get("Content-Type")).To(Equal("application/json"))
			Expect(w.Body.Bytes()).To(Equal(inventoryData))
		})

		// Given no inventory has been collected yet
		// When we request the inventory
		// Then it should return 404 Not Found
		It("should return 404 when inventory not found", func() {
			// Arrange
			mockInventory.InventoryError = srvErrors.NewResourceNotFoundError("inventory not collected yet")

			req := httptest.NewRequest(http.MethodGet, "/inventory", nil)
			w := httptest.NewRecorder()

			// Act
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusNotFound))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("inventory not collected yet"))
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
			Expect(response["error"]).To(ContainSubstring("failed to get inventory"))
		})
	})
})
