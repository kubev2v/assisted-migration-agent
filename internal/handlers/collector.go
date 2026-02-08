package handlers

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Validate required fields
	if req.Credentials.Url == "" || req.Credentials.Username == "" || req.Credentials.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url, username, and password are required"})
		return
	}

	// Validate URL format
	parsedURL, err := url.Parse(req.Credentials.Url)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid url format"})
		return
	}

	creds := &models.Credentials{
		URL:      req.Credentials.Url,
		Username: req.Credentials.Username,
		Password: req.Credentials.Password,
	}

	// Start collection (saves creds, verifies, starts async job)
	if err := h.collectorSrv.Start(c.Request.Context(), creds); err != nil {
		switch err.(type) {
		case *srvErrors.CollectionInProgressError:
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			zap.S().Named("collector_handler").Errorw("failed to start collector", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start collector"})
		}
		return
	}

	// Return current state after starting
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
