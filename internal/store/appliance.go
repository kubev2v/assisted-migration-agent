package store

import (
	"context"
	"database/sql"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

type ApplianceStore struct {
	db QueryInterceptor
}

func NewApplianceStore(db QueryInterceptor) *ApplianceStore {
	return &ApplianceStore{db: db}
}

// List returns appliances grouped by product name, sorted alphabetically.
func (s *ApplianceStore) List(ctx context.Context, filter sq.Sqlizer) ([]models.Appliance, error) {
	selectQuery := sq.Select(
		`p.name`,
		`p.vendor`,
		`p.version`,
		`p.full_version`,
		`p.vendor_url`,
		`p.product_url`,
		`p.app_url`,
		`p.vm_id`,
		`v."VM"`,
	).From("vm_vapp_products p").
		LeftJoin(`vinfo v ON v."VM ID" = p.vm_id`).
		Where(sq.And{sq.NotEq{"p.name": nil}, sq.NotEq{"p.name": ""}})

	if filter != nil {
		selectQuery = selectQuery.Where(filter)
	}

	selectQuery = selectQuery.OrderBy("p.name", "p.vm_id", "p.key")

	query, args, err := selectQuery.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []models.Appliance
	var current *models.Appliance

	for rows.Next() {
		var (
			vmID, name                                                          string
			vendor, version, fullVersion, vendorUrl, productUrl, appUrl, vmName sql.NullString
		)

		if err := rows.Scan(
			&name,
			&vendor,
			&version,
			&fullVersion,
			&vendorUrl,
			&productUrl,
			&appUrl,
			&vmID,
			&vmName,
		); err != nil {
			return nil, err
		}

		if current == nil || current.Name != name {
			if current != nil {
				results = append(results, *current)
			}

			current = &models.Appliance{
				Name:            name,
				Vendor:          "Unknown",
				Confidence:      100,
				VirtualMachines: models.VirtualMachinesIdentites{},
			}

			if vendor.Valid && vendor.String != "" {
				current.Vendor = vendor.String
			}
			if version.Valid {
				current.Version = version.String
			}
			if fullVersion.Valid {
				current.FullVersion = fullVersion.String
			}
			if vendorUrl.Valid {
				current.VendorUrl = vendorUrl.String
			}
			if productUrl.Valid {
				current.ProductUrl = productUrl.String
			}
			if appUrl.Valid {
				current.ApplicationUrl = appUrl.String
			}
		}

		n := ""
		if vmName.Valid {
			n = vmName.String
		}
		current.VirtualMachines[vmID] = n
	}

	if current != nil {
		results = append(results, *current)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}
