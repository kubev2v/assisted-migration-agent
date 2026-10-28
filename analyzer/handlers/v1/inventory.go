package analyzer

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"uuid"

	"github.com/gin-gonic/gin"
	"github.com/kubev2v/migration-planner/pkg/inventory/converters"
	"go.uber.org/zap"

	"github.com/kubev2v/migration-planner/api/v1alpha1"
	agentAPI "github.com/kubev2v/migration-planner/api/v1alpha1/agent"

	v1api "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	services "github.com/kubev2v/assisted-migration-agent/internal/services"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// GetInventory returns the collected inventory for a collection.
// (GET /collections/{id}/inventory)
func (h *Handler) GetInventory(c *gin.Context, id string) {
	invSvc, err := h.svc.InventoryService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.getInventory(c, invSvc)
}

// GetLatestInventory returns the inventory from the latest collection.
// (GET /inventory)
//
// With "?scope=all" it returns a ZIP bundle with the main inventory plus every
// group subset inventory, for manual upload in disconnected environments.
// Any other value (or no query param) keeps the default JSON response.
func (h *Handler) GetLatestInventory(c *gin.Context, params v1api.GetLatestInventoryParams) {
	invSvc, err := h.svc.LatestInventoryService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if params.Scope != nil && *params.Scope == v1api.All {
		h.getInventoryBundle(c, invSvc)
		return
	}

	h.getInventory(c, invSvc)
}

func (h *Handler) GetLatestVcenterInventory(c *gin.Context, vid string) {
	invSvc, err := h.svc.LatestInventoryService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if _, err := uuid.Parse(vid); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "vcenter id should be a valid UUID"})
		return
	}

	inv, err := invSvc.GetVCenterInventory(c.Request.Context(), vid)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no inventory found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var inventory v1alpha1.Inventory
	if err := json.Unmarshal(inv.Data, &inventory); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v1api.Inventory{Inventory: inventory})
}

// ── Private shared logic ───────────────────────────────────────────────

func (h *Handler) getInventory(c *gin.Context, invSvc *services.InventoryService) {
	inventory, err := invSvc.GetAggregatedInventory(c.Request.Context())
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no inventory found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	resp, err := v1api.NewVCenterInventory(inventory)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// getInventoryBundle writes a ZIP archive containing the main inventory
// (inventory.json) and every group subset inventory (subsets/<groupId>.json).
// The subset files reuse the agent-facing SourceSubsetUpdate shape so the SaaS
// upload endpoint consumes exactly what the connected flow pushes.
func (h *Handler) getInventoryBundle(c *gin.Context, invSvc *services.InventoryService) {
	ctx := c.Request.Context()
	log := zap.S().Named("inventory_handler")

	inv, err := invSvc.GetInventory(ctx)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		log.Errorw("failed to get inventory", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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

	// Limit 0 => all groups in the latest collection.
	groups, _, err := groupSvc.List(ctx, services.GroupListParams{})
	if err != nil {
		log.Errorw("failed to list groups for inventory bundle", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// Main inventory: inv.Data is already the marshaled v1alpha1.Inventory JSON.
	if err := writeZipEntry(zw, "inventory.json", inv.Data); err != nil {
		log.Errorw("failed to write main inventory to bundle", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build inventory bundle"})
		return
	}

	for _, g := range groups {
		if g.Inventory == nil {
			continue
		}
		apiInv := converters.ToAPI(g.Inventory)
		if apiInv == nil {
			continue
		}
		if apiInv.Vcenter == nil {
			log.Errorw("failed to build inventory bundle", "group", g.ID, "error", "vcenter inventory is missing")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build inventory bundle"})
			return
		}

		var vcenterID *string
		if apiInv.VcenterId != "" {
			vcenterID = &apiInv.VcenterId
		}
		vmsCount := apiInv.Vcenter.Vms.Total
		subset := agentAPI.SourceSubsetUpdate{
			VcenterId: vcenterID,
			Name:      g.Name,
			VmsCount:  &vmsCount,
			Inventory: *apiInv,
		}
		data, err := json.Marshal(subset)
		if err != nil {
			log.Errorw("failed to marshal subset inventory", "group", g.ID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build inventory bundle"})
			return
		}

		if err := writeZipEntry(zw, fmt.Sprintf("subsets/%s.json", g.ID.String()), data); err != nil {
			log.Errorw("failed to write subset inventory to bundle", "group", g.ID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build inventory bundle"})
			return
		}
	}

	if err := zw.Close(); err != nil {
		log.Errorw("failed to finalize inventory bundle", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build inventory bundle"})
		return
	}

	c.Header("Content-Disposition", `attachment; filename="inventory.zip"`)
	c.Data(http.StatusOK, "application/zip", buf.Bytes())
}

func writeZipEntry(zw *zip.Writer, name string, data []byte) error {
	entry, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = entry.Write(data)
	return err
}
