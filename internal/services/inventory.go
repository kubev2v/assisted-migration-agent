package services

import (
	"context"
	"encoding/json"
	"fmt"
	"uuid"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/filter"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/converters"
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

// DeleteVCenterInventory removes the inventory of a vCenter together with the data derived
// from it: VMs, rightsizing reports and group inventories.
func (c *InventoryService) DeleteVCenterInventory(ctx context.Context, vCenterID uuid.UUID) error {
	if _, err := c.store.Inventory().GetByID(ctx, vCenterID.String()); err != nil {
		return err
	}

	expression := fmt.Sprintf("vcenter_id = %q", vCenterID.String())

	// The VM delete needs to run outside the transaction: DuckDB fails when the
	// FK child and parent rows are deleted in the same transaction.
	// https://github.com/duckdb/duckdb/issues/13819
	if err := NewVMService(c.store).Delete(ctx, expression); err != nil {
		return fmt.Errorf("deleting VMs for vCenter %s: %w", vCenterID, err)
	}

	if err := NewRightsizingService(c.store).DeleteByVCenter(ctx, vCenterID.String()); err != nil {
		return fmt.Errorf("deleting rightsizing data for vCenter %s: %w", vCenterID, err)
	}

	parser := duckdb_parser.New(c.store.Querier(), nil)

	return c.store.WithTx(ctx, func(txCtx context.Context) error {
		inventoryFilter, err := filter.ParseWithInventoryMap([]byte(expression))
		if err != nil {
			return fmt.Errorf("parsing filter expression: %w", err)
		}

		if err := c.store.Inventory().Delete(txCtx, inventoryFilter); err != nil {
			return fmt.Errorf("deleting inventories: %w", err)
		}

		inv, err := parser.BuildInventory(txCtx, nil)
		if err != nil {
			return fmt.Errorf("rebuilding main inventory: %w", err)
		}

		inv.VCenterID = "aggregated"
		inv.VCenterVersion = "aggregated"

		invBytes, err := json.Marshal(converters.ToAPI(inv))
		if err != nil {
			return fmt.Errorf("marshaling main inventory: %w", err)
		}

		if err := c.store.Inventory().Save(txCtx, invBytes); err != nil {
			return fmt.Errorf("saving main inventory: %w", err)
		}

		if err := NewGroupService(c.store, parser).RefreshAll(txCtx); err != nil {
			return fmt.Errorf("refreshing group inventories: %w", err)
		}

		return nil
	})
}
