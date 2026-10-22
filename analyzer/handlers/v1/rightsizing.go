package analyzer

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	services "github.com/kubev2v/assisted-migration-agent/internal/services"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var (
	vmIDPattern = regexp.MustCompile(`^(vm-[0-9]+)$`)
)

// GetVMUtilization returns utilization details for a specific VM.
// (GET /collections/{id}/virtualmachines/{vmId}/utilization)
func (h *Handler) GetVMUtilization(c *gin.Context, id string, vmId string) {
	rsSvc, err := h.svc.RightsizingService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.getVMUtilization(c, rsSvc, vmId)
}

// GetLatestVMUtilization returns utilization details for a VM from the latest collection.
// (GET /virtualmachines/{vmId}/utilization)
func (h *Handler) GetLatestVMUtilization(c *gin.Context, vmId string) {
	rsSvc, err := h.svc.LatestRightsizingService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.getVMUtilization(c, rsSvc, vmId)
}

// ── Private shared logic ───────────────────────────────────────────────

func (h *Handler) getVMUtilization(c *gin.Context, rsSvc *services.RightsizingService, vmId string) {
	if !vmIDPattern.MatchString(vmId) {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid vm_id format: %q", vmId)})
		return
	}

	details, err := rsSvc.GetVMUtilization(c.Request.Context(), vmId)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		zap.S().Named("rightsizing_handler").Errorw("failed to get VM utilization", "vm_id", vmId, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, v1.NewVmUtilizationDetailsFromModel(*details))
}
