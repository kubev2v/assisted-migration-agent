package services

import (
	"context"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

type InventoryService struct {
	store *store.Store
}

func NewInventoryService(st *store.Store) *InventoryService {
	srv := &InventoryService{
		store: st,
	}

	return srv
}

// GetInventory retrieves the stored inventory.
func (c *InventoryService) GetInventory(ctx context.Context) (*models.Inventory, error) {
	return c.store.Inventory().Get(ctx)
}

// GetVCenterInventory returns the aggregated inventory along with the ids of all vcenters composing it.
func (c *InventoryService) GetAggregatedInventory(ctx context.Context) (models.VCenterInventory, error) {
	inv, err := c.store.Inventory().Get(ctx)
	if err != nil {
		return models.VCenterInventory{}, err
	}

	ids, err := c.store.Inspection().GetVCenterIDs(ctx)
	if err != nil {
		return models.VCenterInventory{}, err
	}

	m := models.VCenterInventory{
		Data:       inv.Data,
		CreatedAt:  inv.CreatedAt,
		UpdatedAt:  inv.UpdatedAt,
		VcenterIDs: ids,
	}

	return m, nil
}

func (c *InventoryService) GetVCenterInventory(ctx context.Context, vcenterID string) (models.Inventory, error) {
	return c.store.Inventory().GetByID(ctx, vcenterID)
}
