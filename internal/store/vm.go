package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sq "github.com/Masterminds/squirrel"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	parsermodels "github.com/kubev2v/migration-planner/pkg/duckdb_parser/models"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/filter"
)

// Valid issue categories (lowercase for case-insensitive comparison)
var validCategories = map[string]string{
	"critical":    "Critical",
	"warning":     "Warning",
	"information": "Information",
	"advisory":    "Advisory",
	"error":       "Error",
}

// normalizeCategory validates and normalizes an issue category (case-insensitive).
// If the category is not in the list of valid categories, it logs a warning
// and returns "Other".
func normalizeCategory(category, issueID string) string {
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

type VMStore struct {
	db     QueryInterceptor
	parser *duckdb_parser.Parser
}

func NewVMStore(db QueryInterceptor, parser *duckdb_parser.Parser) *VMStore {
	return &VMStore{db: db, parser: parser}
}

// FilterOption is a SQL WHERE condition for filtering VMs in the flat filter subquery.
type FilterOption = sq.Sqlizer

// List returns VM summaries with filters, sorting, and pagination.
// Filters are applied via a flat subquery that joins all tables, allowing
// WHERE clauses to reference any column. The output query aggregates results into one row per VM.
func (s *VMStore) List(ctx context.Context, filters []sq.Sqlizer, opts ...ListOption) ([]models.VirtualMachineSummary, error) {
	builder := vmOutputQuery()

	if len(filters) > 0 {
		subquery := vmFilterSubquery(filters)
		subSQL, subArgs, err := subquery.ToSql()
		if err != nil {
			return nil, err
		}
		builder = builder.Where(sq.Expr(fmt.Sprintf(`v."VM ID" IN (%s)`, subSQL), subArgs...))
	}

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
		err := rows.Scan(
			&vm.ID,
			&vm.Name,
			&vm.PowerState,
			&vm.Cluster,
			&vm.Datacenter,
			&vm.Memory,
			&vm.DiskSize,
			&vm.IssueCount,
			&vm.Status.State,
			&vm.IsTemplate,
			&vm.IsMigratable,
			&sqlErr,
		)
		if err != nil {
			return nil, err
		}
		vm.Status.Error = errors.New(sqlErr)
		vms = append(vms, vm)
	}

	return vms, rows.Err()
}

