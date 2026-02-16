package store

import (
	"context"
	"database/sql"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

type Store struct {
	db            *sql.DB
	parser        *duckdb_parser.Parser
	configuration *ConfigurationStore
	inventory     *InventoryStore
	vm            *VMStore
	inspection    *InspectionStore
}

func NewStore(db *sql.DB, validator duckdb_parser.Validator) *Store {
	qi := newQueryInterceptor(db)
	parser := duckdb_parser.New(db, validator)
	return &Store{
		db:            db,
		parser:        parser,
		configuration: NewConfigurationStore(qi),
		inventory:     NewInventoryStore(qi),
		vm:            NewVMStore(qi, parser),
		inspection:    NewInspectionStore(qi),
	}
}

func (s *Store) Migrate(ctx context.Context) error {
	if err := s.parser.Init(); err != nil {
		return err
	}

	if err := migrations.Run(ctx, s.db); err != nil {
		return err
	}

	return nil
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

func (s *Store) Inspection() *InspectionStore {
	return s.inspection
}

// Checkpoint forces a WAL flush to the main database file.
func (s *Store) Checkpoint() error {
	_, err := s.db.Exec("FORCE CHECKPOINT")
	return err
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
