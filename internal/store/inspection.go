package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// Column name constants for vm_inspection_status table
const (
	inspectionTable       = "vm_inspection_status"
	inspectionColVmID     = `"VM ID"`
	inspectionColStatus   = "status"
	inspectionColError    = "error"
	inspectionColSequence = "sequence"
)

type InspectionStore struct {
	db QueryInterceptor
}

func NewInspectionStore(db QueryInterceptor) *InspectionStore {
	return &InspectionStore{db: db}
}

// Get returns the inspection status for a VM by its ID.
func (s *InspectionStore) Get(ctx context.Context, vmID string) (*models.InspectionStatus, error) {
	query, args, err := sq.Select(inspectionColVmID, inspectionColStatus, inspectionColError).
		From(inspectionTable).
		Where(sq.Eq{inspectionColVmID: vmID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building query for vm %s: %w", vmID, err)
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	var id, status string
	var errStr sql.NullString
	err = row.Scan(&id, &status, &errStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, srvErrors.NewResourceNotFoundError("vm inspection status", vmID)
	}
	if err != nil {
		return nil, fmt.Errorf("scanning inspection for vm %s: %w", vmID, err)
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
	builder := sq.Select(inspectionColVmID, inspectionColStatus, inspectionColError).From(inspectionTable)

	if filter != nil {
		builder = filter.Apply(builder)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("executing list query: %w", err)
	}
	defer rows.Close()

	result := make(map[string]models.InspectionStatus)
	for rows.Next() {
		var vmID, status string
		var errStr sql.NullString
		if err := rows.Scan(&vmID, &status, &errStr); err != nil {
			return nil, fmt.Errorf("scanning inspection row: %w", err)
		}
		inspStatus := models.InspectionStatus{
			State: models.InspectionState(status),
		}
		if errStr.Valid {
			inspStatus.Error = errors.New(errStr.String)
		}
		result[vmID] = inspStatus
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating inspection rows: %w", err)
	}
	return result, nil
}

// First returns the VM ID for pending inspection with the lowest sequence value.
// Returns empty string and sql.ErrNoRows if no matching record is found.
func (s *InspectionStore) First(ctx context.Context) (string, error) {
	builder := sq.Select(inspectionColVmID).
		From(inspectionTable).
		OrderBy(inspectionColSequence + " ASC").
		Where(sq.Eq{inspectionColStatus: models.InspectionStatePending.Value()}).
		Limit(1)

	query, args, err := builder.ToSql()
	if err != nil {
		return "", fmt.Errorf("building first pending query: %w", err)
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	var vmID string
	err = row.Scan(&vmID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", sql.ErrNoRows
		}
		return "", fmt.Errorf("scanning first pending vm: %w", err)
	}

	return vmID, nil
}

// Add inserts new inspection statuses for multiple VMs. Existing VMs are ignored.
// The sequence is automatically assigned by the database based on insertion order.
func (s *InspectionStore) Add(ctx context.Context, vmIDs []string, status models.InspectionState) error {
	if len(vmIDs) == 0 {
		return nil
	}

	builder := sq.Insert(inspectionTable).
		Columns(inspectionColVmID, inspectionColStatus)

	for _, vmID := range vmIDs {
		builder = builder.Values(vmID, status.Value())
	}

	query, args, err := builder.
		Suffix("ON CONFLICT (" + inspectionColVmID + ") DO NOTHING").
		ToSql()
	if err != nil {
		return fmt.Errorf("building add query: %w", err)
	}

	_, err = s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("executing add for %d vms: %w", len(vmIDs), err)
	}
	return nil
}

// Update updates inspection status for VMs matching the filter.
func (s *InspectionStore) Update(ctx context.Context, filter *InspectionUpdateFilter, status models.InspectionStatus) error {
	var errStr *string
	if status.Error != nil {
		e := status.Error.Error()
		errStr = &e
	}

	builder := sq.Update(inspectionTable).
		Set(inspectionColStatus, status.State.Value()).
		Set(inspectionColError, errStr)

	if filter != nil {
		builder = filter.Apply(builder)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("building update query: %w", err)
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("executing update: %w", err)
	}
	return nil
}

// DeleteAll removes all inspection statuses.
func (s *InspectionStore) DeleteAll(ctx context.Context) error {
	query, args, err := sq.Delete(inspectionTable).ToSql()
	if err != nil {
		return fmt.Errorf("building delete all query: %w", err)
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("deleting all inspections: %w", err)
	}
	return nil
}