// Count returns the total number of VMs matching the filters.
func (s *VMStore) Count(ctx context.Context, filters ...sq.Sqlizer) (int, error) {
	builder := sq.Select("COUNT(*)").From("vinfo v")

	if len(filters) > 0 {
		subquery := vmFilterSubquery(filters)
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

// Get returns full VM details by ID using the parser.
func (s *VMStore) Get(ctx context.Context, id string) (*models.VM, error) {
	vms, err := s.parser.VMs(ctx, duckdb_parser.Filters{VmId: id}, duckdb_parser.Options{})
	if err != nil {
		return nil, err
	}

	if len(vms) == 0 {
		return nil, srvErrors.NewResourceNotFoundError("vm", id)
	}

	result := vmFromParser(vms[0])

	return &result, nil
}

func vmFromParser(pvm parsermodels.VM) models.VM {
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
		FaultToleranceEnabled: pvm.FaultToleranceEnabled,
		Disks:                 disks,
		NICs:                  nics,
		Issues:                issues,
	}
}

// ListOption modifies a SELECT query for sorting/pagination.
type ListOption func(sq.SelectBuilder) sq.SelectBuilder

// SortParam represents a single sort parameter with field name and direction.
type SortParam struct {
	Field string
	Desc  bool
}

// vmOutputQuery builds the aggregated output query that produces one row per VM.
func vmOutputQuery() sq.SelectBuilder {
	return sq.Select(
		`v."VM ID" AS id`,
		`v."VM" AS name`,
		`v."Powerstate" AS power_state`,
		`COALESCE(v."Cluster", '') AS cluster`,
		`COALESCE(v."Datacenter", '') AS datacenter`,
		`v."Memory" AS memory`,
		`COALESCE(d.total_disk, 0) AS disk_size`,
		`COALESCE(c.issue_count, 0) AS issue_count`,
		`COALESCE(i.status, 'not_found') AS status`,
		`v."Template" as template`,
		`COALESCE(crit.critical_count, 0) = 0 AS migratable`,
		`COALESCE(i.error, '') AS error`,
	).From("vinfo v").
		LeftJoin(`(SELECT "VM_ID", COUNT(*) AS issue_count FROM concerns GROUP BY "VM_ID") c ON v."VM ID" = c."VM_ID"`).
		LeftJoin(`(SELECT "VM_ID", COUNT(*) AS critical_count FROM concerns WHERE "Category" = 'Critical' GROUP BY "VM_ID") crit ON v."VM ID" = crit."VM_ID"`).
		LeftJoin(`(SELECT "VM ID", SUM("Capacity MiB") AS total_disk FROM vdisk GROUP BY "VM ID") d ON v."VM ID" = d."VM ID"`).
		LeftJoin(`vm_inspection_status i ON v."VM ID" = i."VM ID"`)
}

// vmFilterSubquery builds a flat JOIN of all tables so WHERE clauses can
// reference any raw column. Returns DISTINCT VM IDs matching the filters.
func vmFilterSubquery(filters []sq.Sqlizer) sq.SelectBuilder {
	b := sq.Select(`DISTINCT v."VM ID"`).
		From("vinfo v").
		LeftJoin(`vdisk dk ON v."VM ID" = dk."VM ID"`).
		LeftJoin(`concerns c ON v."VM ID" = c."VM_ID"`).
		LeftJoin(`vm_inspection_status i ON v."VM ID" = i."VM ID"`).
		LeftJoin(`vcpu cpu ON v."VM ID" = cpu."VM ID"`).
		LeftJoin(`vmemory mem ON v."VM ID" = mem."VM ID"`).
		LeftJoin(`vnetwork net ON v."VM ID" = net."VM ID"`).
		LeftJoin(`vdatastore ds ON ds."Name" = regexp_extract(COALESCE(dk."Path", dk."Disk Path"), '\[([^\]]+)\]', 1)`)

	for _, f := range filters {
		b = b.Where(f)
	}

	return b
}

// ByClusters filters by cluster names (OR logic).
func ByClusters(clusters ...string) sq.Sqlizer {
	if len(clusters) == 0 {
		return nil
	}
	return sq.Eq{`v."Cluster"`: clusters}
}

// ByStatus filters by power state (OR logic).
func ByStatus(statuses ...string) sq.Sqlizer {
	if len(statuses) == 0 {
		return nil
	}
	return sq.Eq{`v."Powerstate"`: statuses}
}

// ByIssues filters VMs with at least minIssues concerns.
func ByIssues(minIssues int) sq.Sqlizer {
	if minIssues <= 0 {
		return nil
	}
	return sq.Expr(
		`v."VM ID" IN (SELECT "VM_ID" FROM concerns GROUP BY "VM_ID" HAVING COUNT(*) >= ?)`,
		minIssues,
	)
}

// ByDiskSizeRange filters by total disk size in MiB [min, max).
func ByDiskSizeRange(min, max int64) sq.Sqlizer {
	return sq.Expr(
		`v."VM ID" IN (SELECT "VM ID" FROM vdisk GROUP BY "VM ID" HAVING SUM("Capacity MiB") >= ? AND SUM("Capacity MiB") < ?)`,
		min, max,
	)
}

// ByMemorySizeRange filters by memory in MiB [min, max].
func ByMemorySizeRange(min, max int64) sq.Sqlizer {
	return sq.And{
		sq.GtOrEq{`v."Memory"`: min},
		sq.LtOrEq{`v."Memory"`: max},
	}
}

// ByFilter applies a raw filter DSL expression.
// Returns nil if the expression is empty or fails to parse.
func ByFilter(expr string) sq.Sqlizer {
	if expr == "" {
		return nil
	}
	sqlizer, _ := filter.ParseWithDefaultMap([]byte(expr))
	return sqlizer
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

// WithSort applies multi-field sorting using output aliases from the
// aggregated query.
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
