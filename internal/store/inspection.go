package store

import (
	"context"
	"database/sql"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
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

// Update upserts the inspection status for a VM.
func (s *InspectionStore) Update(ctx context.Context, vmID string, status models.InspectionStatus) error {
	var errStr *string
	if status.Error != nil {
		e := status.Error.Error()
		errStr = &e
	}

	query, args, err := sq.Insert(inspectionTable).
		Columns(inspectionColVmID, inspectionColStatus, inspectionColError).
		Values(vmID, status.State.Value(), errStr).
		Suffix("ON CONFLICT (" + inspectionColVmID + ") DO UPDATE SET " +
			inspectionColStatus + " = EXCLUDED." + inspectionColStatus + ", " +
			inspectionColError + " = EXCLUDED." + inspectionColError).
		ToSql()
	if err != nil {
		return fmt.Errorf("building update query for vm %s: %w", vmID, err)
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("updating inspection status for vm %s: %w", vmID, err)
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
