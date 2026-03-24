package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/kubev2v/assisted-migration-agent/pkg/filter"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var validSortFields = map[string]bool{
	"name":         true,
	"vCenterState": true,
	"cluster":      true,
	"diskSize":     true,
	"memory":       true,
	"issues":       true,
}

const (
	defaultPageSize      = 20
	maxPageSize          = 100
	maxDescriptionLength = 500
)

// GetVMs returns the list of VMs with filtering and pagination
// (GET /vms)
func (h *Handler) GetVMs(c *gin.Context, params v1.GetVMsParams) {
	// Parse pagination
	page := 1
	if params.Page != nil && *params.Page > 0 {
		page = *params.Page
	}
	pageSize := defaultPageSize
	if params.PageSize != nil && *params.PageSize > 0 {
		pageSize = min(*params.PageSize, maxPageSize)
	}

	// Build service params
	svcParams := services.VMListParams{
		Limit:  uint64(pageSize),
		Offset: uint64((page - 1) * pageSize),
	}

	if params.ByExpression != nil {
		// validate expression
		if _, err := filter.ParseWithDefaultMap([]byte(*params.ByExpression)); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("expression filter is invalid: %v", err)})
			return
		}
		svcParams.Expression = *params.ByExpression
	}

	// Parse and validate sort params
	if params.Sort != nil {
		for _, s := range *params.Sort {
			parts := strings.SplitN(s, ":", 2)
			if len(parts) != 2 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sort format, expected 'field:direction' (e.g., 'name:asc')"})
				return
			}
			field, direction := parts[0], parts[1]
			if !validSortFields[field] {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sort field: " + field})
				return
			}
			if direction != "asc" && direction != "desc" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sort direction: " + direction + ", must be 'asc' or 'desc'"})
				return
			}
			svcParams.Sort = append(svcParams.Sort, services.SortField{Field: field, Desc: direction == "desc"})
		}
	}

	vms, total, err := h.vmSrv.List(c.Request.Context(), svcParams)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list VMs: %v", err)})
		return
	}

	// Calculate page count
	pageCount := (total + pageSize - 1) / pageSize
	if pageCount == 0 {
		pageCount = 1
	}

	// Get inspection status and Map to API response
	apiVMs := make([]v1.VirtualMachine, 0, len(vms))
	for _, vm := range vms {
		vm.Status = h.inspectorSrv.GetVmStatus(vm.ID)
		apiVMs = append(apiVMs, v1.NewVirtualMachineFromSummary(vm))
	}

	c.JSON(http.StatusOK, v1.VirtualMachineListResponse{
		Page:      page,
		PageCount: pageCount,
		Total:     total,
		Vms:       apiVMs,
	})
}

// GetVM returns details for a specific VM
// (GET /vms/{id})
func (h *Handler) GetVM(c *gin.Context, id string) {
	vm, err := h.vmSrv.Get(c.Request.Context(), id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v1.NewVirtualMachineDetailFromModel(*vm))
}

// AddVMToInspection add VM to inspection queue
// (POST /vms/{id}/inspection)
func (h *Handler) AddVMToInspection(c *gin.Context, id string) {
	if err := h.inspectorSrv.Add(id); err != nil {
		if srvErrors.IsInspectorNotRunningError(err) || srvErrors.IsInspectionLimitReachedError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, v1.NewInspectorStatus(h.inspectorSrv.GetStatus()))
}

// RemoveVMFromInspection removes VM from inspection queue
// (DELETE /vms/{id}/inspection)
func (h *Handler) RemoveVMFromInspection(c *gin.Context, id string) {
	if err := h.inspectorSrv.Cancel(id); err != nil {
		if srvErrors.IsInspectorNotRunningError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v1.NewInspectionStatus(h.inspectorSrv.GetVmStatus(id)))
}
