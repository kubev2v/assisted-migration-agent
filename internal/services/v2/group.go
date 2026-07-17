package v2

import (
	"context"
	"encoding/json"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"

	"github.com/kubev2v/migration-planner/pkg/inventory"
	"github.com/kubev2v/migration-planner/pkg/inventory/converters"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/filter"
)

const (
	filterByNameExpression = "name like '%s'"
)

type InventoryBuilder interface {
	BuildInventory(ctx context.Context, vmIDs []string) (*inventory.Inventory, error)
}

type GroupService struct {
	store            *store.Store2
	inventoryBuilder InventoryBuilder
}

func NewGroupService(st *store.Store2, builder InventoryBuilder) *GroupService {
	return &GroupService{
		store:            st,
		inventoryBuilder: builder,
	}
}

type GroupGetParams struct {
	Sort   []SortField
	Limit  uint64
	Offset uint64
}

type GroupListParams struct {
	ByName string
	Limit  uint64
	Offset uint64
}

func (s *GroupService) List(ctx context.Context, params GroupListParams) ([]models.Group, int, error) {
	var filters []sq.Sqlizer
	if params.ByName != "" {
		expr := fmt.Sprintf(filterByNameExpression, params.ByName)
		f, err := filter.ParseWithGroupMap([]byte(expr))
		if err != nil {
			return nil, 0, fmt.Errorf("invalid name filter: %w", err)
		}
		filters = append(filters, f)
	}

	total, err := s.store.Group().Count(ctx, filters...)
	if err != nil {
		return nil, 0, err
	}

	groups, err := s.store.Group().List(ctx, filters, params.Limit, params.Offset)
	if err != nil {
		return nil, 0, err
	}

	return groups, total, nil
}

func (s *GroupService) Get(ctx context.Context, id uuid.UUID) (*models.Group, error) {
	return s.store.Group().Get(ctx, id)
}

func (s *GroupService) ListVirtualMachines(ctx context.Context, id uuid.UUID, params GroupGetParams) ([]models.VirtualMachineSummary, int, error) {
	if _, err := s.store.Group().Get(ctx, id); err != nil {
		return []models.VirtualMachineSummary{}, 0, err
	}

	vmIDs, err := s.store.Group().GetMatchedIDs(ctx, id)
	if err != nil {
		return []models.VirtualMachineSummary{}, 0, err
	}

	total := len(vmIDs)

	var opts []store.ListOption
	opts = append(opts, store.WithVMIDs(vmIDs))

	if len(params.Sort) > 0 {
		sortParams := make([]store.SortParam, len(params.Sort))
		for i, sf := range params.Sort {
			sortParams[i] = store.SortParam{Field: sf.Field, Desc: sf.Desc}
		}
		opts = append(opts, store.WithSort(sortParams))
	} else {
		opts = append(opts, store.WithDefaultSort())
	}

	if params.Limit > 0 {
		opts = append(opts, store.WithLimit(params.Limit))
	}
	if params.Offset > 0 {
		opts = append(opts, store.WithOffset(params.Offset))
	}

	vms, err := s.store.VM().List(ctx, nil, opts...)
	if err != nil {
		return []models.VirtualMachineSummary{}, 0, err
	}

	return vms, total, nil
}

