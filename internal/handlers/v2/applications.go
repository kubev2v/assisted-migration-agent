package v2

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// ListApplications returns detected applications for a collection.
// (GET /collections/{id}/applications)
func (h *Handler) ListApplications(c *gin.Context, id string) {
	appSvc, err := h.svc.ApplicationService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	noFilter := ""
	apps, err := appSvc.List(c.Request.Context(), noFilter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list applications: %v", err)})
		return
	}

	c.JSON(http.StatusOK, v2.ApplicationListResponse{
		Applications: v2.NewApplicationList(apps),
	})
}
