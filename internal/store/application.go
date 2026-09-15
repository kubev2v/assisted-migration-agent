package store

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	vmfilter "github.com/kubev2v/assisted-migration-agent/internal/filter"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

const (
	appTable      = "vm_applications"
	appColAppName = "app_name"
	appColAppDesc = "app_desc"
	appColVMID    = "vm_id"
	appColVMName  = "vm_name"
)

type ApplicationStore struct {
	db QueryInterceptor
}

func NewApplicationStore(db QueryInterceptor) *ApplicationStore {
	return &ApplicationStore{db: db}
}

// ReplaceAll deletes all existing rows and inserts the given records.
func (s *ApplicationStore) ReplaceAll(ctx context.Context, records []models.ApplicationVMRecord) error {
	query, args, err := sq.Delete(appTable).ToSql()
	if err != nil {
		return fmt.Errorf("building delete query: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("clearing %s: %w", appTable, err)
	}

	if len(records) == 0 {
		return nil
	}

	builder := sq.Insert(appTable).Columns(appColAppName, appColAppDesc, appColVMID, appColVMName)
	for _, r := range records {
		builder = builder.Values(r.AppName, r.AppDesc, r.VMID, r.VMName)
	}

	query, args, err = builder.ToSql()
	if err != nil {
		return fmt.Errorf("building insert query: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("inserting into %s: %w", appTable, err)
	}

	return nil
}

// ListOverviews returns application overviews grouped by app name, sorted alphabetically.
// If filterExpr is provided, only applications from VMs matching the filter are returned.
func (s *ApplicationStore) ListOverviews(ctx context.Context, filterExpr string) ([]models.ApplicationOverview, error) {
	builder := sq.Select(appColAppName, appColAppDesc, appColVMID, appColVMName).
		From(appTable+" va").
		OrderBy(appColAppName, appColVMName)

	// If filter expression provided, apply VM filtering
	if filterExpr != "" {
		// Parse the filter expression to get sqlizer
		sqlizer, err := vmfilter.ParseWithDefaultMap([]byte(filterExpr))
		if err != nil {
			return nil, fmt.Errorf("parsing filter expression: %w", err)
		}

		// Join vinfo and other tables needed for filtering
		// Match the pattern from internal/store/vm_queries.go buildListQuery
		builder = builder.
			Join("vinfo v ON va.vm_id = v.\"VM ID\"").
			// Add LEFT JOIN for groups (for "groups contains" filters)
			LeftJoin(`(
				SELECT u.vm_id, ARRAY_AGG(DISTINCT grp.name) AS groups
				FROM group_matches gm
				JOIN groups grp ON gm.group_id = grp.id
				, UNNEST(gm.vm_ids) AS u(vm_id)
				GROUP BY u.vm_id
			) g ON v."VM ID" = g.vm_id`).
			// Add LEFT JOIN for concerns (for "concern.*" filters)
			LeftJoin(`concerns c ON c."VM_ID" = v."VM ID"`).
			// Add LEFT JOIN for critical concerns count (for "migratable" filter)
			LeftJoin(`(
				SELECT "VM_ID", COUNT(*) as critical_count
				FROM concerns
				WHERE "Category" IN ('Critical', 'Error')
				GROUP BY "VM_ID"
			) crit ON v."VM ID" = crit."VM_ID"`).
			// Add LEFT JOIN for issues count
			LeftJoin(`(
				SELECT "VM_ID", COUNT(*) as issues_count
				FROM concerns
				GROUP BY "VM_ID"
			) cc ON v."VM ID" = cc."VM_ID"`).
			// Add LEFT JOIN for disk aggregation (for "total_disk_capacity" filter)
			LeftJoin(`(
				SELECT "VM ID", SUM(COALESCE("Capacity MiB", 0)) as total_disk
				FROM vdisk
				GROUP BY "VM ID"
			) d ON v."VM ID" = d."VM ID"`).
			// Apply the filter
			Where(sqlizer)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []models.ApplicationOverview
	var current *models.ApplicationOverview

	for rows.Next() {
		var appName, appDesc, vmID, vmName string
		if err := rows.Scan(&appName, &appDesc, &vmID, &vmName); err != nil {
			return nil, err
		}

		if current == nil || current.Name != appName {
			if current != nil {
				current.VMCount = len(current.VMs)
				results = append(results, *current)
			}
			current = &models.ApplicationOverview{
				Name:        appName,
				Description: appDesc,
				VMs:         []models.ApplicationVM{{ID: vmID, Name: vmName}},
			}
		} else {
			current.VMs = append(current.VMs, models.ApplicationVM{ID: vmID, Name: vmName})
		}
	}

	if current != nil {
		current.VMCount = len(current.VMs)
		results = append(results, *current)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}
