package store

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type InventoryStore struct {
	db QueryInterceptor
}

func NewInventoryStore(db QueryInterceptor) *InventoryStore {
	return &InventoryStore{db: db}
}

func (s *InventoryStore) Get(ctx context.Context) (*models.Inventory, error) {
	query, args, err := sq.Select("data", "created_at", "updated_at").
		From("inventory").
		Where(sq.Eq{"is_subset": false}).
		ToSql()
	if err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	var inv models.Inventory

	err = row.Scan(&inv.Data, &inv.CreatedAt, &inv.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, srvErrors.NewInventoryNotFoundError()
	}
	if err != nil {
		return nil, err
	}

	return &inv, nil
}

func (s *InventoryStore) GetByID(ctx context.Context, vcenterID string) (models.Inventory, error) {
	query, args, err := sq.Select("data", "created_at", "updated_at").
		From("inventory").
		Where(sq.Eq{"id": vcenterID}).
		ToSql()
	if err != nil {
		return models.Inventory{}, err
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	var inv models.Inventory

	err = row.Scan(&inv.Data, &inv.CreatedAt, &inv.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Inventory{}, srvErrors.NewInventoryNotFoundError()
	}
	if err != nil {
		return models.Inventory{}, err
	}

	return inv, nil
}

func (s *InspectionStore) GetVCenterIDs(ctx context.Context) ([]string, error) {
	query, args, err := sq.Select("id").
		From("inventory").
		Where(sq.Eq{"is_subset": true}).
		ToSql()
	if err != nil {
		return []string{}, err
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return []string{}, err
	}
	defer func() { _ = rows.Close() }()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return []string{}, err
		}

		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return []string{}, err
	}

	return ids, nil
}

func (s *InventoryStore) Save(ctx context.Context, data []byte) error {
	return s.save(ctx, data, false)
}

func (s *InventoryStore) SaveSubset(ctx context.Context, data []byte) error {
	return s.save(ctx, data, true)
}

func (s *InventoryStore) save(ctx context.Context, data []byte, isSubset bool) error {
	// Remark:
	// Inventory is passed as bytes to keep backwards compability with the services
	// but, the schema changed to allow storing inventories for multi vcenter, therefore
	// we need to unmashal it here to extract vcenter id.
	// Because I only need the vcenterid and I dont want to bind the api model,
	// the unmashal is done into anonymous struct.
	// 27.10.2026 Cosmin

	var inventory = struct {
		VCenterID string `json:"vcenter_id"`
	}{}

	if err := json.Unmarshal(data, &inventory); err != nil {
		return err
	}

	query, args, err := sq.Insert("inventory").
		Columns("id", "data", "updated_at", "is_subset").
		Values(inventory.VCenterID, data, sq.Expr("now()"), isSubset).
		Suffix("ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, updated_at = now()").
		ToSql()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}
