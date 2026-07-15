package store

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	pkgstore "github.com/kubev2v/migration-planner/pkg/store"
)

type Store2 struct {
	qi         QueryInterceptor
	transactor pkgstore.Transactor
	parser     *duckdb_parser.Parser
	lastAccess atomic.Int64
}

func NewStore2(qi QueryInterceptor, transactor pkgstore.Transactor, validator duckdb_parser.Validator) *Store2 {
	s := &Store2{}
	tracked := &usageInterceptor{inner: qi, last: &s.lastAccess}
	s.qi = tracked
	s.transactor = transactor
	s.parser = duckdb_parser.New(tracked, validator)
	return s
}

func (s *Store2) LastAccess() int64 {
	return s.lastAccess.Load()
}

// AttachDatabase attaches a new database to the current connection with read-only access.
func (s *Store2) AttachDatabase(ctx context.Context, path, name string) error {
	if _, err := s.qi.ExecContext(ctx, fmt.Sprintf("ATTACH '%s' AS %s (READ_ONLY)", path, name)); err != nil {
		return fmt.Errorf("attaching database %s: %w", name, err)
	}
	return nil
}

// DetachDatabase detach a database from the connection.
// It does not check whatever the db is attached.
func (s *Store2) DetachDatabase(ctx context.Context, name string) error {
	if _, err := s.qi.ExecContext(ctx, fmt.Sprintf("DETACH '%s'", name)); err != nil {
		return fmt.Errorf("detaching database %s: %w", name, err)
	}
	return nil
}

func (s *Store2) VerifyConnection(ctx context.Context) error {
	var result int
	return s.qi.QueryRowContext(ctx, "SELECT 1").Scan(&result)
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
	inner QueryInterceptor
	last  *atomic.Int64
}

func (u *usageInterceptor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	u.last.Store(time.Now().UnixNano())
	return u.inner.QueryRowContext(ctx, query, args...)
}

func (u *usageInterceptor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	u.last.Store(time.Now().UnixNano())
	return u.inner.QueryContext(ctx, query, args...)
}

func (u *usageInterceptor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	u.last.Store(time.Now().UnixNano())
	return u.inner.ExecContext(ctx, query, args...)
}
