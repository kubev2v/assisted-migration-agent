package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"go.uber.org/zap"
)

// GetInventory returns the collected inventory
// (GET /inventory)
func (h *Handler) GetInventory(c *gin.Context) {
	inv, err := h.inventorySrv.GetInventory(c.Request.Context())
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		zap.S().Named("collector_handler").Errorw("failed to get inventory", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get inventory"})
		return
	}

	c.Data(http.StatusOK, "application/json", inv.Data)
}
