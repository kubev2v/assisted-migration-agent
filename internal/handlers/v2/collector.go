package v2

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// StartCollector creates and starts a new collector.
// (POST /collector)
func (h *Handler) StartCollector(c *gin.Context) {
	status, err := h.svc.StartCollecting(c.Request.Context())
	if err != nil {
		if srvErrors.IsOperationInProgressError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if srvErrors.IsCredentialsNotSetError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "credentials required: store via PUT /credentials first"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, v2.NewCollectorStatus(status))
}

// GetCollectorStatus returns the status of a specific collector.
// (GET /collector)
func (h *Handler) GetCollectorStatus(c *gin.Context) {
	c.JSON(http.StatusOK, v2.NewCollectorStatus(h.svc.GetCollectorStatus()))
}

// StopCollector stops and removes a specific collector.
// (DELETE /collector)
func (h *Handler) StopCollector(c *gin.Context) {
	if err := h.svc.StopCollecting(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}
