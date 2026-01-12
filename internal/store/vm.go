package store

import (
	"context"
	"database/sql"

	sq "github.com/Masterminds/squirrel"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

type VMStore struct {
	db *sql.DB
}

func NewVMStore(db *sql.DB) *VMStore {
	return &VMStore{db: db}
}

func (s *VMStore) List(ctx context.Context, opts ...ListOption) ([]models.VM, error) {
	builder := sq.Select(
		"vms.id", "vms.name", "vms.state", "vms.datacenter", "vms.cluster",
		"vms.disk_size", "vms.memory",
		"vms.inspection_state", "vms.inspection_error", "vms.inspection_result",
		"LIST(vms_issues.issue) as issues",
	).From("vms").
		LeftJoin("vms_issues ON vms.id = vms_issues.vm_id").
		GroupBy(
			"vms.id", "vms.name", "vms.state", "vms.datacenter", "vms.cluster",
			"vms.disk_size", "vms.memory",
			"vms.inspection_state", "vms.inspection_error", "vms.inspection_result",
		).
		OrderBy("vms.id")

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

	var vms []models.VM
	for rows.Next() {
		var vm models.VM
		var issues any
		err := rows.Scan(
			&vm.ID,
			&vm.Name,
			&vm.State,
			&vm.Datacenter,
			&vm.Cluster,
			&vm.DiskSize,
			&vm.Memory,
			&vm.InspectionState,
			&vm.InspectionError,
			&vm.InspectionResults,
			&issues,
		)
		if err != nil {
			return nil, err
		}
		vm.Issues = toStringSlice(issues)
		vms = append(vms, vm)
	}

	return vms, rows.Err()
}

func (s *VMStore) Count(ctx context.Context, opts ...ListOption) (int, error) {
	builder := sq.Select("COUNT(*)").From("vms")

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

func (s *VMStore) Insert(ctx context.Context, vms ...models.VM) error {
	if len(vms) == 0 {
		return nil
	}

	vmBuilder := sq.Insert("vms").Columns(
		"id", "name", "state", "datacenter", "cluster",
		"disk_size", "memory",
		"inspection_state", "inspection_error", "inspection_result",
	)

	var issueBuilder sq.InsertBuilder
	hasIssues := false

	for _, vm := range vms {
		vmBuilder = vmBuilder.Values(
			vm.ID,
			vm.Name,
			vm.State,
			vm.Datacenter,
			vm.Cluster,
			vm.DiskSize,
			vm.Memory,
			vm.InspectionState,
			vm.InspectionError,
			vm.InspectionResults,
		)

		for _, issue := range vm.Issues {
			if !hasIssues {
				issueBuilder = sq.Insert("vms_issues").Columns("vm_id", "issue")
				hasIssues = true
			}
			issueBuilder = issueBuilder.Values(vm.ID, issue)
		}
	}

	query, args, err := vmBuilder.ToSql()
	if err != nil {
		return err
	}

	if _, err = s.db.ExecContext(ctx, query, args...); err != nil {
		return err
	}

	if hasIssues {
		query, args, err = issueBuilder.ToSql()
		if err != nil {
			return err
		}
		if _, err = s.db.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}

	return nil
}

func (s *VMStore) Update(ctx context.Context, vm models.VM) error {
	builder := sq.Update("vms").
		Set("name", vm.Name).
		Set("state", vm.State).
		Set("datacenter", vm.Datacenter).
		Set("cluster", vm.Cluster).
		Set("disk_size", vm.DiskSize).
		Set("memory", vm.Memory).
		Set("inspection_state", vm.InspectionState).
		Set("inspection_error", vm.InspectionError).
		Set("inspection_result", vm.InspectionResults).
		Where(sq.Eq{"id": vm.ID})

	query, args, err := builder.ToSql()
	if err != nil {
		return err
	}

	if _, err = s.db.ExecContext(ctx, query, args...); err != nil {
		return err
	}

	// Delete existing issues
	delQuery, delArgs, err := sq.Delete("vms_issues").Where(sq.Eq{"vm_id": vm.ID}).ToSql()
	if err != nil {
		return err
	}
	if _, err = s.db.ExecContext(ctx, delQuery, delArgs...); err != nil {
		return err
	}

	// Insert new issues
	if len(vm.Issues) > 0 {
		issueBuilder := sq.Insert("vms_issues").Columns("vm_id", "issue")
		for _, issue := range vm.Issues {
			issueBuilder = issueBuilder.Values(vm.ID, issue)
		}

		query, args, err = issueBuilder.ToSql()
		if err != nil {
			return err
		}
		if _, err = s.db.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}

	return nil
}

type ListOption func(sq.SelectBuilder) sq.SelectBuilder

func ByDatacenters(datacenters ...string) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		if len(datacenters) == 0 {
			return b
		}
		return b.Where(sq.Eq{"datacenter": datacenters})
	}
}

func ByClusters(clusters ...string) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		if len(clusters) == 0 {
			return b
		}
		return b.Where(sq.Eq{"cluster": clusters})
	}
}

func ByStatus(statuses ...string) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		if len(statuses) == 0 {
			return b
		}
		return b.Where(sq.Eq{"state": statuses})
	}
}

func ByIssues(issues ...string) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		if len(issues) == 0 {
			return b
		}
		return b.Where(sq.Expr(
			"id IN (SELECT vm_id FROM vms_issues WHERE issue IN (?))",
			issues,
		))
	}
}

func ByDiskSizeRange(min, max int64) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Where(sq.And{
			sq.GtOrEq{"disk_size": min},
			sq.Lt{"disk_size": max},
		})
	}
}

func ByMemorySizeRange(min, max int64) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Where(sq.And{
			sq.GtOrEq{"memory": min},
			sq.Lt{"memory": max},
		})
	}
}

func WithLimit(limit uint64) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Limit(limit)
	}
}

func WithOffset(offset uint64) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Offset(offset)
	}
}

func toStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	slice, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if item == nil {
			continue
		}
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
