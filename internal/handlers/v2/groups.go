package v2

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	services "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/filter"
)

// ListGroups returns groups with optional name filtering and pagination.
// (GET /collections/{id}/groups)
func (h *Handler) ListGroups(c *gin.Context, id string, params v2.ListGroupsParams) {
	groupSvc, err := h.svc.GroupService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.listGroups(c, groupSvc, params.ByName, params.Page, params.PageSize)
}

// ListLatestGroups returns groups from the latest collection.
// (GET /groups)
func (h *Handler) ListLatestGroups(c *gin.Context, params v2.ListLatestGroupsParams) {
	groupSvc, err := h.svc.LatestGroupService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.listGroups(c, groupSvc, params.ByName, params.Page, params.PageSize)
}

// CreateGroup creates a new group within a collection.
// (POST /collections/{id}/groups)
func (h *Handler) CreateGroup(c *gin.Context, id string) {
	groupSvc, err := h.svc.GroupService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.createGroup(c, groupSvc)
}

// CreateLatestGroup creates a new group in the latest collection.
// (POST /groups)
func (h *Handler) CreateLatestGroup(c *gin.Context) {
	groupSvc, err := h.svc.LatestGroupService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.createGroup(c, groupSvc)
}

// GetGroup returns a group by ID with its VMs.
// (GET /collections/{id}/groups/{groupId})
func (h *Handler) GetGroup(c *gin.Context, id string, groupId string, params v2.GetGroupParams) {
	groupSvc, err := h.svc.GroupService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.getGroup(c, groupSvc, groupId, params.Sort, params.Page, params.PageSize)
}

// GetLatestGroup returns a group by ID from the latest collection.
// (GET /groups/{groupId})
func (h *Handler) GetLatestGroup(c *gin.Context, groupId string, params v2.GetLatestGroupParams) {
	groupSvc, err := h.svc.LatestGroupService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.getGroup(c, groupSvc, groupId, params.Sort, params.Page, params.PageSize)
}

// UpdateGroup partially updates an existing group.
// (PATCH /collections/{id}/groups/{groupId})
func (h *Handler) UpdateGroup(c *gin.Context, id string, groupId string) {
	groupSvc, err := h.svc.GroupService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.updateGroup(c, groupSvc, groupId)
}

// UpdateLatestGroup partially updates a group in the latest collection.
// (PATCH /groups/{groupId})
func (h *Handler) UpdateLatestGroup(c *gin.Context, groupId string) {
	groupSvc, err := h.svc.LatestGroupService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.updateGroup(c, groupSvc, groupId)
}

// DeleteGroup deletes a group.
// (DELETE /collections/{id}/groups/{groupId})
func (h *Handler) DeleteGroup(c *gin.Context, id string, groupId string) {
	groupSvc, err := h.svc.GroupService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.deleteGroup(c, groupSvc, groupId)
}

// DeleteLatestGroup deletes a group from the latest collection.
// (DELETE /groups/{groupId})
func (h *Handler) DeleteLatestGroup(c *gin.Context, groupId string) {
	groupSvc, err := h.svc.LatestGroupService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.deleteGroup(c, groupSvc, groupId)
}

// ── Private shared logic ───────────────────────────────────────────────

func (h *Handler) listGroups(c *gin.Context, groupSvc *services.GroupService, byName *string, page *int, pageSize *int) {
	pg := 1
	if page != nil && *page > 0 {
		pg = *page
	}

	ps := defaultPageSize
	if pageSize != nil && *pageSize > 0 {
		ps = min(*pageSize, maxPageSize)
	}

	svcParams := services.GroupListParams{
		Limit:  uint64(ps),
		Offset: uint64((pg - 1) * ps),
	}

	if byName != nil {
		escaped := strings.ReplaceAll(*byName, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `'`, `\'`)
		svcParams.ByName = escaped
	}

	groups, total, err := groupSvc.List(c.Request.Context(), svcParams)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	pageCount := (total + ps - 1) / ps
	if pageCount == 0 {
		pageCount = 1
	}

	apiGroups := make([]v2.Group, 0, len(groups))
	for _, g := range groups {
		apiGroups = append(apiGroups, v2.NewGroupFromModel(g))
	}

	c.JSON(http.StatusOK, v2.GroupListResponse{
		Groups:    apiGroups,
		Total:     total,
		Page:      pg,
		PageCount: pageCount,
	})
}

func (h *Handler) createGroup(c *gin.Context, groupSvc *services.GroupService) {
	var req v2.CreateGroupRequest
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

	created, err := groupSvc.Create(c.Request.Context(), group)
	if err != nil {
		if srvErrors.IsDuplicateResourceError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, v2.NewGroupFromModel(*created))
}

func (h *Handler) getGroup(c *gin.Context, groupSvc *services.GroupService, groupId string, sort *[]string, page *int, pageSize *int) {
	gid, err := uuid.Parse(groupId)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group ID"})
		return
	}

	group, err := groupSvc.Get(c.Request.Context(), gid)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	pg := 1
	if page != nil && *page > 0 {
		pg = *page
	}

	ps := defaultPageSize
	if pageSize != nil && *pageSize > 0 {
		ps = min(*pageSize, maxPageSize)
	}

	svcParams := services.GroupGetParams{
		Limit:  uint64(ps),
		Offset: uint64((pg - 1) * ps),
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

	vms, total, err := groupSvc.ListVirtualMachines(c.Request.Context(), gid, svcParams)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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

	c.JSON(http.StatusOK, v2.GroupResponse{
		Group:     v2.NewGroupFromModel(*group),
		Page:      pg,
		PageCount: pageCount,
		Total:     total,
		Vms:       apiVMs,
	})
}

func (h *Handler) updateGroup(c *gin.Context, groupSvc *services.GroupService, groupId string) {
	gid, err := uuid.Parse(groupId)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group ID"})
		return
	}

	var req v2.UpdateGroupRequest
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

	existing, err := groupSvc.Get(c.Request.Context(), gid)
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

	updated, err := groupSvc.Update(c.Request.Context(), gid, *existing)
	if err != nil {
		if srvErrors.IsDuplicateResourceError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.NewGroupFromModel(*updated))
}

func (h *Handler) deleteGroup(c *gin.Context, groupSvc *services.GroupService, groupId string) {
	gid, err := uuid.Parse(groupId)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group ID"})
		return
	}

	if err := groupSvc.Delete(c.Request.Context(), gid); err != nil {
		if !srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.Status(http.StatusNoContent)
}
