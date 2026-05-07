package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	sq "github.com/Masterminds/squirrel"
	duckdb_models "github.com/kubev2v/migration-planner/pkg/duckdb_parser/models"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/filter"
)

type VMStore struct {
	db QueryInterceptor
}

func NewVMStore(db QueryInterceptor) *VMStore {
	return &VMStore{db: db}
}

// FilterOption is a SQL WHERE condition for filtering VMs in the flat filter subquery.
type FilterOption = sq.Sqlizer

// List returns VM summaries with filters, sorting, and pagination.
func (s *VMStore) List(ctx context.Context, filter sq.Sqlizer, opts ...ListOption) ([]models.VirtualMachineSummary, error) {
	builder := vmOutputQuery.
		Columns(
			`u.cpu_p95_pct AS cpu_p95_pct`,
			`u.mem_p95_pct AS mem_p95_pct`,
			`u.disk_pct    AS disk_pct`,
			`u.confidence_pct AS confidence_pct`,
		).
		LeftJoin(`rightsizing_vm_utilization u ON u.moid = v."VM ID" AND u.report_id = (` +
			`SELECT id FROM rightsizing_reports WHERE written_batch_count > 0 ORDER BY created_at DESC LIMIT 1` +
			`)`)

	// Apply external filters via subquery (filters reference table aliases in vmFilterSubquery)
	if filter != nil {
		subquery := vmFilterSubquery.Where(filter)

		subSQL, subArgs, err := subquery.ToSql()
		if err != nil {
			return nil, err
		}

		builder = builder.Where(sq.Expr(fmt.Sprintf(`v."VM ID" IN (%s)`, subSQL), subArgs...))
	}

	// Apply options (sort, limit, offset)
	for _, opt := range opts {
		builder = opt(builder)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var vms []models.VirtualMachineSummary
	for rows.Next() {
		var vm models.VirtualMachineSummary
		var sqlErr string
		var inspectionConcernCount int
		var tags StringArray
		var migrationExcluded bool
		var labels StringArray
		err := rows.Scan(
			&vm.ID,
			&vm.Name,
			&vm.PowerState,
			&vm.Cluster,
			&vm.Datacenter,
			&vm.Memory,
			&vm.DiskSize,
			&vm.IssueCount,
			&vm.InspectionStatus.State,
			&vm.IsTemplate,
			&vm.IsMigratable,
			&sqlErr,
			&inspectionConcernCount,
			&tags,
			&migrationExcluded,
			&labels,
			&vm.UtilizationCpuP95,
			&vm.UtilizationMemP95,
			&vm.UtilizationDisk,
			&vm.UtilizationConfidence,
		)
		if err != nil {
			return nil, err
		}
		if sqlErr != "" {
			vm.InspectionStatus.Error = errors.New(sqlErr)
		}
		vm.InspectionConcernCount = inspectionConcernCount
		vm.Tags = tags
		vm.MigrationExcluded = migrationExcluded
		vm.Labels = labels
		vms = append(vms, vm)
	}

	return vms, rows.Err()
}

// Count returns the total number of VMs matching the filters.
func (s *VMStore) Count(ctx context.Context, filter sq.Sqlizer) (int, error) {
	builder := sq.Select("COUNT(*)").From("vinfo v")

	if filter != nil {
		subquery := vmFilterSubquery.Where(filter)
		subSQL, subArgs, err := subquery.ToSql()
		if err != nil {
			return 0, err
		}
		builder = builder.Where(sq.Expr(fmt.Sprintf(`v."VM ID" IN (%s)`, subSQL), subArgs...))
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return 0, err
	}

	var count int
	err = s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

// Get returns full VM details by ID, including utilization data from the latest rightsizing report.
func (s *VMStore) Get(ctx context.Context, id string) (*models.VM, error) {
	rows, err := s.db.QueryContext(ctx, vmGetQuery, id)
	if err != nil {
		return nil, fmt.Errorf("querying VM %s: %w", id, err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, srvErrors.NewResourceNotFoundError("vm", id)
	}

	var pvm duckdb_models.VM
	var (
		uMoid                                 sql.NullString
		uVmName                               sql.NullString
		uProvCpus                             sql.NullInt64
		uProvMemMb                            sql.NullInt64
		uProvDiskKb                           sql.NullFloat64
		uCpuAvg, uCpuP95, uCpuMax, uCpuLatest sql.NullFloat64
		uMemAvg, uMemP95, uMemMax, uMemLatest sql.NullFloat64
		uDisk, uConfidence                    sql.NullFloat64
	)

	if err := rows.Scan(
		&pvm.ID, &pvm.Name, &pvm.Folder, &pvm.Host, &pvm.UUID,
		&pvm.Firmware, &pvm.PowerState, &pvm.ConnectionState,
		&pvm.FaultToleranceEnabled, &pvm.CpuCount, &pvm.MemoryMB,
		&pvm.GuestName, &pvm.GuestNameFromVmwareTools, &pvm.HostName,
		&pvm.IpAddress, &pvm.StorageUsed, &pvm.IsTemplate,
		&pvm.ChangeTrackingEnabled, &pvm.DiskEnableUuid, &pvm.Datacenter,
		&pvm.Cluster, &pvm.HWVersion, &pvm.TotalDiskCapacityMiB,
		&pvm.ProvisionedMiB, &pvm.ResourcePool, &pvm.OsDiskComplexity,
		&pvm.MigrationExcluded, &pvm.Labels,
		&pvm.CpuHotAddEnabled, &pvm.CpuHotRemoveEnabled, &pvm.CpuSockets,
		&pvm.CoresPerSocket, &pvm.MemoryHotAddEnabled, &pvm.BalloonedMemory,
		&pvm.Disks, &pvm.NICs, &pvm.Networks, &pvm.Concerns,
		&uMoid, &uVmName, &uProvCpus, &uProvMemMb, &uProvDiskKb,
		&uCpuAvg, &uCpuP95, &uCpuMax, &uCpuLatest,
		&uMemAvg, &uMemP95, &uMemMax, &uMemLatest,
		&uDisk, &uConfidence,
	); err != nil {
		return nil, fmt.Errorf("scanning VM %s: %w", id, err)
	}

	for i := range pvm.Disks {
		pvm.Disks[i].ChangeTrackingEnabled = pvm.ChangeTrackingEnabled
	}

	result := fromDB(pvm)

	if uMoid.Valid {
		result.Utilization = &models.VmUtilizationDetails{
			MOID:                uMoid.String,
			VMName:              uVmName.String,
			ProvisionedCpus:     int(uProvCpus.Int64),
			ProvisionedMemoryMb: int(uProvMemMb.Int64),
			ProvisionedDiskKb:   uProvDiskKb.Float64,
			CpuAvg:              uCpuAvg.Float64,
			CpuP95:              uCpuP95.Float64,
			CpuMax:              uCpuMax.Float64,
			CpuLatest:           uCpuLatest.Float64,
			MemAvg:              uMemAvg.Float64,
			MemP95:              uMemP95.Float64,
			MemMax:              uMemMax.Float64,
			MemLatest:           uMemLatest.Float64,
			Disk:                uDisk.Float64,
			Confidence:          uConfidence.Float64,
		}
	}

	return &result, nil
}

// normalizeCategory validates and normalizes an issue category (case-insensitive).
func normalizeCategory(category, issueID string) string {
	// Valid issue categories (lowercase for case-insensitive comparison)
	var validCategories = map[string]string{
		"critical":    "Critical",
		"warning":     "Warning",
		"information": "Information",
		"advisory":    "Advisory",
		"error":       "Error",
	}

	normalized, ok := validCategories[strings.ToLower(category)]
	if ok {
		return normalized
	}
	zap.S().Named("vm_store").Warnw(
		"Unknown issue category encountered, mapping to 'Other'",
		"category", category,
		"issueID", issueID,
	)
	return "Other"
}

func fromDB(pvm duckdb_models.VM) models.VM {
	issues := make([]models.Issue, 0, len(pvm.Concerns))
	criticalCount := 0
	for _, c := range pvm.Concerns {
		normalizedCategory := normalizeCategory(c.Category, c.Id)
		issues = append(issues, models.Issue{
			ID:          c.Id,
			Label:       c.Label,
			Description: c.Assessment,
			Category:    normalizedCategory,
		})
		if normalizedCategory == "Critical" {
			criticalCount++
		}
	}

	disks := make([]models.Disk, 0, len(pvm.Disks))
	var totalDiskCapacityMiB int64
	for _, d := range pvm.Disks {
		disks = append(disks, models.Disk{
			File:     d.File,
			Capacity: d.Capacity,
			Shared:   d.Shared,
			RDM:      d.RDM,
			Bus:      d.Bus,
			Mode:     d.Mode,
		})
		totalDiskCapacityMiB += d.Capacity
	}

	nics := make([]models.NIC, 0, len(pvm.NICs))
	for i, n := range pvm.NICs {
		nics = append(nics, models.NIC{
			MAC:     n.MAC,
			Network: n.Network.ID,
			Index:   i,
		})
	}

	return models.VM{
		ID:                    pvm.ID,
		Name:                  pvm.Name,
		UUID:                  pvm.UUID,
		Firmware:              pvm.Firmware,
		PowerState:            pvm.PowerState,
		ConnectionState:       pvm.ConnectionState,
		Host:                  pvm.Host,
		Folder:                pvm.Folder,
		Datacenter:            pvm.Datacenter,
		Cluster:               pvm.Cluster,
		CpuCount:              pvm.CpuCount,
		CoresPerSocket:        pvm.CoresPerSocket,
		MemoryMB:              pvm.MemoryMB,
		GuestName:             pvm.GuestName,
		HostName:              pvm.HostName,
		IPAddress:             pvm.IpAddress,
		DiskSize:              totalDiskCapacityMiB,
		StorageUsed:           int64(pvm.StorageUsed),
		IsTemplate:            pvm.IsTemplate,
		IsMigratable:          criticalCount == 0,
		MigrationExcluded:     pvm.MigrationExcluded,
		FaultToleranceEnabled: pvm.FaultToleranceEnabled,
		Disks:                 disks,
		NICs:                  nics,
		Issues:                issues,
		Labels:                pvm.Labels,
	}
}

// ListOption modifies a SELECT query for sorting/pagination.
type ListOption func(sq.SelectBuilder) sq.SelectBuilder

// SortParam represents a single sort parameter with field name and direction.
type SortParam struct {
	Field string
	Desc  bool
}

// ByFilter applies a raw filter DSL expression.
// Returns nil if the expression is empty or fails to parse.
func ByFilter(expr string) sq.Sqlizer {
	if expr == "" {
		return nil
	}
	sqlizer, err := filter.ParseWithDefaultMap([]byte(expr))
	if err != nil {
		zap.S().Named("vm_store").Warnw("failed to parse filter expression", "expression", expr, "error", err)
	}
	return sqlizer
}

// WithVMIDs filters the output query to only include VMs with the given IDs.
// This bypasses the filter subquery, using pre-computed group match results.
func WithVMIDs(ids []string) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Where(sq.Eq{`v."VM ID"`: ids})
	}
}

// WithLimit sets the LIMIT clause.
func WithLimit(limit uint64) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Limit(limit)
	}
}

