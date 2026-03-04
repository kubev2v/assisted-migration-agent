package services

import (
	"context"

	sq "github.com/Masterminds/squirrel"

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
	Clusters      []string
	Statuses      []string
	Expression    string
	MinIssues     int
	DiskSizeMin   *int64
	DiskSizeMax   *int64
	MemorySizeMin *int64
	MemorySizeMax *int64
	Sort          []SortField
	Limit         uint64
	Offset        uint64
}

func (s *VMService) Get(ctx context.Context, id string) (*models.VM, error) {
	return s.store.VM().Get(ctx, id)
}

func (s *VMService) List(ctx context.Context, params VMListParams) ([]models.VirtualMachineSummary, int, error) {
	filters, opts := s.buildListOptions(params)

	if len(params.Sort) == 0 {
		opts = append(opts, store.WithDefaultSort())
	}

	vms, err := s.store.VM().List(ctx, filters, opts...)
	if err != nil {
		return nil, 0, err
	}

	countFilters, _ := s.buildListOptions(VMListParams{
		Clusters:      params.Clusters,
		Statuses:      params.Statuses,
		Expression:    params.Expression,
		MinIssues:     params.MinIssues,
		DiskSizeMin:   params.DiskSizeMin,
		DiskSizeMax:   params.DiskSizeMax,
		MemorySizeMin: params.MemorySizeMin,
		MemorySizeMax: params.MemorySizeMax,
	})
	total, err := s.store.VM().Count(ctx, countFilters...)
	if err != nil {
		return nil, 0, err
	}

	return vms, total, nil
}

func (s *VMService) buildListOptions(params VMListParams) ([]sq.Sqlizer, []store.ListOption) {
	var filters []sq.Sqlizer
	var opts []store.ListOption

	if len(params.Clusters) > 0 {
		filters = append(filters, store.ByClusters(params.Clusters...))
	}
	if len(params.Statuses) > 0 {
		filters = append(filters, store.ByStatus(params.Statuses...))
	}
	if params.Expression != "" {
		filters = append(filters, store.ByFilter(params.Expression))
	}
	if params.MinIssues > 0 {
		filters = append(filters, store.ByIssues(params.MinIssues))
	}

	if params.DiskSizeMin != nil || params.DiskSizeMax != nil {
		min := int64(0)
		max := int64(1 << 62)
		if params.DiskSizeMin != nil {
			min = *params.DiskSizeMin
		}
		if params.DiskSizeMax != nil {
			max = *params.DiskSizeMax
		}
		filters = append(filters, store.ByDiskSizeRange(min, max))
	}

	if params.MemorySizeMin != nil || params.MemorySizeMax != nil {
		min := int64(0)
		max := int64(1 << 62)
		if params.MemorySizeMin != nil {
			min = *params.MemorySizeMin
		}
		if params.MemorySizeMax != nil {
			max = *params.MemorySizeMax
		}
		filters = append(filters, store.ByMemorySizeRange(min, max))
	}

	if len(params.Sort) > 0 {
		sortParams := make([]store.SortParam, len(params.Sort))
		for i, s := range params.Sort {
			sortParams[i] = store.SortParam{Field: s.Field, Desc: s.Desc}
		}
		opts = append(opts, store.WithSort(sortParams))
	}

	if params.Limit > 0 {
		opts = append(opts, store.WithLimit(params.Limit))
	}
	if params.Offset > 0 {
		opts = append(opts, store.WithOffset(params.Offset))
	}

	return filters, opts
}