func (s *GroupService) Create(ctx context.Context, group models.Group) (*models.Group, error) {
	var created *models.Group

	err := s.store.WithTx(ctx, func(txCtx context.Context) error {
		var err error
		created, err = s.store.Group().Create(txCtx, group)
		if err != nil {
			return err
		}

		if err := s.store.Group().RefreshMatches(txCtx, created.ID); err != nil {
			return err
		}

		vmIDs, err := s.store.Group().GetMatchedIDs(txCtx, created.ID)
		if err != nil {
			return fmt.Errorf("getting matched VM IDs: %w", err)
		}

		inv, err := s.buildGroupInventory(txCtx, vmIDs)
		if err != nil {
			return fmt.Errorf("building group inventory: %w", err)
		}

		if err := s.store.Group().UpdateInventory(txCtx, created.ID, inv); err != nil {
			return fmt.Errorf("updating group inventory: %w", err)
		}

		created.Inventory = inv

		// Add outbox event for group creation
		if err := s.addGroupInventoryEvent(txCtx, models.GroupInventoryUpsertEvent, created); err != nil {
			return fmt.Errorf("adding group inventory event: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return created, nil
}

func (s *GroupService) Update(ctx context.Context, id uuid.UUID, group models.Group) (*models.Group, error) {
	var updated *models.Group

	err := s.store.WithTx(ctx, func(txCtx context.Context) error {
		var err error
		updated, err = s.store.Group().Update(txCtx, id, group)
		if err != nil {
			return err
		}

		if err := s.store.Group().RefreshMatches(txCtx, id); err != nil {
			return err
		}

		vmIDs, err := s.store.Group().GetMatchedIDs(txCtx, id)
		if err != nil {
			return fmt.Errorf("getting matched VM IDs: %w", err)
		}

		inv, err := s.buildGroupInventory(txCtx, vmIDs)
		if err != nil {
			return fmt.Errorf("building group inventory: %w", err)
		}

		if err := s.store.Group().UpdateInventory(txCtx, id, inv); err != nil {
			return fmt.Errorf("updating group inventory: %w", err)
		}

		updated.Inventory = inv

		// Add outbox event for group update
		if err := s.addGroupInventoryEvent(txCtx, models.GroupInventoryUpsertEvent, updated); err != nil {
			return fmt.Errorf("adding group inventory event: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return updated, nil
}

func (s *GroupService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.store.WithTx(ctx, func(txCtx context.Context) error {
		// Get group info before deletion for the event
		group, err := s.store.Group().Get(txCtx, id)
		if err != nil {
			return err
		}

		// Add delete event BEFORE actual deletion
		payload := models.GroupInventoryDeleteEventPayload{
			GroupID:   id.String(),
			GroupName: group.Name,
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshaling delete event payload: %w", err)
		}

		if err := s.store.Outbox().Insert(txCtx, models.Event{
			Kind: models.GroupInventoryDeleteEvent,
			Data: payloadBytes,
		}); err != nil {
			return fmt.Errorf("adding group delete event: %w", err)
		}

		if err := s.store.Group().Delete(txCtx, id); err != nil {
			return err
		}
		return s.store.Group().DeleteMatches(txCtx, id)
	})
}

// buildGroupInventory creates a subset inventory containing infrastructure
// relevant to the VMs matched by the group.
func (s *GroupService) buildGroupInventory(ctx context.Context, vmIDs []string) (*inventory.Inventory, error) {
	if len(vmIDs) == 0 {
		return nil, nil
	}

	inv, err := s.inventoryBuilder.BuildInventory(ctx, vmIDs)
	if err != nil {
		return nil, fmt.Errorf("building filtered inventory: %w", err)
	}

	return inv, nil
}

// addGroupInventoryEvent creates an outbox event for group inventory changes.
// Must be called within a transaction context.
// Event payload contains: groupID, groupName, and inventory.
// Fields like vmsCount and vCenterID are extracted from inventory when processing the event.
// Always emits an event, even for empty groups, to ensure cross-system consistency.
func (s *GroupService) addGroupInventoryEvent(ctx context.Context, eventKind models.EventKind, group *models.Group) error {
	// Prepare inventory as JSON
	var invJSON json.RawMessage
	if group.Inventory != nil {
		// Convert domain inventory to API type before marshaling
		apiInventory := converters.ToAPI(group.Inventory)
		invBytes, err := json.Marshal(apiInventory)
		if err != nil {
			return fmt.Errorf("marshaling inventory: %w", err)
		}
		invJSON = invBytes
	} else {
		// Empty inventory - use JSON null to indicate the group has no VMs
		invJSON = json.RawMessage("null")
	}

	// Create typed event payload
	payload := models.GroupInventoryEventPayload{
		GroupID:   group.ID.String(),
		GroupName: group.Name,
		Inventory: invJSON,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling event payload: %w", err)
	}

	return s.store.Outbox().Insert(ctx, models.Event{
		Kind: eventKind,
		Data: payloadBytes,
	})
}
