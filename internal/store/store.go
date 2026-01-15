package store

import (
	"context"
	"database/sql"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
)

type Store struct {
	db            *sql.DB
	parser        *duckdb_parser.Parser
	configuration *ConfigurationStore
	inventory     *InventoryStore
	vm            *VMStore
	credentials   *CredentialsStore
}

func NewStore(db *sql.DB) *Store {
	qi := newQueryInterceptor(db)
	parser := duckdb_parser.New(db, nil)
	return &Store{
		db:            db,
		parser:        parser,
		configuration: NewConfigurationStore(qi),
		inventory:     NewInventoryStore(qi),
		vm:            NewVMStore(qi, parser),
		credentials:   NewCredentialsStore(qi),
	}
}

func (s *Store) Parser() *duckdb_parser.Parser {
	return s.parser
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

func (s *Store) Credentials() *CredentialsStore {
	return s.credentials
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

type QueryInterceptor interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
