package handlers

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// GetCollectorStatus returns the collector status
// (GET /collector)
func (h *Handler) GetCollectorStatus(c *gin.Context) {
	status := h.collector.GetStatus(c.Request.Context())

	resp := v1.CollectorStatus{
		Status:         mapStateToAPIStatus(status.State),
		HasCredentials: status.HasCredentials,
	}
	if status.Error != "" {
		resp.Error = &status.Error
	}

	c.JSON(http.StatusOK, resp)
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
	if req.Url == "" || req.Username == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url, username, and password are required"})
		return
	}

	// Validate URL format
	parsedURL, err := url.Parse(req.Url)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid url format"})
		return
	}

	creds := &models.Credentials{
		URL:      req.Url,
		Username: req.Username,
		Password: req.Password,
	}

	// Start collection (saves creds, verifies, starts async job)
	if err := h.collector.Start(c.Request.Context(), creds); err != nil {
		zap.S().Errorw("failed to start collector", "error", err)

		if errors.Is(err, services.ErrCollectionInProgress) {
			c.JSON(http.StatusConflict, gin.H{"error": "collection already in progress"})
			return
		}
		if errors.Is(err, services.ErrInvalidCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid vCenter credentials"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start collector"})
		return
	}

	// Return current state after starting
	status := h.collector.GetStatus(c.Request.Context())
	c.JSON(http.StatusAccepted, v1.CollectorStatus{
		Status:         mapStateToAPIStatus(status.State),
		HasCredentials: status.HasCredentials,
	})
}

// GetInventory returns the collected inventory
// (GET /collector/inventory)
func (h *Handler) GetInventory(c *gin.Context) {
	inv, err := h.collector.GetInventory(c.Request.Context())
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		zap.S().Errorw("failed to get inventory", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get inventory"})
		return
	}

	// Return the raw inventory JSON data
	c.Data(http.StatusOK, "application/json", inv.Data)
}

// StopCollector stops the collection but keeps credentials for retry
// (DELETE /collector)
func (h *Handler) StopCollector(c *gin.Context) {
	if err := h.collector.Stop(c.Request.Context()); err != nil {
		zap.S().Errorw("failed to stop collector", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to stop collector"})
		return
	}

	status := h.collector.GetStatus(c.Request.Context())
	c.JSON(http.StatusOK, v1.CollectorStatus{
		Status:         mapStateToAPIStatus(status.State),
		HasCredentials: status.HasCredentials,
	})
}

// mapStateToAPIStatus converts internal state to API status.
func mapStateToAPIStatus(state models.CollectorState) v1.CollectorStatusStatus {
	switch state {
	case models.CollectorStateReady:
		return v1.CollectorStatusStatusReady
	case models.CollectorStateConnecting:
		return v1.CollectorStatusStatusConnecting
	case models.CollectorStateConnected:
		return v1.CollectorStatusStatusConnected
	case models.CollectorStateCollecting:
		return v1.CollectorStatusStatusCollecting
	case models.CollectorStateCollected:
		return v1.CollectorStatusStatusCollected
	case models.CollectorStateError:
		return v1.CollectorStatusStatusError
	default:
		return v1.CollectorStatusStatusReady
	}
}
