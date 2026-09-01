package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

// Column name constants for vm_inspection_status_v2v table
const (
	inspectionV2VTable      = "vm_inspection_status_v2v"
	inspectionV2VColVmID    = `"VM ID"`
	inspectionV2VColStatus  = "status"
	inspectionV2VColError   = "error"
	inspectionV2VColDetails = "details"
)

// Column name constants for vm_inspection_concerns_v2v table
const (
	vmInspectionConcernsV2VTable           = "vm_inspection_concerns_v2v"
	vmInspectionConcernsV2VColVMID         = `"VM ID"`
	vmInspectionConcernsV2VColInspectionID = "inspection_id"
	vmInspectionConcernsV2VColCategory     = "category"
	vmInspectionConcernsV2VColLabel        = "label"
	vmInspectionConcernsV2VColMsg          = "msg"
	vmInspectionIDV2VSeq                   = "vm_inspection_id_v2v_seq"
)

type InspectionStoreV2V struct {
	db QueryInterceptor
}

func NewInspectionStoreV2V(db QueryInterceptor) *InspectionStoreV2V {
	return &InspectionStoreV2V{db: db}
}

// Update upserts the v2v inspection status for a VM.
func (s *InspectionStoreV2V) Update(ctx context.Context, vmID string, status models.InspectionStatus) error {
	var errStr *string
	if status.Error != nil {
		e := status.Error.Error()
		errStr = &e
	}

	query, args, err := sq.Insert(inspectionV2VTable).
		Columns(inspectionV2VColVmID, inspectionV2VColStatus, inspectionV2VColError, inspectionV2VColDetails).
		Values(vmID, status.State.Value(), errStr, status.Details).
		Suffix("ON CONFLICT (" + inspectionV2VColVmID + ") DO UPDATE SET " +
			inspectionV2VColStatus + " = EXCLUDED." + inspectionV2VColStatus + ", " +
			inspectionV2VColError + " = EXCLUDED." + inspectionV2VColError + ", " +
			inspectionV2VColDetails + " = EXCLUDED." + inspectionV2VColDetails).
		ToSql()
	if err != nil {
		return fmt.Errorf("building update query for vm %s: %w", vmID, err)
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("updating v2v inspection status for vm %s: %w", vmID, err)
	}
	return nil
}

// GetStatus returns the current v2v inspection status for a VM.
// Returns a NotStarted status when no row exists for the VM.
func (s *InspectionStoreV2V) GetStatus(ctx context.Context, vmID string) (models.InspectionStatus, error) {
	query, args, err := sq.Select(
		inspectionV2VColStatus,
		inspectionV2VColError,
		inspectionV2VColDetails,
	).From(inspectionV2VTable).
		Where(sq.Eq{inspectionV2VColVmID: vmID}).
		ToSql()
	if err != nil {
		return models.InspectionStatus{}, fmt.Errorf("building get v2v status query for vm %s: %w", vmID, err)
	}

	var state string
	var errStr, details sql.NullString
	err = s.db.QueryRowContext(ctx, query, args...).Scan(&state, &errStr, &details)
	if errors.Is(err, sql.ErrNoRows) {
		return models.InspectionStatus{State: models.InspectionStateNotStarted}, nil
	}
	if err != nil {
		return models.InspectionStatus{}, fmt.Errorf("getting v2v status for vm %s: %w", vmID, err)
	}

	status := models.InspectionStatus{
		State:   models.InspectionState(state),
		Details: details.String,
	}
	if errStr.Valid && errStr.String != "" {
		status.Error = errors.New(errStr.String)
	}
	return status, nil
}

// ##### V2V Inspection concerns (per-run rows keyed by inspection_id)

func (s *InspectionStoreV2V) insertConcerns(ctx context.Context, vmID string, inspectionID int64, concerns []models.VmInspectionConcern) error {
	if len(concerns) == 0 {
		return nil
	}

	builder := sq.Insert(vmInspectionConcernsV2VTable).
		Columns(
			vmInspectionConcernsV2VColVMID,
			vmInspectionConcernsV2VColInspectionID,
			vmInspectionConcernsV2VColCategory,
			vmInspectionConcernsV2VColLabel,
			vmInspectionConcernsV2VColMsg,
		)
	for _, c := range concerns {
		builder = builder.Values(vmID, inspectionID, c.Category, c.Label, c.Msg)
	}
	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("building insert v2v inspection concerns: %w", err)
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("inserting v2v inspection concerns for vm %s inspection %d: %w", vmID, inspectionID, err)
	}
	return nil
}

// PurgeConcerns deletes all v2v concerns for a VM. Typically called before inserting fresh results.
func (s *InspectionStoreV2V) PurgeConcerns(ctx context.Context, vmID string) error {
	query, args, err := sq.Delete(vmInspectionConcernsV2VTable).
		Where(sq.Eq{vmInspectionConcernsV2VColVMID: vmID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building purge v2v concerns query: %w", err)
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("purging v2v concerns for vm %s: %w", vmID, err)
	}
	return nil
}

func (s *InspectionStoreV2V) InsertResult(ctx context.Context, vmID string, concerns []models.VmInspectionConcern) error {
	if len(concerns) == 0 {
		return nil
	}
	var inspectionID int64
	err := s.db.QueryRowContext(ctx, "SELECT nextval('"+vmInspectionIDV2VSeq+"')").Scan(&inspectionID)
	if err != nil {
		return fmt.Errorf("allocating v2v inspection id for vm %s: %w", vmID, err)
	}
	return s.insertConcerns(ctx, vmID, inspectionID, concerns)
}

func (s *InspectionStoreV2V) ListResults(ctx context.Context, vmID string) ([]models.VmInspectionResult, error) {
	query, args, err := sq.Select(
		"c."+vmInspectionConcernsV2VColInspectionID,
		"c."+vmInspectionConcernsV2VColCategory,
		"c."+vmInspectionConcernsV2VColLabel,
		"c."+vmInspectionConcernsV2VColMsg,
	).From(vmInspectionConcernsV2VTable+" c").
		Where(sq.Eq{`c.` + vmInspectionConcernsV2VColVMID: vmID}).
		OrderBy("c."+vmInspectionConcernsV2VColInspectionID+" DESC", "c.id").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list v2v inspection results: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("executing list v2v inspection results: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []models.VmInspectionResult
	var cur *models.VmInspectionResult
	var lastID int64 = -1

	for rows.Next() {
		var inspectionID int64
		var cat, label, msg sql.NullString
		if err := rows.Scan(&inspectionID, &cat, &label, &msg); err != nil {
			return nil, fmt.Errorf("scanning v2v vm inspection result row: %w", err)
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
		return nil, fmt.Errorf("iterating v2v vm inspection results: %w", err)
	}
	return out, nil
}
