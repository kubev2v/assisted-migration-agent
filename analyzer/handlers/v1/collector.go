package analyzer

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	"go.uber.org/zap"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
)

// GetCollectorStatus returns the status of a specific collector.
// (GET /collector)
func (h *Handler) GetCollectorStatus(c *gin.Context) {
	c.JSON(http.StatusOK, v1.NewCollectorStatus(h.svc.GetCollectorStatus()))
}

// StopCollector stops and removes a specific collector.
// (DELETE /collector)
func (h *Handler) StopCollector(c *gin.Context) {
	if err := h.svc.StopCollecting(); err != nil {
		zap.S().Errorw("failed to stop RVTools collection", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": duckdb_parser.RVToolsFailureMessage})
		return
	}

	c.Status(http.StatusNoContent)
}
