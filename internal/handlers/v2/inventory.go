package v2

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kubev2v/migration-planner/api/v1alpha1"

	services "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// GetInventory returns the collected inventory for a collection.
// (GET /collections/{id}/inventory)
func (h *Handler) GetInventory(c *gin.Context, id string) {
	invSvc, err := h.svc.InventoryService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.getInventory(c, invSvc)
}

// GetLatestInventory returns the inventory from the latest collection.
// (GET /inventory)
func (h *Handler) GetLatestInventory(c *gin.Context) {
	invSvc, err := h.svc.LatestInventoryService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.getInventory(c, invSvc)
}

// ── Private shared logic ───────────────────────────────────────────────

func (h *Handler) getInventory(c *gin.Context, invSvc *services.InventoryService) {
	inv, err := invSvc.GetInventory(c.Request.Context())
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		zap.S().Named("inventory_handler").Errorw("failed to get inventory", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var inventory v1alpha1.Inventory
	if err := json.Unmarshal(inv.Data, &inventory); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error unmarshalling inventory: %v", err)})
		return
	}

	c.JSON(http.StatusOK, inventory)
}
