package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/kubev2v/assisted-migration-agent/internal/models"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
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
	defaultPageSize = 20
	maxPageSize     = 100
)

// GetVMs returns the list of VMs with filtering and pagination
// (GET /vms)
func (h *Handler) GetVMs(c *gin.Context, params v1.GetVMsParams) {
	// Validate disk size range
	if params.DiskSizeMin != nil && params.DiskSizeMax != nil && *params.DiskSizeMin > *params.DiskSizeMax {
		c.JSON(http.StatusBadRequest, gin.H{"error": "diskSizeMin cannot be greater than diskSizeMax"})
		return
	}

	// Validate memory size range
	if params.MemorySizeMin != nil && params.MemorySizeMax != nil && *params.MemorySizeMin > *params.MemorySizeMax {
		c.JSON(http.StatusBadRequest, gin.H{"error": "memorySizeMin cannot be greater than memorySizeMax"})
		return
	}

	// Parse pagination
	page := 1
	if params.Page != nil && *params.Page > 0 {
		page = *params.Page
	}
	pageSize := defaultPageSize
	if params.PageSize != nil && *params.PageSize > 0 {
		pageSize = *params.PageSize
		if pageSize > maxPageSize {
			pageSize = maxPageSize
		}
	}

	// Build service params
	svcParams := services.VMListParams{
		Limit:  uint64(pageSize),
		Offset: uint64((page - 1) * pageSize),
	}

	if params.Clusters != nil {
		svcParams.Clusters = *params.Clusters
	}
	if params.Status != nil {
		svcParams.Statuses = *params.Status
	}
	if params.MinIssues != nil {
		svcParams.MinIssues = *params.MinIssues
	}
	if params.DiskSizeMin != nil {
		svcParams.DiskSizeMin = params.DiskSizeMin
	}
	if params.DiskSizeMax != nil {
		svcParams.DiskSizeMax = params.DiskSizeMax
	}
	if params.MemorySizeMin != nil {
		svcParams.MemorySizeMin = params.MemorySizeMin
	}
	if params.MemorySizeMax != nil {
		svcParams.MemorySizeMax = params.MemorySizeMax
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
		zap.S().Named("vm_handler").Errorw("failed to list VMs", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list VMs"})
		return
	}

	// Calculate page count
	pageCount := (total + pageSize - 1) / pageSize
	if pageCount == 0 {
		pageCount = 1
	}

	// Map to API response
	apiVMs := make([]v1.VM, 0, len(vms))
	for _, vm := range vms {
		s, err := h.inspectorSrv.GetVmStatus(c.Request.Context(), vm.ID)
		switch {
		case err == nil:
			vm.Status = s
		case errors.Is(err, sql.ErrNoRows):
			vm.Status = models.InspectionStatus{State: models.InspectionStateNotFound}
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get VM status: %v", err)})
			return
		}
		apiVMs = append(apiVMs, v1.NewVMFromSummary(vm))
	}

	c.JSON(http.StatusOK, v1.VMListResponse{
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
		zap.S().Named("vm_handler").Errorw("failed to get VM", "id", id, "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "VM not found"})
		return
	}

	c.JSON(http.StatusOK, v1.NewVMDetailsFromModel(*vm))
}

// GetVMInspectionStatus returns the inspection status for a specific VM
// (GET /vms/{id}/inspector)
func (h *Handler) GetVMInspectionStatus(c *gin.Context, id string) {
	s, err := h.inspectorSrv.GetVmStatus(c.Request.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			c.JSON(http.StatusOK, v1.InspectionStatus{State: v1.InspectionStatusStateNotFound})
			return
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get VM status: %v", err)})
			return
		}
	}

	c.JSON(http.StatusOK, v1.NewInspectionStatus(s))
}

// RemoveVMFromInspection removes VM from inspection queue
// (DELETE /vms/{id}/inspector)
func (h *Handler) RemoveVMFromInspection(c *gin.Context, id string) {
	if !h.inspectorSrv.IsInspectorRunning() {
		c.JSON(http.StatusNotFound, gin.H{"error": "inspector not running"})
		return
	}

	if err := h.inspectorSrv.CancelVmsInspection(c.Request.Context(), []string{id}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	s, err := h.inspectorSrv.GetVmStatus(c.Request.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			c.JSON(http.StatusOK, v1.InspectionStatus{State: v1.InspectionStatusStateNotFound})
			return
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get VM status: %v", err)})
			return
		}
	}

	c.JSON(http.StatusOK, v1.NewInspectionStatus(s))
}

// GetInspectorStatus returns the inspector status
// (GET /vms/inspector)
func (h *Handler) GetInspectorStatus(c *gin.Context) {
	c.JSON(http.StatusOK, v1.NewInspectorStatus(h.inspectorSrv.GetStatus()))
}

// StartInspection starts inspection for VMs
// (POST /vms/inspector)
func (h *Handler) StartInspection(c *gin.Context) {
	var req v1.InspectorStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Todo: validate using the openapi spec. do the same for the collector
	if req.VcenterCredentials.Url == "" || req.VcenterCredentials.Username == "" || req.VcenterCredentials.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url, username, and password are required"})
		return
	}

	if len(req.VmIds) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no vms provided"})
		return
	}

	cred := &models.Credentials{
		URL:      req.VcenterCredentials.Url,
		Username: req.VcenterCredentials.Username,
		Password: req.VcenterCredentials.Password,
	}

	if err := h.inspectorSrv.Start(c.Request.Context(), req.VmIds, cred); err != nil {
		switch err.(type) {
		case *srvErrors.InspectorInProgressError:
			c.JSON(http.StatusLocked, gin.H{"error": err.Error()})
		case *srvErrors.InspectorNonExistVmError:
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			zap.S().Named("vm_handler").Errorw("failed to start inspector", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to start inspector: %v", err)})
		}
		return
	}

	c.JSON(http.StatusAccepted, v1.InspectorStatus{State: v1.InspectorStatusStateStarted})
}

// AddVMsToInspection adds more VMs to inspection queue
// (PATCH /vms/inspector)
func (h *Handler) AddVMsToInspection(c *gin.Context) {
	var vmsMoid v1.VMIdArray
	if err := c.ShouldBindJSON(&vmsMoid); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if !h.inspectorSrv.IsInspectorRunning() {
		c.JSON(http.StatusNotFound, gin.H{"error": "inspector not running"})
		return
	}

	if err := h.inspectorSrv.AddMoreVms(c.Request.Context(), vmsMoid); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, v1.NewInspectorStatus(h.inspectorSrv.GetStatus()))

}

// StopInspection stops inspector entirely
// (DELETE /vms/inspector)
func (h *Handler) StopInspection(c *gin.Context) {
	if !h.inspectorSrv.IsInspectorRunning() {
		c.JSON(http.StatusNotFound, gin.H{"error": "inspector not running"})
		return
	}

	c.JSON(http.StatusNotImplemented, gin.H{"error": "not yet implemented"})
}
