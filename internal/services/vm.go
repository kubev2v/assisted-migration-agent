package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kubev2v/migration-planner/pkg/inventory"
	"github.com/kubev2v/migration-planner/pkg/inventory/converters"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

type VMService struct {
	store *store.Store
}

func NewVMService(st *store.Store) *VMService {
	return &VMService{store: st}
}

type SortField struct {
	Field string
	Desc  bool
}

type VMListParams struct {
	Expression string
	Sort       []SortField
	Limit      uint64
	Offset     uint64
}

func (s *VMService) Get(ctx context.Context, id string) (*models.VM, error) {
	vm, err := s.store.VM().Get(ctx, id)
	if err != nil {
		return nil, err
	}

	results, err := s.store.Inspection().ListResults(ctx, id)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return vm, nil
	}

	vm.InspectionConcerns = results[0].Concerns

	return vm, nil
}

func (s *VMService) List(ctx context.Context, params VMListParams) ([]models.VirtualMachineSummary, int, error) {
	filter := store.ByFilter(params.Expression)

	opts := params.listOptions()

	vms, err := s.store.VM().List(ctx, filter, opts...)
	if err != nil {
		return nil, 0, err
	}

	total, err := s.store.VM().Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	return vms, total, nil
}

func (p VMListParams) listOptions() []store.ListOption {
	var opts []store.ListOption

	if len(p.Sort) > 0 {
		sortParams := make([]store.SortParam, len(p.Sort))
		for i, s := range p.Sort {
			sortParams[i] = store.SortParam{Field: s.Field, Desc: s.Desc}
		}
		opts = append(opts, store.WithSort(sortParams))
	} else {
		opts = append(opts, store.WithDefaultSort())
	}

	if p.Limit > 0 {
		opts = append(opts, store.WithLimit(p.Limit))
	}
	if p.Offset > 0 {
		opts = append(opts, store.WithOffset(p.Offset))
	}

	return opts
}

func (s *VMService) GetFilterOptions(ctx context.Context) (models.VMFilterOptions, error) {
	return s.store.VM().GetFilterOptions(ctx)
}

// UpdateMigrationExcluded updates the migration exclusion status for a VM.
// This operation updates the VM first, then rebuilds the main inventory and all
// affected group inventories to reflect the new exclusion state.
// The updates happen in two separate transactions due to DuckDB's single-connection
// constraint (ECOPROJECT-4704). If inventory updates fail, the VM update still succeeds.
//
// TODO: When ECOPROJECT-4704 is resolved, refactor to use a single atomic transaction:
//  1. Build inventories with modified VM state (in-memory, not from DB)
//  2. Update VM + main inventory + all group inventories in one transaction
func (s *VMService) UpdateMigrationExcluded(ctx context.Context, id string, excluded bool) error {
	// Verify VM exists first
	_, err := s.store.VM().Get(ctx, id)
	if err != nil {
		return err
	}

	// Find all groups that contain this VM (before updating)
	groupIDs, err := s.store.Group().GetGroupsContainingVM(ctx, id)
	if err != nil {
		return fmt.Errorf("finding groups containing VM: %w", err)
	}

	// Transaction 1: Update the VM's migration_excluded field
	err = s.store.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.store.VM().UpdateMigrationExcluded(txCtx, id, excluded); err != nil {
			return fmt.Errorf("updating VM migration_excluded: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Now build inventories with the updated VM state
	// BuildInventory will read from the database where the VM is now marked as excluded

	// Build main inventory (for all VMs)
	mainInventory, err := s.store.Parser().BuildInventory(ctx, nil)
	if err != nil {
		return fmt.Errorf("building main inventory: %w", err)
	}

	// Marshal main inventory to JSON
	mainInventoryData, err := json.Marshal(converters.ToAPI(mainInventory))
	if err != nil {
		return fmt.Errorf("marshaling main inventory: %w", err)
	}

	// Build group inventories
	type groupInventory struct {
		groupID   int
		inventory *inventory.Inventory
	}
	newInventories := make([]groupInventory, 0, len(groupIDs))

	for _, groupID := range groupIDs {
		// Get current VM matches for this group
		vmIDs, err := s.store.Group().GetMatchedIDs(ctx, groupID)
		if err != nil {
			return fmt.Errorf("getting matched VM IDs for group %d: %w", groupID, err)
		}

		// Build scoped inventory for this group's VMs
		// This reads the current DB state where the VM is now excluded
		var inv *inventory.Inventory
		if len(vmIDs) > 0 {
			inv, err = s.store.Parser().BuildInventory(ctx, vmIDs)
			if err != nil {
				return fmt.Errorf("building inventory for group %d: %w", groupID, err)
			}
		}

		newInventories = append(newInventories, groupInventory{
			groupID:   groupID,
			inventory: inv,
		})
	}

	// Transaction 2: Update main inventory and all affected group inventories
	err = s.store.WithTx(ctx, func(txCtx context.Context) error {
		// Update main inventory
		if err := s.store.Inventory().Save(txCtx, mainInventoryData); err != nil {
			return fmt.Errorf("updating main inventory: %w", err)
		}

		// Update group inventories
		for _, gi := range newInventories {
			if err := s.store.Group().UpdateInventory(txCtx, gi.groupID, gi.inventory); err != nil {
				return fmt.Errorf("updating inventory for group %d: %w", gi.groupID, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

// UpdateLabels updates the labels for a VM.
func (s *VMService) UpdateLabels(ctx context.Context, id string, labels []string) error {
	return s.store.VM().UpdateLabels(ctx, id, labels)
}

// GetAllLabels returns all distinct labels in use across VMs along with their counts.
// The labels and counts are returned in the same order (sorted alphabetically by label).
func (s *VMService) GetAllLabels(ctx context.Context) ([]string, []int, error) {
	return s.store.VM().GetAllLabels(ctx)
}

// RemoveLabelFromAllVMs removes a label from all VMs in the system.
func (s *VMService) RemoveLabelFromAllVMs(ctx context.Context, label string) (int, error) {
	return s.store.VM().RemoveLabelGlobally(ctx, label)
}

// UpdateLabelVMs adds and/or removes a label from multiple VMs atomically.
// All operations succeed or fail together - if any VM is not found or any
// operation fails, the entire transaction is rolled back and no changes are made.
func (s *VMService) UpdateLabelVMs(ctx context.Context, addVMIDs, removeVMIDs []string, label string) error {
	return s.store.WithTx(ctx, func(txCtx context.Context) error {
		// Perform batch add operation (validates VMs exist internally)
		if len(addVMIDs) > 0 {
			if err := s.store.VM().AddLabelBatch(txCtx, addVMIDs, label); err != nil {
				return err
			}
		}

		// Perform batch remove operation (validates VMs exist internally)
		if len(removeVMIDs) > 0 {
			if err := s.store.VM().RemoveLabelBatch(txCtx, removeVMIDs, label); err != nil {
				return err
			}
		}

		return nil
	})
}
