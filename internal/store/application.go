package store

import (
	"context"
	"database/sql"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

const (
	appTable         = "vm_applications"
	appColAppName    = "app_name"
	appColAppDesc    = "app_desc"
	appColVendor     = "app_vendor"
	appColCategory   = "app_category"
	appColConfidence = "confidence"
	appColVMID       = "vm_id"
	appColVMName     = "vm_name"
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

	builder := sq.Insert(appTable).Columns(appColAppName, appColAppDesc, appColVendor, appColCategory, appColConfidence, appColVMID, appColVMName)
	for _, r := range records {
		builder = builder.Values(r.AppName, r.AppDesc, r.AppVendor, r.AppCategory, r.Confidence, r.VMID, r.VMName)
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
func (s *ApplicationStore) ListOverviews(ctx context.Context, filter sq.Sqlizer) ([]models.ApplicationOverview, error) {
	selectQuery := sq.Select(appColAppName, appColAppDesc, appColVendor, appColCategory, appColConfidence, appColVMID, appColVMName).From(appTable)

	if filter != nil {
		selectQuery = selectQuery.Where(filter)
	}

	selectQuery = selectQuery.OrderBy(appColAppName, appColVMName)

	query, args, err := selectQuery.ToSql()
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
		var appVendor, appCategory sql.NullString
		var confidence sql.NullInt64
		if err := rows.Scan(&appName, &appDesc, &appVendor, &appCategory, &confidence, &vmID, &vmName); err != nil {
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
				Vendor:      "Unknown",
				Category:    "Unclassified",
				VMs:         []models.ApplicationVM{{ID: vmID, Name: vmName}},
			}
			if appVendor.Valid && appVendor.String != "" {
				current.Vendor = appVendor.String
			}
			if appCategory.Valid && appCategory.String != "" {
				current.Category = appCategory.String
			}
			value := 50
			if confidence.Valid {
				value = int(confidence.Int64)
			}
			current.Confidence = &value
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
