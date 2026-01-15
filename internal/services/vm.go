package services

import (
	"context"

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

func (s *VMService) List(ctx context.Context, params VMListParams) ([]models.VMSummary, int, error) {
	opts := s.buildListOptions(params)

	if len(params.Sort) == 0 {
		opts = append(opts, store.WithDefaultSort())
	}

	vms, err := s.store.VM().List(ctx, opts...)
	if err != nil {
		return nil, 0, err
	}

	// Get total count without pagination
	countOpts := s.buildListOptions(VMListParams{
		Clusters:      params.Clusters,
		Statuses:      params.Statuses,
		MinIssues:     params.MinIssues,
		DiskSizeMin:   params.DiskSizeMin,
		DiskSizeMax:   params.DiskSizeMax,
		MemorySizeMin: params.MemorySizeMin,
		MemorySizeMax: params.MemorySizeMax,
	})
	total, err := s.store.VM().Count(ctx, countOpts...)
	if err != nil {
		return nil, 0, err
	}

	return vms, total, nil
}

func (s *VMService) buildListOptions(params VMListParams) []store.ListOption {
	var opts []store.ListOption

	if len(params.Clusters) > 0 {
		opts = append(opts, store.ByClusters(params.Clusters...))
	}
	if len(params.Statuses) > 0 {
		opts = append(opts, store.ByStatus(params.Statuses...))
	}
	if params.MinIssues > 0 {
		opts = append(opts, store.ByIssues(params.MinIssues))
	}

	// Handle disk size filter (values in MB)
	if params.DiskSizeMin != nil || params.DiskSizeMax != nil {
		min := int64(0)
		max := int64(1 << 62) // effectively no upper limit
		if params.DiskSizeMin != nil {
			min = *params.DiskSizeMin
		}
		if params.DiskSizeMax != nil {
			max = *params.DiskSizeMax
		}
		opts = append(opts, store.ByDiskSizeRange(min, max))
	}

	// Handle memory size filter (values in MB)
	if params.MemorySizeMin != nil || params.MemorySizeMax != nil {
		min := int64(0)
		max := int64(1 << 62) // effectively no upper limit
		if params.MemorySizeMin != nil {
			min = *params.MemorySizeMin
		}
		if params.MemorySizeMax != nil {
			max = *params.MemorySizeMax
		}
		opts = append(opts, store.ByMemorySizeRange(min, max))
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

	return opts
}
