package services

import (
	"context"

	sq "github.com/Masterminds/squirrel"
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

	// Query user info from vm_user_info
	userInfo, err := s.store.VM().GetUserInfo(ctx, id)
	if err != nil {
		// Log warning but don't fail - default to empty user info
		zap.S().Named("vm_service").Warnw("failed to query user info", "vmID", id, "error", err)
	} else {
		vm.MigrationExcluded = userInfo.MigrationExcluded
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
	filters, opts := s.buildListOptions(params)

	if len(params.Sort) == 0 {
		opts = append(opts, store.WithDefaultSort())
	}

	vms, err := s.store.VM().List(ctx, filters, opts...)
	if err != nil {
		return nil, 0, err
	}

	countFilters, _ := s.buildListOptions(VMListParams{
		Expression: params.Expression,
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

	if params.Expression != "" {
		filters = append(filters, store.ByFilter(params.Expression))
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

// UpdateMigrationExcluded updates the migration exclusion status for a VM.
func (s *VMService) UpdateMigrationExcluded(ctx context.Context, id string, excluded bool) error {
	// Verify VM exists first
	_, err := s.store.VM().Get(ctx, id)
	if err != nil {
		return err
	}

	return s.store.VM().UpdateMigrationExcluded(ctx, id, excluded)
}
