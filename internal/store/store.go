package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	sq "github.com/Masterminds/squirrel"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	pkgstore "github.com/kubev2v/migration-planner/pkg/store"
	"go.uber.org/zap"

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
	credentials   *CredentialsStore
	collection    *CollectionStore
	export        *ExportStore
}

func NewStore(db *sql.DB, validator duckdb_parser.Validator) *Store {
	qi := pkgstore.NewQueryInterceptor(db)
	transactor := pkgstore.NewTransactor(db)
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
		transactor:    transactor,
		application:   NewApplicationStore(qi),
		credentials:   NewCredentialsStore(qi),
		collection:    NewCollectionStore(qi),
		export:        NewExportStore(qi),
	}
}

func (s *Store) InitCollection(ctx context.Context) error {
	if err := s.parser.Init(); err != nil {
		return fmt.Errorf("initializing parser tables: %w", err)
	}
	ns, err := s.GetCurrentDatabase(ctx)
	if err != nil {
		return fmt.Errorf("getting current database: %w", err)
	}
	if err := migrations.RunCollection(ctx, s.db, ns); err != nil {
		return fmt.Errorf("running collection migrations: %w", err)
	}
	return nil
}

func (s *Store) Migrate(ctx context.Context, dataDir string) error {
	if err := migrations.RunMain(ctx, s.db); err != nil {
		return err
	}
	if dataDir != "" {
		if err := s.LoadDatabases(ctx, dataDir); err != nil {
			return err
		}
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

func (s *Store) Credentials() *CredentialsStore {
	return s.credentials
}

func (s *Store) Collection() *CollectionStore {
	return s.collection
}

func (s *Store) Export() *ExportStore {
	return s.export
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

func (s *Store) SetCurrentDatabase(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("USE %s", name))
	return err
}

func (s *Store) GetCurrentDatabase(ctx context.Context) (string, error) {
	query, _, err := sq.Select("current_database()").ToSql()
	if err != nil {
		return "", fmt.Errorf("building current database query: %w", err)
	}

	var name string
	if err := s.db.QueryRowContext(ctx, query).Scan(&name); err != nil {
		return "", fmt.Errorf("reading current database: %w", err)
	}

	return name, nil
}

func (s *Store) AttachDatabase(ctx context.Context, dataDir, name string) error {
	dbPath := filepath.Join(dataDir, fmt.Sprintf("%s.duckdb", name))
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ATTACH '%s' AS %s", dbPath, name)); err != nil {
		return fmt.Errorf("attaching database %s: %w", name, err)
	}
	return s.SetCurrentDatabase(ctx, name)
}

func (s *Store) CreateDatabase(ctx context.Context, dataDir, name string) error {
	dbPath := filepath.Join(dataDir, fmt.Sprintf("%s.duckdb", name))
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ATTACH '%s' AS %s", dbPath, name)); err != nil {
		return fmt.Errorf("attaching database %s: %w", name, err)
	}

	prev, err := s.GetCurrentDatabase(ctx)
	if err != nil {
		return err
	}

	if err := s.SetCurrentDatabase(ctx, name); err != nil {
		return fmt.Errorf("switching to %s: %w", name, err)
	}
	defer func() { _ = s.SetCurrentDatabase(ctx, prev) }()

	if err := s.parser.Init(); err != nil {
		return fmt.Errorf("initializing parser tables in %s: %w", name, err)
	}

	if err := migrations.RunCollection(ctx, s.db, name); err != nil {
		return fmt.Errorf("running collection migrations in %s: %w", name, err)
	}

	return nil
}

func (s *Store) LoadDatabases(ctx context.Context, dataDir string) error {
	matches, err := filepath.Glob(filepath.Join(dataDir, "collection_*.duckdb"))
	if err != nil {
		return fmt.Errorf("scanning for collection databases: %w", err)
	}

	var latest string
	var latestTS int64
	for _, match := range matches {
		name := strings.TrimSuffix(filepath.Base(match), ".duckdb")
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ATTACH '%s' AS %s", match, name)); err != nil {
			return fmt.Errorf("attaching collection database %s: %w", name, err)
		}
		zap.S().Infow("attached collection database", "name", name, "path", match)
		if ts, err := strconv.ParseInt(strings.TrimPrefix(name, "collection_"), 10, 64); err == nil && ts > latestTS {
			latestTS = ts
			latest = name
		}
	}

	if latest != "" {
		if err := s.SetCurrentDatabase(ctx, latest); err != nil {
			return fmt.Errorf("switching to latest collection %s: %w", latest, err)
		}
		zap.S().Infow("defaulted to latest collection database", "name", latest)
	}
	return nil
}
