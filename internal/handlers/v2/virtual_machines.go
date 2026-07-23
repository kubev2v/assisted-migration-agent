package v2

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	services "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/filter"
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
	defaultPageSize = 20
	maxPageSize     = 100
)

// ListVirtualMachines returns VMs for a collection with filtering, sorting, and pagination.
// (GET /collections/{id}/virtualmachines)
func (h *Handler) ListVirtualMachines(c *gin.Context, id string, params v2.ListVirtualMachinesParams) {
	vmSvc, err := h.svc.VirtualMachineService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.listVirtualMachines(c, vmSvc, params.ByExpression, params.Sort, params.Page, params.PageSize)
}

// ListLatestVirtualMachines returns VMs from the latest collection.
// (GET /virtualmachines)
func (h *Handler) ListLatestVirtualMachines(c *gin.Context, params v2.ListLatestVirtualMachinesParams) {
	vmSvc, err := h.svc.LatestVirtualMachineService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.listVirtualMachines(c, vmSvc, params.ByExpression, params.Sort, params.Page, params.PageSize)
}

// GetVirtualMachine returns details for a specific VM in a collection.
// (GET /collections/{id}/virtualmachines/{vmId})
func (h *Handler) GetVirtualMachine(c *gin.Context, id string, vmId string) {
	vmSvc, err := h.svc.VirtualMachineService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.getVirtualMachine(c, vmSvc, vmId)
}

// GetLatestVirtualMachine returns details for a specific VM from the latest collection.
// (GET /virtualmachines/{vmId})
func (h *Handler) GetLatestVirtualMachine(c *gin.Context, vmId string) {
	vmSvc, err := h.svc.LatestVirtualMachineService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.getVirtualMachine(c, vmSvc, vmId)
}

// UpdateLatestVirtualMachine updates VM properties in the latest collection.
// (PATCH /virtualmachines/{vmId})
func (h *Handler) UpdateLatestVirtualMachine(c *gin.Context, vmId string) {
	vmSvc, err := h.svc.LatestVirtualMachineService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.updateVirtualMachine(c, vmSvc, vmId)
}

// BatchUpdateLatestVMExclusion updates migration exclusion for multiple VMs in the latest collection.
// (POST /virtualmachines/batch-update-exclusion)
func (h *Handler) BatchUpdateLatestVMExclusion(c *gin.Context) {
	vmSvc, err := h.svc.LatestVirtualMachineService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.batchUpdateVMExclusion(c, vmSvc)
}

// GetVMFilterOptions returns distinct filter option values.
// (GET /collections/{id}/virtualmachines/filter-options)
func (h *Handler) GetVMFilterOptions(c *gin.Context, id string) {
	vmSvc, err := h.svc.VirtualMachineService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.getVMFilterOptions(c, vmSvc)
}

// GetLatestVMFilterOptions returns distinct filter option values from the latest collection.
// (GET /virtualmachines/filter-options)
func (h *Handler) GetLatestVMFilterOptions(c *gin.Context) {
	vmSvc, err := h.svc.LatestVirtualMachineService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.getVMFilterOptions(c, vmSvc)
}

// GetVMLabels returns all distinct labels with counts.
// (GET /collections/{id}/virtualmachines/labels)
func (h *Handler) GetVMLabels(c *gin.Context, id string) {
	vmSvc, err := h.svc.VirtualMachineService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.getVMLabels(c, vmSvc)
}

// GetLatestVMLabels returns all distinct labels with counts from the latest collection.
// (GET /virtualmachines/labels)
func (h *Handler) GetLatestVMLabels(c *gin.Context) {
	vmSvc, err := h.svc.LatestVirtualMachineService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.getVMLabels(c, vmSvc)
}

// UpdateLatestLabelVMs adds or removes a label from multiple VMs in the latest collection.
// (PATCH /virtualmachines/labels/{label})
func (h *Handler) UpdateLatestLabelVMs(c *gin.Context, label string) {
	vmSvc, err := h.svc.LatestVirtualMachineService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.updateLabelVMs(c, vmSvc, label)
}

// DeleteLatestLabelGlobally removes a label from all VMs in the latest collection.
// (DELETE /virtualmachines/labels/{label})
func (h *Handler) DeleteLatestLabelGlobally(c *gin.Context, label string) {
	vmSvc, err := h.svc.LatestVirtualMachineService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.deleteLabelGlobally(c, vmSvc, label)
}

// ── Private shared logic ───────────────────────────────────────────────

