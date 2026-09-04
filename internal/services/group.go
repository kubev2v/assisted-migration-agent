package services

import (
	"context"
	"encoding/json"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"

	"github.com/kubev2v/migration-planner/pkg/inventory"
	"github.com/kubev2v/migration-planner/pkg/inventory/converters"

	vmfilter "github.com/kubev2v/assisted-migration-agent/internal/filter"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

const (
	filterByNameExpression = "name like '%s'"
)

type InventoryBuilder interface {
	BuildInventory(ctx context.Context, vmIDs []string) (*inventory.Inventory, error)
}

type GroupService struct {
	store            *store.Store
	inventoryBuilder InventoryBuilder
	eventSrv         *EventService
}

func NewGroupService(st *store.Store, builder InventoryBuilder) *GroupService {
	return &GroupService{
		store:            st,
		inventoryBuilder: builder,
		eventSrv:         NewEventService(st),
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
		f, err := vmfilter.ParseWithGroupMap([]byte(expr))
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

		// If group has no VMs we consider it a legal group, but no need to send an inventory in this case
		if len(vmIDs) == 0 {
			return nil
		}

		inv, err := s.inventoryBuilder.BuildInventory(txCtx, vmIDs)
		if err != nil {
			return fmt.Errorf("building filtered inventory: %w", err)
		}

		if err := s.store.Group().UpdateInventory(txCtx, created.ID, inv); err != nil {
			return fmt.Errorf("updating group inventory: %w", err)
		}

		created.Inventory = inv

		data, err := buildGroupInventoryEventData(created)
		if err != nil {
			return fmt.Errorf("building inventory event data: %w", err)
		}

		return s.eventSrv.AddGroupInventoryEvent(txCtx, data)
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

		if len(vmIDs) == 0 {
			if err := s.store.Group().UpdateInventory(txCtx, id, nil); err != nil {
				return fmt.Errorf("clearing group inventory: %w", err)
			}
			updated.Inventory = nil

			data, err := buildGroupInventoryDeleteEventData(updated)
			if err != nil {
				return fmt.Errorf("building inventory delete event data: %w", err)
			}

			return s.eventSrv.AddGroupInventoryDeleteEvent(txCtx, data)
		}

		inv, err := s.inventoryBuilder.BuildInventory(txCtx, vmIDs)
		if err != nil {
			return fmt.Errorf("building filtered inventory: %w", err)
		}

		if err := s.store.Group().UpdateInventory(txCtx, id, inv); err != nil {
			return fmt.Errorf("updating group inventory: %w", err)
		}

		updated.Inventory = inv

		// Add outbox event for group update
		data, err := buildGroupInventoryEventData(updated)
		if err != nil {
			return fmt.Errorf("building inventory event data: %w", err)
		}

		return s.eventSrv.AddGroupInventoryEvent(txCtx, data)
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

		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshaling delete event payload: %w", err)
		}

		if err := s.eventSrv.AddGroupInventoryDeleteEvent(txCtx, data); err != nil {
			return fmt.Errorf("adding group delete event: %w", err)
		}

		if err := s.store.Group().Delete(txCtx, id); err != nil {
			return err
		}
		return s.store.Group().DeleteMatches(txCtx, id)
	})
}

func buildGroupInventoryEventData(group *models.Group) ([]byte, error) {
	apiInventory := converters.ToAPI(group.Inventory)

	invJSON, err := json.Marshal(apiInventory)
	if err != nil {
		return nil, fmt.Errorf("marshaling inventory: %w", err)
	}

	payload := models.GroupInventoryEventPayload{
		GroupID:   group.ID.String(),
		GroupName: group.Name,
		Inventory: invJSON,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("building group inventory event payload: %w", err)
	}

	return data, nil
}

func buildGroupInventoryDeleteEventData(group *models.Group) ([]byte, error) {
	payload := models.GroupInventoryDeleteEventPayload{
		GroupID:   group.ID.String(),
		GroupName: group.Name,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("building group inventory delete event payload: %w", err)
	}

	return data, nil
}
