package services

import (
	"context"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
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

func (s *GroupService) List(ctx context.Context) ([]models.Group, error) {
	return s.store.Group().List(ctx)
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
