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

// UpdateMigrationExcluded updates the migration exclusion status for a VM.
func (s *VMService) UpdateMigrationExcluded(ctx context.Context, id string, excluded bool) error {
	// Verify VM exists first
	_, err := s.store.VM().Get(ctx, id)
	if err != nil {
		return err
	}

	return s.store.VM().UpdateMigrationExcluded(ctx, id, excluded)
}
