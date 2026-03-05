package services

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/filter"
)

type GroupService struct {
	store *store.Store
}

func NewGroupService(st *store.Store) *GroupService {
	return &GroupService{store: st}
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
		expr := fmt.Sprintf("name = '%s'", params.ByName)
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
	group, err := s.store.Group().Get(ctx, id)
	if err != nil {
		return []models.VirtualMachineSummary{}, 0, err
	}

	var filters []sq.Sqlizer
	if f := store.ByFilter(group.Filter); f != nil {
		filters = append(filters, f)
	}

	var opts []store.ListOption
	if len(params.Sort) > 0 {
		sortParams := make([]store.SortParam, len(params.Sort))
		for i, sf := range params.Sort {
			sortParams[i] = store.SortParam{Field: sf.Field, Desc: sf.Desc}
		}
		opts = append(opts, store.WithSort(sortParams))
	} else {
		opts = append(opts, store.WithDefaultSort())
	}

	total, err := s.store.VM().Count(ctx, filters...)
	if err != nil {
		return []models.VirtualMachineSummary{}, 0, err
	}

	if params.Limit > 0 {
		opts = append(opts, store.WithLimit(params.Limit))
	}
	if params.Offset > 0 {
		opts = append(opts, store.WithOffset(params.Offset))
	}

	vms, err := s.store.VM().List(ctx, filters, opts...)
	if err != nil {
		return []models.VirtualMachineSummary{}, 0, err
	}

	return vms, total, nil
}

func (s *GroupService) Create(ctx context.Context, group models.Group) (*models.Group, error) {
	return s.store.Group().Create(ctx, group)
}

func (s *GroupService) Update(ctx context.Context, id int, group models.Group) (*models.Group, error) {
	return s.store.Group().Update(ctx, id, group)
}

func (s *GroupService) Delete(ctx context.Context, id int) error {
	return s.store.Group().Delete(ctx, id)
}
