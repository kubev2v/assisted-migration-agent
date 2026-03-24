package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// StartInspection starts inspection for VMs
// (POST /inspector)
func (h *Handler) StartInspection(c *gin.Context) {
	var req v1.InspectorStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	if len(req.VmIds) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "vmIds is required"})
		return
	}

	cred := &models.Credentials{
		URL:      req.VcenterCredentials.Url,
		Username: req.VcenterCredentials.Username,
		Password: req.VcenterCredentials.Password,
	}

	if err := h.inspectorSrv.Start(c.Request.Context(), req.VmIds, cred); err != nil {
		if srvErrors.IsOperationInProgressError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if srvErrors.IsInspectionLimitReachedError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to start inspector: %v", err)})
		return
	}

	c.JSON(http.StatusAccepted, v1.InspectorStatus{State: v1.InspectorStatusStateInitiating})
}

// GetInspectorStatus returns the inspector status
// (GET /inspector)
func (h *Handler) GetInspectorStatus(c *gin.Context) {
	c.JSON(http.StatusOK, v1.NewInspectorStatus(h.inspectorSrv.GetStatus()))
}

// StopInspection stops inspector entirely
// (DELETE /inspector)
func (h *Handler) StopInspection(c *gin.Context) {
	if err := h.inspectorSrv.Stop(); err != nil {
		if srvErrors.IsInspectorNotRunningError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, v1.NewInspectorStatus(h.inspectorSrv.GetStatus()))
}
