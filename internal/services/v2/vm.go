package v2

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	"github.com/kubev2v/migration-planner/pkg/inventory"
	"github.com/kubev2v/migration-planner/pkg/inventory/converters"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

type VMService struct {
	store *store.Store2
}

func NewVMService(st *store.Store2) *VMService {
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
// All mutations (VM update, inventory rebuild, outbox events) happen in a
// single transaction. BuildInventory sees the uncommitted VM change via the
// transaction-aware QueryInterceptor.
func (s *VMService) UpdateMigrationExcluded(ctx context.Context, id string, excluded bool) error {
	if _, err := s.store.VM().Get(ctx, id); err != nil {
		return err
	}

	return s.store.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.store.VM().UpdateMigrationExcluded(txCtx, id, excluded); err != nil {
			return fmt.Errorf("updating VM migration_excluded: %w", err)
		}

		if err := s.buildAndSaveMainInventory(txCtx); err != nil {
			return err
		}

		groupIDs, err := s.store.Group().GetGroupsContainingVM(txCtx, id)
		if err != nil {
			return fmt.Errorf("finding groups containing VM: %w", err)
		}

		for _, groupID := range groupIDs {
			if err := s.buildAndSaveGroupInventory(txCtx, groupID); err != nil {
				return err
			}
		}

		return nil
	})
}

// UpdateMigrationExcludedBatch updates the migration exclusion status for multiple VMs.
// All mutations happen in a single transaction — BuildInventory sees uncommitted
// changes via the transaction-aware QueryInterceptor.
func (s *VMService) UpdateMigrationExcludedBatch(ctx context.Context, vmIDs []string, excluded bool) error {
	uniqueIDs := deduplicateStrings(vmIDs)
	if len(uniqueIDs) == 0 {
		return nil
	}

	// Validate all VMs exist
	if _, err := s.store.VM().GetMigrationExcludedStates(ctx, uniqueIDs); err != nil {
		return err
	}

	return s.store.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.store.VM().UpdateMigrationExcludedBatch(txCtx, uniqueIDs, excluded); err != nil {
			return fmt.Errorf("updating VMs migration_excluded: %w", err)
		}

		if err := s.buildAndSaveMainInventory(txCtx); err != nil {
			return err
		}

		// Find all groups containing any of these VMs
		groupIDsMap := make(map[uuid.UUID]bool)
		for _, vmID := range uniqueIDs {
			vmGroups, err := s.store.Group().GetGroupsContainingVM(txCtx, vmID)
			if err != nil {
				return fmt.Errorf("finding groups containing VM %s: %w", vmID, err)
			}
			for _, gid := range vmGroups {
				groupIDsMap[gid] = true
			}
		}

		for gid := range groupIDsMap {
			if err := s.buildAndSaveGroupInventory(txCtx, gid); err != nil {
				return err
			}
		}

		return nil
	})
}

// buildAndSaveMainInventory builds the main (ungrouped) inventory and saves it
// along with an outbox event. Must be called within a transaction.
func (s *VMService) buildAndSaveMainInventory(ctx context.Context) error {
	mainInventory, err := duckdb_parser.New(s.store.Querier(), nil).BuildInventory(ctx, nil)
	if err != nil {
		return fmt.Errorf("building main inventory: %w", err)
	}

	mainInventoryData, err := json.Marshal(converters.ToAPI(mainInventory))
	if err != nil {
		return fmt.Errorf("marshaling main inventory: %w", err)
	}

	if err := s.store.Inventory().Save(ctx, mainInventoryData); err != nil {
		return fmt.Errorf("updating main inventory: %w", err)
	}

	return s.store.Outbox().Insert(ctx, models.Event{
		Kind: models.InventoryUpdateEvent,
		Data: mainInventoryData,
	})
}

// buildAndSaveGroupInventory builds an inventory for a group's matched VMs and
// saves it along with an outbox event. Must be called within a transaction.
func (s *VMService) buildAndSaveGroupInventory(ctx context.Context, groupID uuid.UUID) error {
	vmIDs, err := s.store.Group().GetMatchedIDs(ctx, groupID)
	if err != nil {
		return fmt.Errorf("getting matched VM IDs for group %s: %w", groupID, err)
	}

	var inv *inventory.Inventory
	if len(vmIDs) > 0 {
		inv, err = duckdb_parser.New(s.store.Querier(), nil).BuildInventory(ctx, vmIDs)
		if err != nil {
			return fmt.Errorf("building inventory for group %s: %w", groupID, err)
		}
	}

	if err := s.store.Group().UpdateInventory(ctx, groupID, inv); err != nil {
		return fmt.Errorf("updating inventory for group %s: %w", groupID, err)
	}

	group, err := s.store.Group().Get(ctx, groupID)
	if err != nil {
		return fmt.Errorf("getting group %s: %w", groupID, err)
	}

	var invJSON json.RawMessage
	if inv != nil {
		apiInventory := converters.ToAPI(inv)
		invBytes, err := json.Marshal(apiInventory)
		if err != nil {
			return fmt.Errorf("marshaling inventory for group %s: %w", groupID, err)
		}
		invJSON = invBytes
	} else {
		invJSON = json.RawMessage("null")
	}

	payload := models.GroupInventoryEventPayload{
		GroupID:   groupID.String(),
		GroupName: group.Name,
		Inventory: invJSON,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling event payload for group %s: %w", groupID, err)
	}

	if err := s.store.Outbox().Insert(ctx, models.Event{
		Kind: models.GroupInventoryUpsertEvent,
		Data: payloadBytes,
	}); err != nil {
		return fmt.Errorf("adding group inventory event for group %s: %w", groupID, err)
	}

	return nil
}

// deduplicateStrings removes duplicate strings from a slice while preserving order.
func deduplicateStrings(input []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))

	for _, s := range input {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	return result
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