// WithOffset sets the OFFSET clause.
func WithOffset(offset uint64) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Offset(offset)
	}
}

// WithDefaultSort applies default sorting by VM ID.
func WithDefaultSort() ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.OrderBy("id")
	}
}

// WithSort applies multi-field sorting using output aliases.
func WithSort(sorts []SortParam) ListOption {
	apiFieldToDBColumn := map[string]string{
		"name":         "name",
		"vCenterState": "power_state",
		"cluster":      "cluster",
		"diskSize":     "disk_size",
		"memory":       "memory",
		"issues":       "issue_count",
	}

	return func(b sq.SelectBuilder) sq.SelectBuilder {
		var orderClauses []string
		for _, s := range sorts {
			col, ok := apiFieldToDBColumn[s.Field]
			if !ok {
				continue
			}
			if s.Desc {
				orderClauses = append(orderClauses, col+" DESC")
			} else {
				orderClauses = append(orderClauses, col+" ASC")
			}
		}
		orderClauses = append(orderClauses, "id")
		return b.OrderBy(orderClauses...)
	}
}

// GetFolders returns a list of distinct folders from the vinfo table.
func (s *VMStore) GetFolders(ctx context.Context) ([]models.Folder, error) {
	builder := sq.Select(
		`COALESCE("Folder ID", '') AS id`,
		`COALESCE("Folder", '') AS name`,
	).Distinct().
		From("vinfo").
		Where(`COALESCE("Folder ID", "Folder", '') != ''`).
		OrderBy("name")

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var folders []models.Folder
	for rows.Next() {
		var folder models.Folder
		if err := rows.Scan(&folder.ID, &folder.Name); err != nil {
			return nil, err
		}
		folders = append(folders, folder)
	}

	return folders, rows.Err()
}

