package v2

import (
	"context"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

type InventoryService struct {
	store *store.Store2
}

func NewInventoryService(st *store.Store2) *InventoryService {
	srv := &InventoryService{
		store: st,
	}

	return srv
}

// GetInventory retrieves the stored inventory.
func (c *InventoryService) GetInventory(ctx context.Context) (*models.Inventory, error) {
	return c.store.Inventory().Get(ctx)
}
