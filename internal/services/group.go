package services

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/migration-planner/pkg/inventory"

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
	store            *store.Store
	inventoryBuilder InventoryBuilder
}

func NewGroupService(st *store.Store) *GroupService {
	return &GroupService{
		store:            st,
		inventoryBuilder: st.Parser(),
	}
}

// NewGroupServiceWithInventoryBuilder creates a GroupService with a custom inventory builder.
// Used for testing to inject mocks.
func NewGroupServiceWithInventoryBuilder(st *store.Store, builder InventoryBuilder) *GroupService {
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

func (s *GroupService) Get(ctx context.Context, id int) (*models.Group, error) {
	return s.store.Group().Get(ctx, id)
}

func (s *GroupService) ListVirtualMachines(ctx context.Context, id int, params GroupGetParams) ([]models.VirtualMachineSummary, int, error) {
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
		return nil
	})
	if err != nil {
		return nil, err
	}

	return created, nil
}

func (s *GroupService) Update(ctx context.Context, id int, group models.Group) (*models.Group, error) {
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
		return nil
	})
	if err != nil {
		return nil, err
	}

	return updated, nil
}

func (s *GroupService) Delete(ctx context.Context, id int) error {
	return s.store.WithTx(ctx, func(txCtx context.Context) error {
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
