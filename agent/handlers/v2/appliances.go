package v2

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// ListAppliances returns detected appliances for a collection.
// (GET /collections/{id}/appliances)
func (h *Handler) ListAppliances(c *gin.Context, id string) {
	applianceSvc, err := h.svc.ApplianceService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	noFilter := ""
	appliances, err := applianceSvc.List(c.Request.Context(), noFilter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list appliances: %v", err)})
		return
	}

	c.JSON(http.StatusOK, v2.ApplianceListResponse{
		Appliances: v2.NewApplianceList(appliances),
	})
}

// ListGroupAppliances returns detected appliances for a group.
// (GET /groups/{groupId}/appliances)
func (h *Handler) ListGroupAppliances(c *gin.Context, groupId string) {
	gid, err := uuid.Parse(groupId)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group ID"})
		return
	}

	groupSvc, err := h.svc.LatestGroupService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	appliances, err := groupSvc.ListAppliances(c.Request.Context(), gid)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.ApplianceListResponse{
		Appliances: v2.NewApplianceList(appliances),
	})
}
