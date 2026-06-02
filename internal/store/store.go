package store

import (
	"context"
	"database/sql"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	pkgstore "github.com/kubev2v/migration-planner/pkg/store"

	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

type Store struct {
	db            *sql.DB
	parser        *duckdb_parser.Parser
	configuration *ConfigurationStore
	inventory     *InventoryStore
	vm            *VMStore
	inspection    *InspectionStore
	group         *GroupStore
	vddk          *VddkStore
	outbox        *OutboxStore
	rightsizing   *RightSizingStore
	forecast      *ForecastStore
	transactor    pkgstore.Transactor
	application   *ApplicationStore
}

func NewStore(db *sql.DB, validator duckdb_parser.Validator) *Store {
	qi := pkgstore.NewQueryInterceptor(db)
	parser := duckdb_parser.New(qi, validator)
	return &Store{
		db:            db,
		parser:        parser,
		configuration: NewConfigurationStore(qi),
		inventory:     NewInventoryStore(qi),
		vm:            NewVMStore(qi),
		inspection:    NewInspectionStore(qi),
		group:         NewGroupStore(qi),
		vddk:          NewVddkStore(qi),
		outbox:        NewOutboxStore(qi),
		rightsizing:   NewRightSizingStore(qi),
		forecast:      NewForecastStore(qi),
		transactor:    pkgstore.NewTransactor(db),
		application:   NewApplicationStore(qi),
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

func (s *Store) Group() *GroupStore {
	return s.group
}

func (s *Store) Vddk() *VddkStore {
	return s.vddk
}

func (s *Store) Outbox() *OutboxStore {
	return s.outbox
}

func (s *Store) RightSizing() *RightSizingStore {
	return s.rightsizing
}

func (s *Store) Forecast() *ForecastStore {
	return s.forecast
}

func (s *Store) Application() *ApplicationStore {
	return s.application
}

func (s *Store) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return s.transactor.WithTx(ctx, fn)
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

// QueryInterceptor is an alias for the shared store.QueryInterceptor interface.
// Kept for backward compatibility with existing repository constructors.
type QueryInterceptor = pkgstore.QueryInterceptor
