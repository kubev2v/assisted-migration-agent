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

// Column name constants for vm_inspection_concerns table
const (
	vmInspectionConcernsTable           = "vm_inspection_concerns"
	vmInspectionConcernsColVMID         = `"VM ID"`
	vmInspectionConcernsColInspectionID = "inspection_id"
	vmInspectionConcernsColCategory     = "category"
	vmInspectionConcernsColLabel        = "label"
	vmInspectionConcernsColMsg          = "msg"
	vmInspectionIDSeq                   = "vm_inspection_id_seq"
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
	defer func() {
		_ = rows.Close()
	}()

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

// ##### Inspection concerns (per-run rows keyed by inspection_id)

func (s *InspectionStore) insertConcerns(ctx context.Context, vmID string, inspectionID int64, concerns []models.VmInspectionConcern) error {
	if len(concerns) == 0 {
		return nil
	}

	builder := sq.Insert(vmInspectionConcernsTable).
		Columns(
			vmInspectionConcernsColVMID,
			vmInspectionConcernsColInspectionID,
			vmInspectionConcernsColCategory,
			vmInspectionConcernsColLabel,
			vmInspectionConcernsColMsg,
		)
	for _, c := range concerns {
		builder = builder.Values(vmID, inspectionID, c.Category, c.Label, c.Msg)
	}
	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("building insert inspection concerns: %w", err)
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("inserting inspection concerns for vm %s inspection %d: %w", vmID, inspectionID, err)
	}
	return nil
}

func (s *InspectionStore) InsertResult(ctx context.Context, vmID string, concerns []models.VmInspectionConcern) error {
	if len(concerns) == 0 {
		return nil
	}
	var inspectionID int64
	err := s.db.QueryRowContext(ctx, "SELECT nextval('"+vmInspectionIDSeq+"')").Scan(&inspectionID)
	if err != nil {
		return fmt.Errorf("allocating inspection id for vm %s: %w", vmID, err)
	}
	return s.insertConcerns(ctx, vmID, inspectionID, concerns)
}

func (s *InspectionStore) ListResults(ctx context.Context, vmID string) ([]models.VmInspectionResult, error) {
	query, args, err := sq.Select(
		"c."+vmInspectionConcernsColInspectionID,
		"c."+vmInspectionConcernsColCategory,
		"c."+vmInspectionConcernsColLabel,
		"c."+vmInspectionConcernsColMsg,
	).From(vmInspectionConcernsTable+" c").
		Where(sq.Eq{`c.` + vmInspectionConcernsColVMID: vmID}).
		OrderBy("c."+vmInspectionConcernsColInspectionID+" DESC", "c.id").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list inspection results: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("executing list inspection results: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []models.VmInspectionResult
	var cur *models.VmInspectionResult
	var lastID int64 = -1

	for rows.Next() {
		var inspectionID int64
		var cat, label, msg sql.NullString
		if err := rows.Scan(&inspectionID, &cat, &label, &msg); err != nil {
			return nil, fmt.Errorf("scanning vm inspection result row: %w", err)
		}
		if inspectionID != lastID {
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &models.VmInspectionResult{
				InspectionID: inspectionID,
				VMID:         vmID,
				Concerns:     []models.VmInspectionConcern{},
			}
			lastID = inspectionID
		}
		if cur != nil {
			cur.Concerns = append(cur.Concerns, models.VmInspectionConcern{
				Category: cat.String,
				Label:    label.String,
				Msg:      msg.String,
			})
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating vm inspection results: %w", err)
	}
	return out, nil
}
