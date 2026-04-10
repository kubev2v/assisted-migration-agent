package v1

import (
	"encoding/json"
	"fmt"
	"net/http"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/kubev2v/migration-planner/api/v1alpha1"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// GetInventory returns the collected inventory
// (GET /inventory)
func (h *Handler) GetInventory(c *gin.Context, params v1.GetInventoryParams) {
	inv, err := h.inventorySrv.GetInventory(c.Request.Context())
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Errorf("error unmarshalling inventory: %w", err)})
		return
	}

	withAgentId := false
	if params.WithAgentId != nil {
		withAgentId = *params.WithAgentId
	}

	// Return inventory without agent ID
	if !withAgentId {
		c.JSON(http.StatusOK, inventory)
		return
	}

	// With Agent ID
	payload := &v1alpha1.UpdateInventory{
		Inventory: inventory,
		AgentId:   uuid.MustParse(h.cfg.Agent.ID),
	}
	c.JSON(http.StatusOK, payload)
}
