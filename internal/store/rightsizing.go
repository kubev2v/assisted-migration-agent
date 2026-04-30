package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

const (
	rsReportsTable                  = "rightsizing_reports"
	rsReportsColID                  = "id"
	rsReportsColVCenter             = "vcenter"
	rsReportsColClusterID           = "cluster_id"
	rsReportsColIntervalID          = "interval_id"
	rsReportsColWindowStart         = "window_start"
	rsReportsColWindowEnd           = "window_end"
	rsReportsColExpectedSampleCount = "expected_sample_count"
	rsReportsColExpectedBatchCount  = "expected_batch_count"
	rsReportsColWrittenBatchCount   = "written_batch_count"
	rsReportsColCreatedAt           = "created_at"

	rsMetricsTable          = "rightsizing_metrics"
	rsMetricsColReportID    = "report_id"
	rsMetricsColVMName      = "vm_name"
	rsMetricsColMOID        = "moid"
	rsMetricsColMetricKey   = "metric_key"
	rsMetricsColSampleCount = "sample_count"
	rsMetricsColAverage     = "average"
	rsMetricsColP95         = "p95"
	rsMetricsColP99         = "p99"
	rsMetricsColMax         = "max"
	rsMetricsColLatest      = "latest"

	vinfoTable   = "vinfo"
	vinfoColVMID = "VM ID"
	vinfoColName = "VM"

	rsWarningsTable       = "rightsizing_vm_warnings"
	rsWarningsColReportID = "report_id"
	rsWarningsColMOID     = "moid"
	rsWarningsColVMName   = "vm_name"
	rsWarningsColWarning  = "warning"
)

// RightSizingStore persists rightsizing report metadata and per-VM metric aggregates.
type RightSizingStore struct {
	db QueryInterceptor
}

func NewRightSizingStore(db QueryInterceptor) *RightSizingStore {
	return &RightSizingStore{db: db}
}