// UpdateMigrationExcluded sets the migration_excluded flag for a VM in vinfo table.
func (s *VMStore) UpdateMigrationExcluded(ctx context.Context, vmID string, excluded bool) error {
	query, args, err := sq.Update("vinfo").
		Set(`"migration_excluded"`, excluded).
		Where(sq.Eq{`"VM ID"`: vmID}).
		ToSql()
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return srvErrors.NewResourceNotFoundError("VM", vmID)
	}
	return nil
}

// UpdateLabels sets the labels array for a VM in vinfo table.
func (s *VMStore) UpdateLabels(ctx context.Context, vmID string, labels []string) error {
	if labels == nil {
		labels = []string{}
	}

	// Build JSON array: ['label1', 'label2']
	labelsJSON := "[]"
	if len(labels) > 0 {
		escaped := make([]string, len(labels))
		for i, l := range labels {
			escaped[i] = fmt.Sprintf("'%s'", strings.ReplaceAll(l, "'", "''"))
		}
		labelsJSON = "[" + strings.Join(escaped, ",") + "]"
	}

	query, args, err := sq.Update("vinfo").
		Set(`"labels"`, sq.Expr(labelsJSON)).
		Where(sq.Eq{`"VM ID"`: vmID}).
		ToSql()
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return srvErrors.NewResourceNotFoundError("VM", vmID)
	}

	return nil
}

