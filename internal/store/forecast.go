package store

import (
	"context"
	"database/sql"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

const (
	forecastRunsTable  = "forecast_runs"
	forecastSessionSeq = "forecast_session_seq"
)

// ForecastStore provides data access for forecast benchmark runs and
// datastore capability information.
type ForecastStore struct {
	db QueryInterceptor
}

// NewForecastStore creates a new ForecastStore.
func NewForecastStore(db QueryInterceptor) *ForecastStore {
	return &ForecastStore{db: db}
}

// NextSessionID allocates a new session ID for a forecast run.
func (s *ForecastStore) NextSessionID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, "SELECT nextval('"+forecastSessionSeq+"')").Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("allocating session id: %w", err)
	}
	return id, nil
}

// InsertRun persists a single benchmark run result.
func (s *ForecastStore) InsertRun(ctx context.Context, run models.BenchmarkRun) error {
	query, args, err := sq.Insert(forecastRunsTable).
		Columns(
			"session_id", "pair_name", "source_datastore", "target_datastore",
			"iteration", "disk_size_gb", "prep_duration_sec", "duration_sec", "throughput_mbps",
			"method", "error",
		).
		Values(
			run.SessionID, run.PairName, run.SourceDS, run.TargetDS,
			run.Iteration, run.DiskSizeGB, run.PrepDurationSec, run.DurationSec, run.ThroughputMBps,
			run.Method, sql.NullString{String: run.Error, Valid: run.Error != ""},
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("building insert run query: %w", err)
	}

	_, err = s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("inserting benchmark run: %w", err)
	}

	return nil
}

// ListRuns returns all benchmark runs for a given pair, ordered by creation time descending.
func (s *ForecastStore) ListRuns(ctx context.Context, pairName string) ([]models.BenchmarkRun, error) {
	builder := sq.Select(
		"id", "session_id", "pair_name", "source_datastore", "target_datastore",
		"iteration", "disk_size_gb", "prep_duration_sec", "duration_sec", "throughput_mbps",
		"method", "error", "created_at",
	).From(forecastRunsTable)

	if pairName != "" {
		builder = builder.Where(sq.Eq{"pair_name": pairName})
	}

	builder = builder.OrderBy("created_at DESC")

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list runs query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("executing list runs query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanBenchmarkRuns(rows)
}

// DeleteRun deletes a specific benchmark run by its ID.
func (s *ForecastStore) DeleteRun(ctx context.Context, runID int64) error {
	query, args, err := sq.Delete(forecastRunsTable).
		Where(sq.Eq{"id": runID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building delete run query: %w", err)
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("deleting run %d: %w", runID, err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if affected == 0 {
		return srvErrors.NewResourceNotFoundError("run", fmt.Sprintf("%d", runID))
	}

	return nil
}

// DatastoreBacking holds the raw backing-device data from the vdatastore inventory table.
type DatastoreBacking struct {
	Name           string
	Type           string
	CapacityMiB    float64
	FreeMiB        float64
	BackingDevices string // JSON array, e.g. '["naa.600a..."]'
}

// ListDatastoreDetails returns datastore records from the inventory, including
// backing device identifiers needed for vendor and array derivation.
func (s *ForecastStore) ListDatastoreDetails(ctx context.Context) ([]DatastoreBacking, error) {
	query := `SELECT "Name", "Type", "Capacity MiB", "Free MiB", "Backing Devices" FROM vdatastore`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying vdatastore: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []DatastoreBacking
	for rows.Next() {
		var d DatastoreBacking
		if err := rows.Scan(&d.Name, &d.Type, &d.CapacityMiB, &d.FreeMiB, &d.BackingDevices); err != nil {
			return nil, fmt.Errorf("scanning vdatastore row: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating vdatastore rows: %w", err)
	}

	return result, nil
}

func scanBenchmarkRuns(rows *sql.Rows) ([]models.BenchmarkRun, error) {
	var result []models.BenchmarkRun
	for rows.Next() {
		var run models.BenchmarkRun
		var method, errStr sql.NullString
		if err := rows.Scan(
			&run.ID, &run.SessionID, &run.PairName, &run.SourceDS, &run.TargetDS,
			&run.Iteration, &run.DiskSizeGB, &run.PrepDurationSec, &run.DurationSec, &run.ThroughputMBps,
			&method, &errStr, &run.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning benchmark run row: %w", err)
		}
		run.Method = method.String
		run.Error = errStr.String
		result = append(result, run)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating benchmark run rows: %w", err)
	}

	return result, nil
}
