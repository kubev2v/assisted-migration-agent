package v2

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kubev2v/migration-planner/pkg/inventory/converters"
	"go.uber.org/zap"

	"github.com/kubev2v/migration-planner/api/v1alpha1"

	v2api "github.com/kubev2v/assisted-migration-agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/inventoryutil"
	services "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
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
func (h *Handler) GetLatestInventory(c *gin.Context, params v2api.GetLatestInventoryParams) {
	invSvc, err := h.svc.LatestInventoryService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if params.Scope != nil && *params.Scope == v2api.All {
		h.getInventoryBundle(c, invSvc)
		return
	}

	h.getInventory(c, invSvc)
}

// ── Private shared logic ───────────────────────────────────────────────

func (h *Handler) getInventory(c *gin.Context, invSvc *services.InventoryService) {
	inv, err := invSvc.GetInventory(c.Request.Context())
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		zap.S().Named("inventory_handler").Errorw("failed to get inventory", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var inventory v1alpha1.Inventory
	if err := json.Unmarshal(inv.Data, &inventory); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error unmarshalling inventory: %v", err)})
		return
	}

	agentID, err := uuid.Parse(h.cfg.Agent.ID)
	if err != nil {
		zap.S().Named("inventory_handler").Errorw("invalid agent ID in configuration", "agent_id", h.cfg.Agent.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid agent ID configuration"})
		return
	}

	c.JSON(http.StatusOK, v2api.Inventory{
		Inventory: v1alpha1.UpdateInventory{
			AgentId:   agentID,
			Inventory: inventory,
		},
	})
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

		subset := inventoryutil.NewSourceSubsetUpdate(g.Name, *apiInv)
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
