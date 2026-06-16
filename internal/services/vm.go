package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/kubev2v/migration-planner/pkg/inventory"
	"github.com/kubev2v/migration-planner/pkg/inventory/converters"
	"go.uber.org/zap"

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
// This operation updates the VM and rebuilds the main inventory and all
// affected group inventories to reflect the new exclusion state.
//
// To maintain atomicity between VM mutation and outbox events, the VM update
// is performed in the same transaction as inventory saves and outbox inserts.
// Inventory building happens outside the transaction (reading committed state),
// then all writes occur atomically in one transaction.
func (s *VMService) UpdateMigrationExcluded(ctx context.Context, id string, excluded bool) error {
	// Get VM and capture original migration_excluded value for rollback
	vm, err := s.store.VM().Get(ctx, id)
	if err != nil {
		return err
	}
	originalExcluded := vm.MigrationExcluded

	// Find all groups that contain this VM (before updating)
	groupIDs, err := s.store.Group().GetGroupsContainingVM(ctx, id)
	if err != nil {
		return fmt.Errorf("finding groups containing VM: %w", err)
	}

	// Transaction 1: Update the VM's migration_excluded field
	// If inventory building or Transaction 2 fails, this will be rolled back in the deferred cleanup
	err = s.store.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.store.VM().UpdateMigrationExcluded(txCtx, id, excluded); err != nil {
			return fmt.Errorf("updating VM migration_excluded: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Track whether we need to rollback the VM update if subsequent operations fail
	vmUpdateSucceeded := true
	defer func() {
		// If inventory building or Transaction 2 failed, rollback to the ORIGINAL value
		if !vmUpdateSucceeded {
			// Rollback: restore the original excluded state (not !excluded)
			// Use background context to ensure rollback isn't blocked by cancelled/timed-out request context
			rollbackCtx := context.Background()
			if err := s.store.WithTx(rollbackCtx, func(txCtx context.Context) error {
				return s.store.VM().UpdateMigrationExcluded(txCtx, id, originalExcluded)
			}); err != nil {
				// Log rollback failure - VM is now inconsistent
				zap.S().Named("vm_service").Errorw(
					"failed to rollback VM migration_excluded after operation failure - VM state may be inconsistent",
					"vmID", id,
					"attemptedValue", excluded,
					"originalValue", originalExcluded,
					"error", err,
				)
			}
		}
	}()

	// Now build inventories with the updated VM state
	// BuildInventory will read from the database where the VM is now marked as excluded

	// Build main inventory (for all VMs)
	mainInventory, err := s.store.Parser().BuildInventory(ctx, nil)
	if err != nil {
		vmUpdateSucceeded = false
		return fmt.Errorf("building main inventory: %w", err)
	}

	// Marshal main inventory to JSON
	mainInventoryData, err := json.Marshal(converters.ToAPI(mainInventory))
	if err != nil {
		vmUpdateSucceeded = false
		return fmt.Errorf("marshaling main inventory: %w", err)
	}

	// Build group inventories
	type groupInventory struct {
		groupID   uuid.UUID
		inventory *inventory.Inventory
	}
	newInventories := make([]groupInventory, 0, len(groupIDs))

	for _, groupID := range groupIDs {
		// Get current VM matches for this group
		vmIDs, err := s.store.Group().GetMatchedIDs(ctx, groupID)
		if err != nil {
			vmUpdateSucceeded = false
			return fmt.Errorf("getting matched VM IDs for group %s: %w", groupID, err)
		}

		// Build scoped inventory for this group's VMs
		// This reads the current DB state where the VM is now excluded
		var inv *inventory.Inventory
		if len(vmIDs) > 0 {
			inv, err = s.store.Parser().BuildInventory(ctx, vmIDs)
			if err != nil {
				vmUpdateSucceeded = false
				return fmt.Errorf("building inventory for group %s: %w", groupID, err)
			}
		}

		newInventories = append(newInventories, groupInventory{
			groupID:   groupID,
			inventory: inv,
		})
	}

	// Transaction 2: Update main inventory and all affected group inventories + add outbox events
	// If this fails, the deferred rollback will restore the original VM state
	err = s.store.WithTx(ctx, func(txCtx context.Context) error {
		// Update main inventory
		if err := s.store.Inventory().Save(txCtx, mainInventoryData); err != nil {
			return fmt.Errorf("updating main inventory: %w", err)
		}

		// Add outbox event for main inventory update
		mainEvent := models.Event{
			Kind: models.InventoryUpdateEvent,
			Data: mainInventoryData,
		}
		if err := s.store.Outbox().Insert(txCtx, mainEvent); err != nil {
			return fmt.Errorf("adding main inventory event: %w", err)
		}

		// Update group inventories and add outbox events
		for _, gi := range newInventories {
			if err := s.store.Group().UpdateInventory(txCtx, gi.groupID, gi.inventory); err != nil {
				return fmt.Errorf("updating inventory for group %s: %w", gi.groupID, err)
			}

			// Add outbox event for group inventory update
			// Need to fetch group name from database for event payload
			group, err := s.store.Group().Get(txCtx, gi.groupID)
			if err != nil {
				return fmt.Errorf("getting group %s: %w", gi.groupID, err)
			}

			// Prepare inventory as JSON (always emit event, even for empty groups)
			var invJSON json.RawMessage
			if gi.inventory != nil {
				// Convert domain inventory to API type before marshaling
				apiInventory := converters.ToAPI(gi.inventory)
				invBytes, err := json.Marshal(apiInventory)
				if err != nil {
					return fmt.Errorf("marshaling inventory for group %s: %w", gi.groupID, err)
				}
				invJSON = invBytes
			} else {
				// Empty inventory - use JSON null to indicate the group has no VMs
				invJSON = json.RawMessage("null")
			}

			// Create typed event payload
			payload := models.GroupInventoryEventPayload{
				GroupID:   gi.groupID.String(),
				GroupName: group.Name,
				Inventory: invJSON,
			}

			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("marshaling event payload for group %s: %w", gi.groupID, err)
			}

			groupEvent := models.Event{
				Kind: models.GroupInventoryUpsertEvent,
				Data: payloadBytes,
			}
			if err := s.store.Outbox().Insert(txCtx, groupEvent); err != nil {
				return fmt.Errorf("adding group inventory event for group %s: %w", gi.groupID, err)
			}
		}
		return nil
	})
	if err != nil {
		vmUpdateSucceeded = false
		return err
	}

	// Success - all changes are now atomic (VM + inventories + outbox events)
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
