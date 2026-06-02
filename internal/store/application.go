package store

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"

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
func (s *ApplicationStore) ListOverviews(ctx context.Context) ([]models.ApplicationOverview, error) {
	query, args, err := sq.Select(appColAppName, appColAppDesc, appColVMID, appColVMName).
		From(appTable).
		OrderBy(appColAppName, appColVMName).
		ToSql()
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
