package v1

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
	"cpuUsage":     true,
	"diskUsage":    true,
	"ramUsage":     true,
	"cpuAvg":       true,
	"memAvg":       true,
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

	apiVMs := make([]v1.VirtualMachine, 0, len(vms))
	for _, vm := range vms {
		apiVMs = append(apiVMs, v1.NewVirtualMachineFromSummary(vm))
	}

	c.JSON(http.StatusOK, v1.VirtualMachineListResponse{
		Page:      page,
		PageCount: pageCount,
		Total:     total,
		Vms:       apiVMs,
	})
}

// GetVMsFilterOptions returns distinct filter option values
// (GET /vms/filter-options)
func (h *Handler) GetVMsFilterOptions(c *gin.Context) {
	opts, err := h.vmSrv.GetFilterOptions(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get filter options: %v", err)})
		return
	}

	c.JSON(http.StatusOK, v1.NewVMFilterOptionsFromModel(opts))
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

// RemoveVMFromInspection removes VM from inspection queue
// (DELETE /vms/{id}/inspection)
func (h *Handler) RemoveVMFromInspection(c *gin.Context, id string) {
	if err := h.inspectorSrv.Cancel(id); err != nil {
		if srvErrors.IsInspectorNotRunningError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

// UpdateVM updates VM properties
// (PATCH /vms/{id})
func (h *Handler) UpdateVM(c *gin.Context, id string) {
	var req v1.VirtualMachineUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	// Only migrationExcluded is supported for now
	if req.MigrationExcluded != nil {
		if err := h.vmSrv.UpdateMigrationExcluded(c.Request.Context(), id, *req.MigrationExcluded); err != nil {
			if srvErrors.IsResourceNotFoundError(err) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// Handle labels updates
	if req.Labels != nil {
		if err := h.vmSrv.UpdateLabels(c.Request.Context(), id, *req.Labels); err != nil {
			if srvErrors.IsResourceNotFoundError(err) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			if srvErrors.IsValidationError(err) {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.Status(http.StatusOK)
}

// BatchUpdateVMExclusion updates migration exclusion for multiple VMs
// (POST /vms/batch-update-exclusion)
func (h *Handler) BatchUpdateVMExclusion(c *gin.Context) {
	var req v1.BatchUpdateExclusionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	if len(req.VmIds) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "vmIds array cannot be empty"})
		return
	}

	if err := h.vmSrv.UpdateMigrationExcludedBatch(c.Request.Context(), req.VmIds, req.MigrationExcluded); err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

// GetVMLabels returns all distinct labels in use across VMs along with their counts
// (GET /vms/labels)
func (h *Handler) GetVMLabels(c *gin.Context) {
	labels, counts, err := h.vmSrv.GetAllLabels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get labels: %v", err)})
		return
	}

	c.JSON(http.StatusOK, v1.VMLabelsResponse{
		Labels: labels,
		Counts: counts,
	})
}

// UpdateLabelVMs modifies label VM membership (add/remove label to/from VMs)
// (PATCH /vms/labels/{label})
func (h *Handler) UpdateLabelVMs(c *gin.Context, label string) {
	// Validate label parameter
	if strings.TrimSpace(label) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "label cannot be empty or whitespace-only"})
		return
	}
	if len(label) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "label exceeds maximum length of 100 characters"})
		return
	}

	var req v1.UpdateLabelVMsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	// At least one of 'add' or 'remove' must be present
	if (req.Add == nil || len(*req.Add) == 0) && (req.Remove == nil || len(*req.Remove) == 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one of 'add' or 'remove' must be provided with VM IDs"})
		return
	}

	// Prepare VM ID lists
	var addVMIDs, removeVMIDs []string
	if req.Add != nil {
		addVMIDs = *req.Add
	}
	if req.Remove != nil {
		removeVMIDs = *req.Remove
	}

	// Execute atomic update (all-or-nothing)
	err := h.vmSrv.UpdateLabelVMs(c.Request.Context(), addVMIDs, removeVMIDs, label)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if srvErrors.IsValidationError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Success - all operations completed atomically
	c.Status(http.StatusOK)
}

// DeleteLabelGlobally removes a label from all VMs in the system
// (DELETE /vms/labels/{label})
func (h *Handler) DeleteLabelGlobally(c *gin.Context, label string) {
	// Validate label parameter
	if strings.TrimSpace(label) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "label cannot be empty or whitespace-only"})
		return
	}
	if len(label) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "label exceeds maximum length of 100 characters"})
		return
	}

	affected, err := h.vmSrv.RemoveLabelFromAllVMs(c.Request.Context(), label)
	if err != nil {
		if srvErrors.IsValidationError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v1.DeleteLabelGloballyResponse{
		Affected: affected,
		Label:    label,
	})
}
