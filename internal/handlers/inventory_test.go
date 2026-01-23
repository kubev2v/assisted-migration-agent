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
		handler = handlers.New("", nil, nil, mockInventory, nil)
		router = gin.New()
		router.GET("/inventory", handler.GetInventory)
	})

	Describe("GetInventory", func() {
		It("should return inventory data", func() {
			inventoryData := []byte(`{"vms":[{"id":"vm-1","name":"Test VM"}]}`)
			mockInventory.InventoryResult = &models.Inventory{Data: inventoryData}

			req := httptest.NewRequest(http.MethodGet, "/inventory", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Header().Get("Content-Type")).To(Equal("application/json"))
			Expect(w.Body.Bytes()).To(Equal(inventoryData))
		})

		It("should return 404 when inventory not found", func() {
			mockInventory.InventoryError = srvErrors.NewResourceNotFoundError("inventory not collected yet")

			req := httptest.NewRequest(http.MethodGet, "/inventory", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusNotFound))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("inventory not collected yet"))
		})

		It("should return 500 for other errors", func() {
			mockInventory.InventoryError = errors.New("database error")

			req := httptest.NewRequest(http.MethodGet, "/inventory", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response["error"]).To(ContainSubstring("failed to get inventory"))
		})
	})
})
