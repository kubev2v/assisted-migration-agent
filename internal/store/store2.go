package store

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"time"

	pkgstore "github.com/kubev2v/migration-planner/pkg/store"
)

const (
	dbNameKey string = "db_name"
)

type Store2 struct {
	qi         QueryInterceptor
	transactor pkgstore.Transactor
	lastAccess *atomic.Int64
}

func newStore2(name string, qi QueryInterceptor, transactor pkgstore.Transactor) *Store2 {
	lastAccess := &atomic.Int64{}
	return &Store2{
		qi:         &usageInterceptor{dbName: name, inner: qi, last: lastAccess},
		lastAccess: lastAccess,
		transactor: transactor,
	}
}

func (s *Store2) LastAccess() int64 {
	return s.lastAccess.Load()
}

func (s *Store2) Querier() QueryInterceptor {
	return s.qi
}

// AttachDatabase attaches a database to the current connection with the given access mode.
// DuckDB does not allow attaching a file that is already open by another connection,
// so the caller must close the target database before attaching it.
// See test AttachDatabase in pool_test.go.
func (s *Store2) AttachDatabase(ctx context.Context, db *Database, name string, mode DatabaseAccessMode) error {
	accessMode := "READ_ONLY"
	if mode == ReadWriteDatabase {
		accessMode = "READ_WRITE"
	}
	if _, err := s.qi.ExecContext(ctx, fmt.Sprintf("ATTACH '%s' AS %s (%s)", db.Path, name, accessMode)); err != nil {
		return fmt.Errorf("attaching database %s: %w", name, err)
	}
	return nil
}

// DetachDatabase detach a database from the connection.
// It does not check whatever the db is attached.
func (s *Store2) DetachDatabase(ctx context.Context, name string) error {
	if _, err := s.qi.ExecContext(ctx, fmt.Sprintf("DETACH %s", name)); err != nil {
		return fmt.Errorf("detaching database %s: %w", name, err)
	}
	return nil
}

func (s *Store2) VerifyConnection(ctx context.Context) error {
	var result int
	return s.qi.QueryRowContext(ctx, "SELECT 1").Scan(&result)
}

// Checkpoint forces a WAL flush to the main database file.
func (s *Store2) Checkpoint(ctx context.Context) error {
	_, err := s.qi.ExecContext(ctx, "FORCE CHECKPOINT")
	return err
}

func (s *Store2) Configuration() *ConfigurationStore { return NewConfigurationStore(s.qi) }
func (s *Store2) Inventory() *InventoryStore         { return NewInventoryStore(s.qi) }
func (s *Store2) VM() *VMStore                       { return NewVMStore(s.qi) }
func (s *Store2) Inspection() *InspectionStore       { return NewInspectionStore(s.qi) }
func (s *Store2) Group() *GroupStore                 { return NewGroupStore(s.qi) }
func (s *Store2) Vddk() *VddkStore                   { return NewVddkStore(s.qi) }
func (s *Store2) Outbox() *OutboxStore               { return NewOutboxStore(s.qi) }
func (s *Store2) RightSizing() *RightSizingStore     { return NewRightSizingStore(s.qi) }
func (s *Store2) Forecast() *ForecastStore           { return NewForecastStore(s.qi) }
func (s *Store2) Application() *ApplicationStore     { return NewApplicationStore(s.qi) }
func (s *Store2) Credentials() *CredentialsStore     { return NewCredentialsStore(s.qi) }
func (s *Store2) Collection() *CollectionStore       { return NewCollectionStore(s.qi) }
func (s *Store2) Export() *ExportStore               { return NewExportStore(s.qi) }

func (s *Store2) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return s.transactor.WithTx(ctx, fn)
}

// usageInterceptor updates the lastAccess at every query for pool to know if it is time to close the unused connection.
// This is **best-effort** because QueryRowContext return a sql.Row that keep connection opened so it might happen
// that the last timestamp don't correspont to the timestamp when the last row.Scan query was made.
// But, the pool has 5min timeout which enough for row.Scan to finish scanning all the rows.
type usageInterceptor struct {
	dbName string
	inner  QueryInterceptor
	last   *atomic.Int64
}

func (u *usageInterceptor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	ctx = context.WithValue(ctx, dbNameKey, u.dbName) //nolint:staticcheck // string key is intentional — cross-library context value
	u.last.Store(time.Now().UnixNano())
	return u.inner.QueryRowContext(ctx, query, args...)
}

func (u *usageInterceptor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	ctx = context.WithValue(ctx, dbNameKey, u.dbName) //nolint:staticcheck // string key is intentional — cross-library context value
	u.last.Store(time.Now().UnixNano())
	return u.inner.QueryContext(ctx, query, args...)
}

func (u *usageInterceptor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	ctx = context.WithValue(ctx, dbNameKey, u.dbName) //nolint:staticcheck // string key is intentional — cross-library context value
	u.last.Store(time.Now().UnixNano())
	return u.inner.ExecContext(ctx, query, args...)
}
