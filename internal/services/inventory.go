package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kubev2v/migration-planner/pkg/inventory"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
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

// preserveCollectionTime carries the source date into inventories rebuilt after ingestion.
func preserveCollectionTime(ctx context.Context, st *store.Store, inv *inventory.Inventory) error {
	saved, err := st.Inventory().Get(ctx)
	if srvErrors.IsResourceNotFoundError(err) {
		return nil // The first build already has the date from its source.
	}
	if err != nil {
		return fmt.Errorf("getting saved inventory: %w", err)
	}
	var previous struct {
		CollectionTime *time.Time `json:"collection_time"`
	}
	if err := json.Unmarshal(saved.Data, &previous); err != nil {
		return fmt.Errorf("reading saved collection time: %w", err)
	}
	inv.CollectionTime = previous.CollectionTime
	return nil
}
