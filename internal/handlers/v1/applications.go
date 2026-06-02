package v1

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
)

// GetApplications returns an overview of detected applications running on VMs
// (GET /applications)
func (h *Handler) GetApplications(c *gin.Context) {
	apps, err := h.appSrv.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list applications: %v", err)})
		return
	}

	apiApps := make([]v1.ApplicationOverview, 0, len(apps))
	for _, app := range apps {
		vms := make([]v1.ApplicationVM, 0, len(app.VMs))
		for _, vm := range app.VMs {
			vms = append(vms, v1.ApplicationVM{Id: vm.ID, Name: vm.Name})
		}
		apiApps = append(apiApps, v1.ApplicationOverview{
			Name:        app.Name,
			Description: app.Description,
			VmCount:     app.VMCount,
			Vms:         vms,
		})
	}

	c.JSON(http.StatusOK, v1.ApplicationListResponse{
		Applications: apiApps,
	})
}