func (h *Handler) listVirtualMachines(c *gin.Context, vmSvc *services.VMService, byExpression *string, sort *[]string, page *int, pageSize *int) {
	pg := 1
	if page != nil && *page > 0 {
		pg = *page
	}
	ps := defaultPageSize
	if pageSize != nil && *pageSize > 0 {
		ps = min(*pageSize, maxPageSize)
	}

	svcParams := services.VMListParams{
		Limit:  uint64(ps),
		Offset: uint64((pg - 1) * ps),
	}

	if byExpression != nil {
		if _, err := filter.ParseWithDefaultMap([]byte(*byExpression)); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("expression filter is invalid: %v", err)})
			return
		}
		svcParams.Expression = *byExpression
	}

	if sort != nil {
		for _, s := range *sort {
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

	vms, total, err := vmSvc.List(c.Request.Context(), svcParams)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list VMs: %v", err)})
		return
	}

	pageCount := (total + ps - 1) / ps
	if pageCount == 0 {
		pageCount = 1
	}

	apiVMs := make([]v2.VirtualMachine, 0, len(vms))
	for _, vm := range vms {
		apiVMs = append(apiVMs, v2.NewVirtualMachineFromSummary(vm))
	}

	c.JSON(http.StatusOK, v2.VirtualMachineListResponse{
		Page:            pg,
		PageCount:       pageCount,
		Total:           total,
		VirtualMachines: apiVMs,
	})
}

func (h *Handler) getVirtualMachine(c *gin.Context, vmSvc *services.VMService, vmId string) {
	vm, err := vmSvc.Get(c.Request.Context(), vmId)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.NewVirtualMachineDetailFromModel(*vm))
}

func (h *Handler) updateVirtualMachine(c *gin.Context, vmSvc *services.VMService, vmId string) {
	var req v2.VirtualMachineUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	ctx := c.Request.Context()

	if req.MigrationExcluded != nil {
		if err := vmSvc.UpdateMigrationExcluded(ctx, vmId, *req.MigrationExcluded); err != nil {
			if srvErrors.IsResourceNotFoundError(err) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	if req.Labels != nil {
		if err := vmSvc.UpdateLabels(ctx, vmId, *req.Labels); err != nil {
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

func (h *Handler) batchUpdateVMExclusion(c *gin.Context, vmSvc *services.VMService) {
	var req v2.BatchUpdateExclusionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	if len(req.VmIds) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "vmIds array cannot be empty"})
		return
	}

	if err := vmSvc.UpdateMigrationExcludedBatch(c.Request.Context(), req.VmIds, req.MigrationExcluded); err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

func (h *Handler) getVMFilterOptions(c *gin.Context, vmSvc *services.VMService) {
	opts, err := vmSvc.GetFilterOptions(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get filter options: %v", err)})
		return
	}

	c.JSON(http.StatusOK, v2.VMFilterOptionsResponse{
		Clusters:          opts.Clusters,
		Datacenters:       opts.Datacenters,
		ConcernLabels:     opts.ConcernLabels,
		ConcernCategories: opts.ConcernCategories,
		Applications:      opts.Applications,
	})
}

func (h *Handler) getVMLabels(c *gin.Context, vmSvc *services.VMService) {
	labels, counts, err := vmSvc.GetAllLabels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get labels: %v", err)})
		return
	}

	c.JSON(http.StatusOK, v2.VMLabelsResponse{
		Labels: labels,
		Counts: counts,
	})
}

func (h *Handler) updateLabelVMs(c *gin.Context, vmSvc *services.VMService, label string) {
	if strings.TrimSpace(label) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "label cannot be empty or whitespace-only"})
		return
	}
	if len(label) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "label exceeds maximum length of 100 characters"})
		return
	}

	var req v2.UpdateLabelVMsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	if (req.Add == nil || len(*req.Add) == 0) && (req.Remove == nil || len(*req.Remove) == 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one of 'add' or 'remove' must be provided with VM IDs"})
		return
	}

	var addVMIDs, removeVMIDs []string
	if req.Add != nil {
		addVMIDs = *req.Add
	}
	if req.Remove != nil {
		removeVMIDs = *req.Remove
	}

	if err := vmSvc.UpdateLabelVMs(c.Request.Context(), addVMIDs, removeVMIDs, label); err != nil {
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

	c.Status(http.StatusOK)
}

func (h *Handler) deleteLabelGlobally(c *gin.Context, vmSvc *services.VMService, label string) {
	if strings.TrimSpace(label) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "label cannot be empty or whitespace-only"})
		return
	}
	if len(label) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "label exceeds maximum length of 100 characters"})
		return
	}

	affected, err := vmSvc.RemoveLabelFromAllVMs(c.Request.Context(), label)
	if err != nil {
		if srvErrors.IsValidationError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.DeleteLabelGloballyResponse{
		Affected: affected,
		Label:    label,
	})
}