// GetAllLabels returns all distinct labels in use across VMs.
func (s *VMStore) GetAllLabels(ctx context.Context) ([]string, error) {
	// Build subquery for unnesting labels
	subquery := sq.Select(`unnest(CAST(v."labels" AS VARCHAR[])) AS label`).
		From("vinfo v").
		Where(sq.NotEq{`v."labels"`: "[]"})

	// Build main query
	query, args, err := sq.Select("DISTINCT label").
		FromSelect(subquery, "labels_unnested").
		Where(sq.NotEq{"label": ""}).
		OrderBy("label ASC").
		ToSql()
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var labels []string
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}

	return labels, rows.Err()
}

// AddLabel adds a label to a VM's labels array (idempotent - no duplicates).
func (s *VMStore) AddLabel(ctx context.Context, vmID string, label string) error {
	// Use DuckDB's list functions to add label without creating duplicates
	// list_append adds the element, list_distinct removes duplicates
	query, args, err := sq.Update("vinfo").
		Set(`"labels"`, sq.Expr("list_distinct(list_append(CAST(\"labels\" AS VARCHAR[]), ?))", label)).
		Where(sq.Eq{`"VM ID"`: vmID}).
		ToSql()
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return srvErrors.NewResourceNotFoundError("VM", vmID)
	}

	return nil
}

// RemoveLabel removes a label from a VM's labels array (idempotent).
func (s *VMStore) RemoveLabel(ctx context.Context, vmID string, label string) error {
	// Use DuckDB's list_filter with lambda to remove the specific label
	// If label doesn't exist, this is a no-op (idempotent)
	query, args, err := sq.Update("vinfo").
		Set(`"labels"`, sq.Expr("list_filter(CAST(\"labels\" AS VARCHAR[]), x -> x != ?)", label)).
		Where(sq.Eq{`"VM ID"`: vmID}).
		ToSql()
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return srvErrors.NewResourceNotFoundError("VM", vmID)
	}

	return nil
}

