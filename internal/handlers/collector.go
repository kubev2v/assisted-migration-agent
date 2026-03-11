package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// GetCollectorStatus returns the collector status
// (GET /collector)
func (h *Handler) GetCollectorStatus(c *gin.Context) {
	status := h.collectorSrv.GetStatus()
	c.JSON(http.StatusOK, v1.NewCollectorStatus(status))
}

// StartCollector starts inventory collection
// (POST /collector)
func (h *Handler) StartCollector(c *gin.Context) {
	var req v1.CollectorStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	creds := &models.Credentials{
		URL:      req.Url,
		Username: req.Username,
		Password: req.Password,
	}

	if err := h.collectorSrv.Start(c.Request.Context(), creds); err != nil {
		if srvErrors.IsResourceInProgressError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	status := h.collectorSrv.GetStatus()
	c.JSON(http.StatusAccepted, v1.NewCollectorStatus(status))
}

// StopCollector stops the collection but keeps credentials for retry
// (DELETE /collector)
func (h *Handler) StopCollector(c *gin.Context) {
	h.collectorSrv.Stop()

	status := h.collectorSrv.GetStatus()
	c.JSON(http.StatusOK, v1.NewCollectorStatus(status))
}
