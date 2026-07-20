package v2

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var (
	clusterIDPattern = regexp.MustCompile(`^(domain-c\d+|cluster-[0-9a-f]{16})$`)
	vmIDPattern      = regexp.MustCompile(`^(vm-[0-9a-f]{16})$`)
)

// GetClusterUtilization returns utilization for a specific cluster from the latest completed report.
// (GET /collections/{id}/clusters/{clusterId}/utilization)
func (h *Handler) GetClusterUtilization(c *gin.Context, id string, clusterId string) {
	if !clusterIDPattern.MatchString(clusterId) {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid cluster_id format: %q", clusterId)})
		return
	}

	rsSvc, err := h.svc.RightsizingService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	reportID, clusters, err := rsSvc.ListLatestClusterUtilization(c.Request.Context(), clusterId)
	if err != nil {
		zap.S().Named("rightsizing_handler").Errorw("failed to get latest cluster utilization", "collection_id", id, "cluster_id", clusterId, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if reportID == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no completed rightsizing report found"})
		return
	}
	if len(clusters) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "cluster not found in report"})
		return
	}
	c.JSON(http.StatusOK, v2.RightsizingClusterResponse{
		ReportId: reportID,
		Cluster:  v2.NewRightsizingClusterUtilizationFromModel(clusters[0]),
	})
}

// GetVMUtilization returns utilization details for a specific VM.
// (GET /collections/{id}/virtualmachines/{vmId}/utilization)
func (h *Handler) GetVMUtilization(c *gin.Context, id string, vmId string) {
	if !vmIDPattern.MatchString(vmId) {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid vm_id format: %q", vmId)})
		return
	}

	rsSvc, err := h.svc.RightsizingService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	details, err := rsSvc.GetVMUtilization(c.Request.Context(), vmId)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		zap.S().Named("rightsizing_handler").Errorw("failed to get VM utilization", "collection_id", id, "vm_id", vmId, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, v2.NewVmUtilizationDetailsFromModel(*details))
}
