package store

import (
	"context"
	"errors"

	sq "github.com/Masterminds/squirrel"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	parsermodels "github.com/kubev2v/migration-planner/pkg/duckdb_parser/models"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type VMStore struct {
	db     QueryInterceptor
	parser *duckdb_parser.Parser
}

func NewVMStore(db QueryInterceptor, parser *duckdb_parser.Parser) *VMStore {
	return &VMStore{db: db, parser: parser}
}

// List returns VM summaries with filters, sorting, and pagination.
func (s *VMStore) List(ctx context.Context, opts ...ListOption) ([]models.VirtualMachineSummary, error) {
	builder := sq.Select(
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
	defer rows.Close()

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
func (s *VMStore) Count(ctx context.Context, opts ...ListOption) (int, error) {
	builder := sq.Select("COUNT(*)").
		From("vinfo v").
		LeftJoin(`(SELECT "VM_ID", COUNT(*) AS issue_count FROM concerns GROUP BY "VM_ID") c ON v."VM ID" = c."VM_ID"`).
		LeftJoin(`(SELECT "VM ID", SUM("Capacity MiB") AS total_disk FROM vdisk GROUP BY "VM ID") d ON v."VM ID" = d."VM ID"`)

	// Apply only WHERE filters, skip ORDER BY/LIMIT/OFFSET
	for _, opt := range opts {
		builder = opt(builder)
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
	issues := make([]string, 0, len(pvm.Concerns))
	criticalCount := 0
	for _, c := range pvm.Concerns {
		issues = append(issues, c.Label)
		if c.Category == "Critical" {
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

// ListOption modifies a SELECT query for filtering/sorting/pagination.
type ListOption func(sq.SelectBuilder) sq.SelectBuilder

// SortParam represents a single sort parameter with field name and direction.
type SortParam struct {
	Field string
	Desc  bool
}

// ByClusters filters by cluster names (OR logic).
func ByClusters(clusters ...string) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		if len(clusters) == 0 {
			return b
		}
		return b.Where(sq.Eq{`v."Cluster"`: clusters})
	}
}

// ByStatus filters by power state (OR logic).
func ByStatus(statuses ...string) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		if len(statuses) == 0 {
			return b
		}
		return b.Where(sq.Eq{`v."Powerstate"`: statuses})
	}
}

// ByIssues filters VMs with issue_count >= minIssues.
func ByIssues(minIssues int) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		if minIssues <= 0 {
			return b
		}
		return b.Where(sq.GtOrEq{"COALESCE(c.issue_count, 0)": minIssues})
	}
}

// ByDiskSizeRange filters by disk size in MB [min, max).
func ByDiskSizeRange(min, max int64) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Where(sq.And{
			sq.GtOrEq{`COALESCE(d.total_disk, 0)`: min},
			sq.Lt{`COALESCE(d.total_disk, 0)`: max},
		})
	}
}

// ByMemorySizeRange filters by memory in MB [min, max).
func ByMemorySizeRange(min, max int64) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Where(sq.And{
			sq.GtOrEq{`v."Memory"`: min},
			sq.Lt{`v."Memory"`: max},
		})
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

// apiFieldToDBColumn maps API field names to database column expressions.
// WithDefaultSort applies default sorting by VM ID.
func WithDefaultSort() ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.OrderBy(`v."VM ID"`)
	}
}

// WithSort applies multi-field sorting.
func WithSort(sorts []SortParam) ListOption {
	apiFieldToDBColumn := map[string]string{
		"name":         `v."VM"`,
		"vCenterState": `v."Powerstate"`,
		"cluster":      `v."Cluster"`,
		"diskSize":     `COALESCE(d.total_disk, 0)`,
		"memory":       `v."Memory"`,
		"issues":       `issue_count`,
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
		// Always add VM ID as tie-breaker for stable sorting
		orderClauses = append(orderClauses, `v."VM ID"`)
		return b.OrderBy(orderClauses...)
	}
}
