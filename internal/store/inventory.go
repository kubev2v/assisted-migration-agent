package store

import (
	"context"
	"database/sql"
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
		Where(sq.Eq{"id": 1}).
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

func (s *InventoryStore) Save(ctx context.Context, data []byte) error {
	query, args, err := sq.Insert("inventory").
		Columns("id", "data", "updated_at").
		Values(1, data, sq.Expr("now()")).
		Suffix("ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, updated_at = now()").
		ToSql()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}
