package store

import (
	"context"
	"database/sql"
)

type Store struct {
	db            *sql.DB
	configuration *ConfigurationStore
	inventory     *InventoryStore
	vm            *VMStore
}

func NewStore(db *sql.DB) *Store {
	qi := newQueryInterceptor(db)
	return &Store{
		db:            db,
		configuration: NewConfigurationStore(qi),
		inventory:     NewInventoryStore(qi),
		vm:            NewVMStore(qi),
	}
}

func (s *Store) Configuration() *ConfigurationStore {
	return s.configuration
}

func (s *Store) Inventory() *InventoryStore {
	return s.inventory
}

func (s *Store) VM() *VMStore {
	return s.vm
}

func (s *Store) Close() error {
	return s.db.Close()
}

type QueryInterceptor interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