// RemoveLabelGlobally removes a label from all VMs that have it.
func (s *VMStore) RemoveLabelGlobally(ctx context.Context, label string) (int, error) {
	// Update all VMs that have the label
	// WHERE clause with list_contains optimizes to only update relevant VMs
	query, args, err := sq.Update("vinfo").
		Set(`"labels"`, sq.Expr("list_filter(CAST(\"labels\" AS VARCHAR[]), x -> x != ?)", label)).
		Where(sq.Expr("list_contains(CAST(\"labels\" AS VARCHAR[]), ?)", label)).
		ToSql()
	if err != nil {
		return 0, err
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return int(rows), nil
}

// validateVMsExist checks if all provided VM IDs exist in the database.
// Returns the list of missing VM IDs. If all exist, returns an empty slice.
func (s *VMStore) validateVMsExist(ctx context.Context, vmIDs []string) ([]string, error) {
	if len(vmIDs) == 0 {
		return nil, nil
	}

	// Use unnest in a FROM clause to create a derived table
	query := `
		SELECT t.id
		FROM (SELECT unnest(?) AS id) AS t
		WHERE t.id NOT IN (SELECT "VM ID" FROM vinfo)
	`

	rows, err := s.db.QueryContext(ctx, query, vmIDs)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var missing []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		missing = append(missing, id)
	}

	return missing, rows.Err()
}

// AddLabelBatch adds a label to multiple VMs in a single UPDATE statement.
// Validates all VMs exist only on failure (lazy validation for performance).
// Returns an error if any VM is not found.
func (s *VMStore) AddLabelBatch(ctx context.Context, vmIDs []string, label string) error {
	if len(vmIDs) == 0 {
		return nil
	}

	query, args, err := sq.Update("vinfo").
		Set(`"labels"`, sq.Expr("list_distinct(list_append(CAST(\"labels\" AS VARCHAR[]), ?))", label)).
		Where(sq.Expr(`"VM ID" IN (SELECT unnest(?))`, vmIDs)).
		ToSql()
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// Verify all VMs were updated
	if int(rowsAffected) != len(vmIDs) {
		// Only query for missing VMs when we detect a mismatch
		missing, err := s.validateVMsExist(ctx, vmIDs)
		if err != nil {
			return err
		}
		if len(missing) > 0 {
			return srvErrors.NewResourceNotFoundError("VM", missing[0])
		}
		// Edge case: rows affected doesn't match but no VMs are missing
		// This could happen due to concurrent deletion or other race conditions
		return fmt.Errorf("expected to update %d VMs but only updated %d", len(vmIDs), rowsAffected)
	}

	return nil
}

// RemoveLabelBatch removes a label from multiple VMs in a single UPDATE statement.
// Validates all VMs exist only on failure (lazy validation for performance).
// Returns an error if any VM is not found.
func (s *VMStore) RemoveLabelBatch(ctx context.Context, vmIDs []string, label string) error {
	if len(vmIDs) == 0 {
		return nil
	}

	query, args, err := sq.Update("vinfo").
		Set(`"labels"`, sq.Expr("list_filter(CAST(\"labels\" AS VARCHAR[]), x -> x != ?)", label)).
		Where(sq.Expr(`"VM ID" IN (SELECT unnest(?))`, vmIDs)).
		ToSql()
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// Verify all VMs were updated
	if int(rowsAffected) != len(vmIDs) {
		// Only query for missing VMs when we detect a mismatch
		missing, err := s.validateVMsExist(ctx, vmIDs)
		if err != nil {
			return err
		}
		if len(missing) > 0 {
			return srvErrors.NewResourceNotFoundError("VM", missing[0])
		}
		// Edge case: rows affected doesn't match but no VMs are missing
		// This could happen due to concurrent deletion or other race conditions
		return fmt.Errorf("expected to update %d VMs but only updated %d", len(vmIDs), rowsAffected)
	}

	return nil
}
