package store

import (
	"context"
	"database/sql"
	"errors"

	sq "github.com/Masterminds/squirrel"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

type InspectionStore struct {
	db QueryInterceptor
}

func NewInspectionStore(db QueryInterceptor) *InspectionStore {
	return &InspectionStore{db: db}
}

// Get returns the inspection status for a VM by its moid.
func (s *InspectionStore) Get(ctx context.Context, vmMoid string) (*models.InspectionStatus, error) {
	query, args, err := sq.Select("vm_moid", "status", "error").
		From("vm_inspection").
		Where(sq.Eq{"vm_moid": vmMoid}).
		ToSql()
	if err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	var moid, status string
	var errStr sql.NullString
	err = row.Scan(&moid, &status, &errStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	result := &models.InspectionStatus{
		State: models.InspectionState(status),
	}
	if errStr.Valid {
		result.Error = errors.New(errStr.String)
	}
	return result, nil
}

// List returns inspection statuses matching the filter. If filter is nil, returns all.
func (s *InspectionStore) List(ctx context.Context, filter *InspectionQueryFilter) (map[string]models.InspectionStatus, error) {
	builder := sq.Select("vm_moid", "status", "error").From("vm_inspection")

	if filter != nil {
		builder = filter.apply(builder)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]models.InspectionStatus)
	for rows.Next() {
		var vmMoid, status string
		var errStr sql.NullString
		if err := rows.Scan(&vmMoid, &status, &errStr); err != nil {
			return nil, err
		}
		inspStatus := models.InspectionStatus{
			State: models.InspectionState(status),
		}
		if errStr.Valid {
			inspStatus.Error = errors.New(errStr.String)
		}
		result[vmMoid] = inspStatus
	}

	return result, rows.Err()
}

// Upsert inserts or updates the inspection status for a VM.
func (s *InspectionStore) Upsert(ctx context.Context, vmMoid string, status models.InspectionStatus) error {
	var errStr *string
	if status.Error != nil {
		e := status.Error.Error()
		errStr = &e
	}

	query, args, err := sq.Insert("vm_inspection").
		Columns("vm_moid", "status", "error").
		Values(vmMoid, status.State.Value(), errStr).
		Suffix("ON CONFLICT (vm_moid) DO UPDATE SET status = EXCLUDED.status, error = EXCLUDED.error").
		ToSql()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}

// UpsertMany inserts or updates specific inspection status for multiple VMs.
func (s *InspectionStore) UpsertMany(
	ctx context.Context,
	vmsMoid []string,
	status models.InspectionState,
) error {
	builder := sq.Insert("vm_inspection").
		Columns("vm_moid", "status")

	for _, vmMoid := range vmsMoid {
		builder = builder.Values(vmMoid, status.Value())
	}

	query, args, err := builder.
		Suffix("ON CONFLICT (vm_moid) DO UPDATE SET status = EXCLUDED.status").
		ToSql()
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}

// Delete removes the inspection status for a VM.
func (s *InspectionStore) Delete(ctx context.Context, vmMoid string) error {
	query, args, err := sq.Delete("vm_inspection").
		Where(sq.Eq{"vm_moid": vmMoid}).
		ToSql()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}

// DeleteAll removes all inspection statuses.
func (s *InspectionStore) DeleteAll(ctx context.Context) error {
	query, args, err := sq.Delete("vm_inspection").ToSql()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}

type InspectionFilterFunc func(sq.SelectBuilder) sq.SelectBuilder

type InspectionQueryFilter struct {
	filters []InspectionFilterFunc
}

func NewInspectionQueryFilter() *InspectionQueryFilter {
	return &InspectionQueryFilter{
		filters: make([]InspectionFilterFunc, 0),
	}
}

func (f *InspectionQueryFilter) Add(filter InspectionFilterFunc) *InspectionQueryFilter {
	f.filters = append(f.filters, filter)
	return f
}

func (f *InspectionQueryFilter) ByVmMoids(vmMoids ...string) *InspectionQueryFilter {
	if len(vmMoids) == 0 {
		return f
	}
	return f.Add(func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Where(sq.Eq{"vm_moid": vmMoids})
	})
}

func (f *InspectionQueryFilter) ByStatus(statuses ...models.InspectionState) *InspectionQueryFilter {
	if len(statuses) == 0 {
		return f
	}
	statusStrings := make([]string, len(statuses))
	for i, s := range statuses {
		statusStrings[i] = s.Value()
	}
	return f.Add(func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Where(sq.Eq{"status": statusStrings})
	})
}

func (f *InspectionQueryFilter) Limit(limit int) *InspectionQueryFilter {
	return f.Add(func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Limit(uint64(limit))
	})
}

func (f *InspectionQueryFilter) apply(builder sq.SelectBuilder) sq.SelectBuilder {
	for _, filter := range f.filters {
		builder = filter(builder)
	}
	return builder
}
