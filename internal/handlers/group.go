package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/filter"
)

// ListGroups returns groups with optional name filtering and pagination
// (GET /vms/groups)
func (h *Handler) ListGroups(c *gin.Context, params v1.ListGroupsParams) {
	page := 1
	if params.Page != nil && *params.Page > 0 {
		page = *params.Page
	}

	pageSize := defaultPageSize
	if params.PageSize != nil && *params.PageSize > 0 {
		pageSize = min(*params.PageSize, maxPageSize)
	}

	svcParams := services.GroupListParams{
		Limit:  uint64(pageSize),
		Offset: uint64((page - 1) * pageSize),
	}

	if params.ByName != nil {
		escaped := strings.ReplaceAll(*params.ByName, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `'`, `\'`)
		svcParams.ByName = escaped
	}

	groups, total, err := h.groupSrv.List(c.Request.Context(), svcParams)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	pageCount := (total + pageSize - 1) / pageSize
	if pageCount == 0 {
		pageCount = 1
	}

	apiGroups := make([]v1.Group, 0, len(groups))
	for _, g := range groups {
		apiGroups = append(apiGroups, v1.NewGroupFromModel(g))
	}

	c.JSON(http.StatusOK, v1.GroupListResponse{
		Groups:    apiGroups,
		Total:     total,
		Page:      page,
		PageCount: pageCount,
	})
}

// CreateGroup creates a new group
// (POST /vms/groups)
func (h *Handler) CreateGroup(c *gin.Context) {
	var req v1.CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be blank"})
		return
	}

	if _, err := filter.ParseWithDefaultMap([]byte(req.Filter)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("filter is invalid: %v", err)})
		return
	}

	group := models.Group{
		Name:   req.Name,
		Filter: req.Filter,
	}
	if req.Description != nil {
		group.Description = *req.Description
	}

	created, err := h.groupSrv.Create(c.Request.Context(), group)
	if err != nil {
		if srvErrors.IsDuplicateResourceError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, v1.NewGroupFromModel(*created))
}

// GetGroup returns a group by ID with its VMs
// (GET /vms/groups/{id})
func (h *Handler) GetGroup(c *gin.Context, id string, params v1.GetGroupParams) {
	groupID, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group id"})
		return
	}

	group, err := h.groupSrv.Get(c.Request.Context(), groupID)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	page := 1
	if params.Page != nil && *params.Page > 0 {
		page = *params.Page
	}

	pageSize := defaultPageSize
	if params.PageSize != nil && *params.PageSize > 0 {
		pageSize = min(*params.PageSize, maxPageSize)
	}

	svcParams := services.GroupGetParams{
		Limit:  uint64(pageSize),
		Offset: uint64((page - 1) * pageSize),
	}

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

	vms, total, err := h.groupSrv.ListVirtualMachines(c.Request.Context(), groupID, svcParams)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	pageCount := (total + pageSize - 1) / pageSize
	if pageCount == 0 {
		pageCount = 1
	}

	apiVMs := make([]v1.VirtualMachine, 0, len(vms))
	for _, vm := range vms {
		apiVMs = append(apiVMs, v1.NewVirtualMachineFromSummary(vm))
	}

	c.JSON(http.StatusOK, v1.GroupResponse{
		Group:     v1.NewGroupFromModel(*group),
		Page:      page,
		PageCount: pageCount,
		Total:     total,
		Vms:       apiVMs,
	})
}

// UpdateGroup partially updates an existing group
// (PATCH /vms/groups/{id})
func (h *Handler) UpdateGroup(c *gin.Context, id string) {
	groupID, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group id"})
		return
	}

	var req v1.UpdateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		req.Name = &trimmed
		if *req.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be blank"})
			return
		}
	}

	if req.Filter != nil {
		if _, err := filter.ParseWithDefaultMap([]byte(*req.Filter)); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("filter is invalid: %v", err)})
			return
		}
	}

	existing, err := h.groupSrv.Get(c.Request.Context(), groupID)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Filter != nil {
		existing.Filter = *req.Filter
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}

	updated, err := h.groupSrv.Update(c.Request.Context(), groupID, *existing)
	if err != nil {
		if srvErrors.IsDuplicateResourceError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v1.NewGroupFromModel(*updated))
}

// DeleteGroup deletes a group
// (DELETE /vms/groups/{id})
func (h *Handler) DeleteGroup(c *gin.Context, id string) {
	groupID, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group id"})
		return
	}

	if err := h.groupSrv.Delete(c.Request.Context(), groupID); err != nil {
		if !srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.Status(http.StatusNoContent)
}
