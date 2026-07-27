package v2

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// StartCollector creates and starts a new collector.
// (POST /collector)
func (h *Handler) StartCollector(c *gin.Context) {
	collector, err := h.svc.CreateCollector()
	if err != nil {
		if srvErrors.IsOperationInProgressError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := collector.Start(c.Request.Context()); err != nil {
		if srvErrors.IsCredentialsNotSetError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "credentials required: store via PUT /credentials first"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, v2.NewCollectorStatus(collector.GetStatus()))
}

// GetCollectorStatus returns the status of a specific collector.
// If not found, return "ready"
// (GET /collector)
func (h *Handler) GetCollectorStatus(c *gin.Context) {
	svc, err := h.svc.GetCollector()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusOK, v2.NewCollectorStatus(models.CollectorStatus{State: models.CollectorStateReady}))
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.NewCollectorStatus(svc.GetStatus()))
}

// StopCollector stops and removes a specific collector.
// (DELETE /collector)
func (h *Handler) StopCollector(c *gin.Context) {
	if err := h.svc.StopCollector(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}
