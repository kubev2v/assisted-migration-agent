package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// InventoryStore handles inventory storage using DuckDB.
type InventoryStore struct {
	db *sql.DB
}

// NewInventoryStore creates a new inventory store.
func NewInventoryStore(db *sql.DB) *InventoryStore {
	return &InventoryStore{db: db}
}

// Get retrieves the stored inventory.
func (s *InventoryStore) Get(ctx context.Context) (*models.Inventory, error) {
	row := s.db.QueryRowContext(ctx, queryGetInventory)

	var inv models.Inventory
	err := row.Scan(&inv.Data, &inv.CreatedAt, &inv.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, srvErrors.NewInventoryNotFoundError()
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// Save stores or updates the inventory.
func (s *InventoryStore) Save(ctx context.Context, data []byte) error {
	_, err := s.db.ExecContext(ctx, queryUpsertInventory, data)
	return err
}
