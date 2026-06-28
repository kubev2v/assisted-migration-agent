package v1

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
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
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if req.Url != "" {
		if _, err := url.ParseRequestURI(req.Url); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Url must be a valid URL"})
			return
		}
		if req.Username == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username is required"})
			return
		}
		if req.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password is required"})
			return
		}
		creds, err := v1.CredsFromAPI(req)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := creds.Validate(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if _, err := h.credentialsSrv.Store(c.Request.Context(), creds); err != nil {
			if srvErrors.IsVCenterError(err) || srvErrors.IsValidationError(err) {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store credentials"})
			return
		}
	}

	if err := h.collectorSrv.Start(c.Request.Context()); err != nil {
		if srvErrors.IsOperationInProgressError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if srvErrors.IsCredentialsNotSetError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "credentials required: provide inline or store via PUT /credentials"})
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