// CreateReport inserts a new report record and returns its UUID.
// expectedBatchCount = ceil(vmCount / batchSize).
func (s *RightSizingStore) CreateReport(ctx context.Context, r models.RightSizingReport, vmCount, batchSize int) (string, error) {
	if batchSize <= 0 {
		return "", fmt.Errorf("batchSize must be > 0, got %d", batchSize)
	}
	id := uuid.New().String()
	expectedBatches := int(math.Ceil(float64(vmCount) / float64(batchSize)))

	query, args, err := sq.Insert(rsReportsTable).
		Columns(
			rsReportsColID, rsReportsColVCenter, rsReportsColClusterID, rsReportsColIntervalID,
			rsReportsColWindowStart, rsReportsColWindowEnd,
			rsReportsColExpectedSampleCount, rsReportsColExpectedBatchCount,
		).
		Values(
			id, r.VCenter, r.ClusterID, r.IntervalID,
			r.WindowStart, r.WindowEnd,
			r.ExpectedSampleCount, expectedBatches,
		).
		ToSql()
	if err != nil {
		return "", fmt.Errorf("building create report query: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return "", fmt.Errorf("inserting report: %w", err)
	}
	return id, nil
}

// WriteBatch inserts metric rows for a slice of RightSizingMetrics.
// Rows with zero SampleCount are skipped. Duplicate rows are silently ignored.
func (s *RightSizingStore) WriteBatch(ctx context.Context, reportID string, metrics []models.RightSizingMetric) error {
	builder := sq.Insert(rsMetricsTable).
		Columns(
			rsMetricsColReportID, rsMetricsColVMName, rsMetricsColMOID, rsMetricsColMetricKey,
			rsMetricsColSampleCount, rsMetricsColAverage, rsMetricsColP95, rsMetricsColP99,
			rsMetricsColMax, rsMetricsColLatest,
		)

	hasRows := false
	for _, m := range metrics {
		if m.SampleCount == 0 {
			continue
		}
		builder = builder.Values(
			reportID, m.VMName, m.MOID, m.MetricKey,
			m.SampleCount, m.Average, m.P95, m.P99, m.Max, m.Latest,
		)
		hasRows = true
	}

	if !hasRows {
		return nil
	}

	query, args, err := builder.Suffix(
		"ON CONFLICT (" + rsMetricsColReportID + ", " + rsMetricsColMOID + ", " + rsMetricsColMetricKey + ") DO NOTHING",
	).ToSql()
	if err != nil {
		return fmt.Errorf("building write batch query: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("inserting metrics batch: %w", err)
	}
	return nil
}

// IncrementWrittenBatchCount increments written_batch_count by 1 for the given report.
// Call this inside the same WithTx block as WriteBatch so the increment is atomic with the inserts.
func (s *RightSizingStore) IncrementWrittenBatchCount(ctx context.Context, reportID string) error {
	query, args, err := sq.Update(rsReportsTable).
		Set(rsReportsColWrittenBatchCount, sq.Expr(rsReportsColWrittenBatchCount+" + 1")).
		Where(sq.Eq{rsReportsColID: reportID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building increment query: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("incrementing written_batch_count: %w", err)
	}
	return nil
}

// UpdateExpectedBatchCount sets expected_batch_count = ceil(vmCount/batchSize).
// Called after VM discovery, once the real VM count is known.
func (s *RightSizingStore) UpdateExpectedBatchCount(ctx context.Context, reportID string, vmCount, batchSize int) error {
	if batchSize <= 0 {
		return fmt.Errorf("batchSize must be > 0, got %d", batchSize)
	}
	expectedBatches := int(math.Ceil(float64(vmCount) / float64(batchSize)))

	query, args, err := sq.Update(rsReportsTable).
		Set(rsReportsColExpectedBatchCount, expectedBatches).
		Where(sq.Eq{rsReportsColID: reportID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building update expected batch count query: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("updating expected_batch_count: %w", err)
	}
	return nil
}

// WriteVMWarnings persists one warning row per VM that was queried but had no metrics.
// Duplicate rows (same report_id/moid) are silently ignored.
func (s *RightSizingStore) WriteVMWarnings(ctx context.Context, reportID string, warnings []models.VMWarning) error {
	if len(warnings) == 0 {
		return nil
	}

	builder := sq.Insert(rsWarningsTable).
		Columns(rsWarningsColReportID, rsWarningsColMOID, rsWarningsColVMName, rsWarningsColWarning)

	for _, w := range warnings {
		builder = builder.Values(reportID, w.MOID, w.VMName, w.Warning)
	}

	query, args, err := builder.Suffix("ON CONFLICT DO NOTHING").ToSql()
	if err != nil {
		return fmt.Errorf("building write VM warnings query: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("inserting VM warnings: %w", err)
	}
	return nil
}

// ComputeAndStoreUtilization computes per-VM utilization percentages from collected
// metrics and the vinfo inventory, persisting them to rightsizing_vm_utilization.
// Uses a single SQL pivot query; idempotent via ON CONFLICT DO NOTHING.
func (s *RightSizingStore) ComputeAndStoreUtilization(ctx context.Context, reportID string) error {
	query := `
INSERT INTO rightsizing_vm_utilization
    (report_id, moid, vm_name,
     cpu_avg_pct, cpu_p95_pct, cpu_max_pct, cpu_latest_pct,
     mem_avg_pct, mem_p95_pct, mem_max_pct, mem_latest_pct,
     disk_pct, confidence_pct)
SELECT
    rm.report_id,
    rm.moid,
    MAX(rm.vm_name) AS vm_name,
    MAX(CASE WHEN rm.metric_key = 'cpu.usage.average' THEN rm.average  / 100.0 END) AS cpu_avg_pct,
    MAX(CASE WHEN rm.metric_key = 'cpu.usage.average' THEN rm.p95     / 100.0 END) AS cpu_p95_pct,
    MAX(CASE WHEN rm.metric_key = 'cpu.usage.average' THEN rm.max     / 100.0 END) AS cpu_max_pct,
    MAX(CASE WHEN rm.metric_key = 'cpu.usage.average' THEN rm.latest  / 100.0 END) AS cpu_latest_pct,
    MAX(CASE WHEN rm.metric_key = 'mem.consumed.average' THEN rm.average / NULLIF(v."Memory" * 1024.0, 0) * 100.0 END) AS mem_avg_pct,
    MAX(CASE WHEN rm.metric_key = 'mem.consumed.average' THEN rm.p95     / NULLIF(v."Memory" * 1024.0, 0) * 100.0 END) AS mem_p95_pct,
    MAX(CASE WHEN rm.metric_key = 'mem.consumed.average' THEN rm.max     / NULLIF(v."Memory" * 1024.0, 0) * 100.0 END) AS mem_max_pct,
    MAX(CASE WHEN rm.metric_key = 'mem.consumed.average' THEN rm.latest  / NULLIF(v."Memory" * 1024.0, 0) * 100.0 END) AS mem_latest_pct,
    MAX(CASE WHEN rm.metric_key = 'disk.used.latest'        THEN rm.latest END)
      / NULLIF(MAX(CASE WHEN rm.metric_key = 'disk.provisioned.latest' THEN rm.latest END), 0)
      * 100.0 AS disk_pct,
    MAX(CASE WHEN rm.metric_key = 'cpu.usage.average' THEN rm.sample_count END)
      * 100.0 / NULLIF(r.expected_sample_count, 0) AS confidence_pct
FROM rightsizing_metrics rm
LEFT JOIN vinfo v ON v."VM ID" = rm.moid
JOIN rightsizing_reports r ON r.id = rm.report_id
WHERE rm.report_id = ?
GROUP BY rm.report_id, rm.moid, r.expected_sample_count
ON CONFLICT DO NOTHING`

	if _, err := s.db.ExecContext(ctx, query, reportID); err != nil {
		return fmt.Errorf("computing VM utilization: %w", err)
	}
	return nil
}

// GetVMUtilization returns the full utilization breakdown for a VM from the latest
// completed report (written_batch_count > 0). Returns ResourceNotFoundError if no
// rightsizing data exists for this VM.
func (s *RightSizingStore) GetVMUtilization(ctx context.Context, moid string) (*models.VmUtilizationDetails, error) {
	query := `
SELECT moid, vm_name,
       cpu_avg_pct, cpu_p95_pct, cpu_max_pct, cpu_latest_pct,
       mem_avg_pct, mem_p95_pct, mem_max_pct, mem_latest_pct,
       disk_pct, confidence_pct
FROM rightsizing_vm_utilization
WHERE moid = ?
  AND report_id = (
      SELECT id FROM rightsizing_reports
      WHERE written_batch_count > 0
      ORDER BY created_at DESC LIMIT 1
  )`

	var d models.VmUtilizationDetails
	var (
		cpuAvg, cpuP95, cpuMax, cpuLatest sql.NullFloat64
		memAvg, memP95, memMax, memLatest sql.NullFloat64
		disk, confidence                  sql.NullFloat64
	)
	err := s.db.QueryRowContext(ctx, query, moid).Scan(
		&d.MOID, &d.VMName,
		&cpuAvg, &cpuP95, &cpuMax, &cpuLatest,
		&memAvg, &memP95, &memMax, &memLatest,
		&disk, &confidence,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, srvErrors.NewResourceNotFoundError("vm rightsizing", moid)
	}
	if err != nil {
		return nil, fmt.Errorf("scanning VM utilization: %w", err)
	}
	d.CpuAvg = cpuAvg.Float64
	d.CpuP95 = cpuP95.Float64
	d.CpuMax = cpuMax.Float64
	d.CpuLatest = cpuLatest.Float64
	d.MemAvg = memAvg.Float64
	d.MemP95 = memP95.Float64
	d.MemMax = memMax.Float64
	d.MemLatest = memLatest.Float64
	d.Disk = disk.Float64
	d.Confidence = confidence.Float64
	return &d, nil
}

// ListInventoryVMs reads VM IDs and names from the local inventory (vinfo table).
// "VM ID" is the MoRef value; "VM" is the display name.
// Returns all entries ordered by name.
func (s *RightSizingStore) ListInventoryVMs(ctx context.Context) ([]models.InventoryVM, error) {
	builder := sq.Select(
		fmt.Sprintf(`"%s"`, vinfoColVMID),
		fmt.Sprintf(`"%s"`, vinfoColName),
	).From(vinfoTable).
		OrderBy(fmt.Sprintf(`"%s"`, vinfoColName))

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list inventory VMs query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("executing list inventory VMs query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []models.InventoryVM
	for rows.Next() {
		var vm models.InventoryVM
		if err := rows.Scan(&vm.ID, &vm.Name); err != nil {
			return nil, fmt.Errorf("scanning inventory VM row: %w", err)
		}
		result = append(result, vm)
	}
	return result, rows.Err()
}

// ListReports returns metadata for all rightsizing reports ordered by creation time descending.
// VM metrics are not included; use GetReport to retrieve the full report with metrics.
// Returns an empty slice (not nil) when no reports exist.
func (s *RightSizingStore) ListReports(ctx context.Context) ([]models.RightsizingReportSummary, error) {
	query, args, err := sq.Select(
		rsReportsColID, rsReportsColVCenter, rsReportsColClusterID, rsReportsColIntervalID,
		rsReportsColWindowStart, rsReportsColWindowEnd, rsReportsColExpectedSampleCount, rsReportsColCreatedAt,
	).From(rsReportsTable).
		OrderBy(rsReportsColCreatedAt + " DESC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list reports query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("executing list reports query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	reports := []models.RightsizingReportSummary{}
	for rows.Next() {
		var r models.RightsizingReportSummary
		if err := rows.Scan(
			&r.ID, &r.VCenter, &r.ClusterID, &r.IntervalID,
			&r.WindowStart, &r.WindowEnd, &r.ExpectedSampleCount, &r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning report row: %w", err)
		}
		reports = append(reports, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating report rows: %w", err)
	}

	return reports, nil
}

// GetReport returns a single rightsizing report by ID with all VM metrics populated.
// Returns a ResourceNotFoundError if the ID does not exist.
func (s *RightSizingStore) GetReport(ctx context.Context, id string) (*models.RightsizingReport, error) {
	query, args, err := sq.Select(
		rsReportsColID, rsReportsColVCenter, rsReportsColClusterID, rsReportsColIntervalID,
		rsReportsColWindowStart, rsReportsColWindowEnd, rsReportsColExpectedSampleCount, rsReportsColCreatedAt,
	).From(rsReportsTable).
		Where(sq.Eq{rsReportsColID: id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building get report query: %w", err)
	}

	var r models.RightsizingReport
	err = s.db.QueryRowContext(ctx, query, args...).Scan(
		&r.ID, &r.VCenter, &r.ClusterID, &r.IntervalID,
		&r.WindowStart, &r.WindowEnd, &r.ExpectedSampleCount, &r.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, srvErrors.NewResourceNotFoundError("rightsizing report", id)
	}
	if err != nil {
		return nil, fmt.Errorf("scanning report: %w", err)
	}

	r.VMs = []models.RightsizingVMReport{}
	reports := []models.RightsizingReport{r}
	if err := s.appendMetrics(ctx, reports, map[string]int{r.ID: 0}); err != nil {
		return nil, err
	}
	if err := s.appendVMWarnings(ctx, reports, map[string]int{r.ID: 0}); err != nil {
		return nil, err
	}
	return &reports[0], nil
}

// appendMetrics fetches all metric rows for the given reports (by index map)
// and builds the nested VMs structure in place.
func (s *RightSizingStore) appendMetrics(ctx context.Context, reports []models.RightsizingReport, idxByID map[string]int) error {
	ids := make([]string, 0, len(idxByID))
	for id := range idxByID {
		ids = append(ids, id)
	}

	query, args, err := sq.Select(
		rsMetricsColReportID, rsMetricsColVMName, rsMetricsColMOID, rsMetricsColMetricKey,
		rsMetricsColSampleCount, rsMetricsColAverage, rsMetricsColP95, rsMetricsColP99,
		rsMetricsColMax, rsMetricsColLatest,
	).From(rsMetricsTable).
		Where(sq.Eq{rsMetricsColReportID: ids}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building metrics query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("executing metrics query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// vmIdx[reportID][moid] = index in reports[rIdx].VMs
	vmIdx := make(map[string]map[string]int)

	for rows.Next() {
		var reportID, vmName, moid, metricKey string
		var stats models.RightsizingMetricStats
		if err := rows.Scan(
			&reportID, &vmName, &moid, &metricKey,
			&stats.SampleCount, &stats.Average, &stats.P95, &stats.P99, &stats.Max, &stats.Latest,
		); err != nil {
			return fmt.Errorf("scanning metric row: %w", err)
		}

		rIdx := idxByID[reportID]
		if vmIdx[reportID] == nil {
			vmIdx[reportID] = make(map[string]int)
		}

		vIdx, ok := vmIdx[reportID][moid]
		if !ok {
			reports[rIdx].VMs = append(reports[rIdx].VMs, models.RightsizingVMReport{
				Name:    vmName,
				MOID:    moid,
				Metrics: map[string]models.RightsizingMetricStats{},
			})
			vIdx = len(reports[rIdx].VMs) - 1
			vmIdx[reportID][moid] = vIdx
		}

		reports[rIdx].VMs[vIdx].Metrics[metricKey] = stats
	}

	return rows.Err()
}

// appendVMWarnings reads warning-only VMs and merges them into reports[rIdx].VMs
// with an empty Metrics map and populated Warnings slice.
func (s *RightSizingStore) appendVMWarnings(ctx context.Context, reports []models.RightsizingReport, idxByID map[string]int) error {
	ids := make([]string, 0, len(idxByID))
	for id := range idxByID {
		ids = append(ids, id)
	}

	query, args, err := sq.Select(
		rsWarningsColReportID, rsWarningsColMOID, rsWarningsColVMName, rsWarningsColWarning,
	).From(rsWarningsTable).
		Where(sq.Eq{rsWarningsColReportID: ids}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building VM warnings query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("executing VM warnings query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var reportID, moid, vmName, warning string
		if err := rows.Scan(&reportID, &moid, &vmName, &warning); err != nil {
			return fmt.Errorf("scanning VM warning row: %w", err)
		}
		rIdx := idxByID[reportID]
		reports[rIdx].VMs = append(reports[rIdx].VMs, models.RightsizingVMReport{
			Name:     vmName,
			MOID:     moid,
			Metrics:  map[string]models.RightsizingMetricStats{},
			Warnings: []string{warning},
		})
	}
	return rows.Err()
}
