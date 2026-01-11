package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
)

// GetVMs returns the list of VMs with filtering and pagination
// (GET /vms)
func (h *Handler) GetVMs(c *gin.Context, params v1.GetVMsParams) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not yet implemented"})
}

// GetVMInspectionStatus returns the inspection status for a specific VM
// (GET /vms/{id}/inspector)
func (h *Handler) GetVMInspectionStatus(c *gin.Context, id int) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not yet implemented"})
}

// GetInspectorStatus returns the inspector status
// (GET /vms/inspector)
func (h *Handler) GetInspectorStatus(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not yet implemented"})
}

// StartInspection starts inspection for VMs
// (POST /vms/inspector)
func (h *Handler) StartInspection(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not yet implemented"})
}

// AddVMsToInspection adds more VMs to inspection queue
// (PATCH /vms/inspector)
func (h *Handler) AddVMsToInspection(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not yet implemented"})
}

// RemoveVMsFromInspection removes VMs from inspection queue or stops inspector entirely
// (DELETE /vms/inspector)
func (h *Handler) RemoveVMsFromInspection(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not yet implemented"})
}
