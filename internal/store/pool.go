package store

import (
	"context"
	"database/sql"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/kubev2v/assisted-migration-agent/pkg/errors"

	sq "github.com/Masterminds/squirrel"
	pkgstore "github.com/kubev2v/migration-planner/pkg/store"
	"go.uber.org/zap"
)

// Pool manages multiple isolated DuckDB database connections. Each database
// gets its own *sql.DB with independent memory limits and access modes.
//
// NewDatabase creates and optionally opens a Database but does NOT register
// it in the pool. The returned Database is private to the caller — it can
// be written to (e.g. during a collection run) without being visible to
// Get() or cleanup. The caller calls Add() to publish it once the database
// is ready for general use. This separation is important for collections:
// an in-progress collection database must not be exposed to readers until
// the collection is complete.
//
// Database.Store() lazily initializes the connection and returns a *Store.
// Calling Store() on a closed connection transparently reconnects.
//
// Idle eligibility is tracked by Store itself: every query updates a
// lastAccess timestamp via usageInterceptor (see store2.go). Pool.cleanup()
// compares that timestamp against the cleanup interval to decide whether
// to close the connection. This is best-effort — see the usageInterceptor
// comment for details.
//
// Cleanup runs lazily inside Get() when the pool-wide cleanup timer fires —
// no background goroutines are spawned.
//
// Usage:
//
//	pool := store.NewPool(5 * time.Minute)
//
//	// Register the main agent database (eager, always connected).
//	mainDB, _ := pool.NewDatabase(
//	    "/data/agent.duckdb",
//	    store.EagerConnectionInitilization,
//		512,
//		store.ReadWriteDatabase,
//	)
//	pool.Add(mainDB)
//
//	// Register collection databases from disk.
//	files, _ := filepath.Glob("/data/collection_*.duckdb")
//	sort.Strings(files) // oldest first
//	for i, f := range files {
//	    mode := store.ReadOnlyDatabase
//	    if i == len(files)-1 {
//	        mode = store.ReadWriteDatabase // latest collection
//	    }
//	    db, _ := pool.NewDatabase(f, validator, store.LazyConnectionInitilization, 256, mode)
//	    pool.Add(db)
//	}
//
//	// Get a *Store for any registered database.
//	db, _ := pool.Get(mainDB.ID)
//	st, _ := db.Store()
//	cfg, _ := st.Configuration().Get(ctx)
const MainDatabaseID = "main"

type ConnectionInitilizationType int

const (
	EagerConnectionInitilization ConnectionInitilizationType = iota
	LazyConnectionInitilization
)

type DatabaseAccessMode int

const (
	ReadOnlyDatabase DatabaseAccessMode = iota
	ReadWriteDatabase
)

// holds the constraint at db level
// used in append db to look for constraint before copy paste values
type constraint struct {
	// Kind is either PK, UNIQUE or FK.
	// These are the only constraints that are important in copy paste op
	Kind string
	// If Kind is FK ReferencedTable holds the name of the referenced table
	ReferencedTable string
}

// key is the table
type constraintsType map[string][]constraint

func (c constraintsType) getReferencedTables(tableName string) []string {
	refTables := []string{}
	for table, constraints := range c {
		if table != tableName {
			continue
		}
		for _, constraint := range constraints {
			if constraint.Kind == "FOREIGN KEY" {
				refTables = append(refTables, constraint.ReferencedTable)
			}
		}
	}
	return refTables
}

func (c constraintsType) HasConstraint(tableName string, constraintName string) bool {
	constraints, found := c[tableName]
	if !found {
		return false
	}
	for _, constraint := range constraints {
		if strings.EqualFold(constraint.Kind, constraintName) {
			return true
		}
	}
	return false
}

type Database struct {
	ID          string
	Path        string
	CreatedAt   time.Time
	mu          sync.Mutex
	store       *Store
	accessMode  DatabaseAccessMode
	connection  *sql.DB
	memoryLimit int
}

func (d *Database) Store() (*Store, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.store != nil {
		return d.store, nil
	}

	if d.connection == nil {
		conn, err := newDatabase(NewDefaultExtentionLoader(), d.Path, d.memoryLimit, d.accessMode)
		if err != nil {
			return nil, err
		}
		d.connection = conn
	}

	d.store = newStore(filepath.Base(d.Path), pkgstore.NewQueryInterceptor(d.connection), pkgstore.NewTransactor(d.connection))

	return d.store, nil
}

func (d *Database) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.store = nil

	if d.connection == nil {
		return nil
	}

	err := d.connection.Close()
	d.connection = nil

	return err
}

func (d *Database) LastAccess() int64 {
	d.mu.Lock()
	s := d.store
	d.mu.Unlock()

	if s == nil {
		return 0
	}

	return s.LastAccess()
}

// Append copies all source tables into this database. The source must be closed.
// This method tries to be agnostic of the db schema but, it assummes the source and dest schemas matches.
func (d *Database) Append(ctx context.Context, source *Database) error {
	if source == nil || source == d {
		return fmt.Errorf("source must be a different database")
	}

	if d.accessMode != ReadWriteDatabase {
		return fmt.Errorf("destination database is read-only")
	}

	st, err := d.Store()
	if err != nil {
		return fmt.Errorf("opening destination database: %w", err)
	}

	const sourceName = "source_db"
	if err := st.AttachDatabase(ctx, source, sourceName, ReadOnlyDatabase); err != nil {
		return err
	}
	defer func() {
		_ = st.DetachDatabase(context.WithoutCancel(ctx), sourceName)
	}()

	tablesQuery, args, err := sq.Select("database_name", "table_name").
		From("duckdb_tables()").
		Where(sq.Eq{"schema_name": "main"}).
		OrderBy("database_name", "table_oid").ToSql()
	if err != nil {
		return err
	}

	rows, err := st.Querier().QueryContext(ctx, tablesQuery, args...)
	if err != nil {
		return fmt.Errorf("listing tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	destinationTables := make(map[string]bool)
	sourceTables := make([]string, 0)

	for rows.Next() {
		var databaseName string
		var table string
		if err := rows.Scan(&databaseName, &table); err != nil {
			return fmt.Errorf("reading table list: %w", err)
		}

		if databaseName == sourceName {
			sourceTables = append(sourceTables, table)
		} else {
			destinationTables[table] = true
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("listing tables: %w", err)
	}

	// for conflict cause to work, the table should have an unique constraint
	// so, list all the PK, UNIQUE and FK constraints for each table.
	// if table has the constraint, we should add ON CONFLICT clause
	// if table references another table, copy the referenced table first
	constraints := make(constraintsType)
	q, args, err := sq.Select("table_name,constraint_type,referenced_table").
		From("duckdb_constraints()").
		Where(sq.Eq{"constraint_type": []string{"PRIMARY KEY", "UNIQUE", "FOREIGN KEY"}}).
		Where(sq.Eq{"schema_name": "main"}).
		Where(sq.Expr("database_name = current_database()")).
		ToSql()
	if err != nil {
		return fmt.Errorf("building constraints query: %w", err)
	}

	qRows, qErr := st.Querier().QueryContext(ctx, q, args...)
	if qErr != nil {
		return fmt.Errorf("listing constraints: %w", qErr)
	}
	defer func() { _ = qRows.Close() }()

	for qRows.Next() {
		var (
			table           string
			constraintType  string
			referencedTable *string
		)
		if err := qRows.Scan(&table, &constraintType, &referencedTable); err != nil {
			return fmt.Errorf("reading constraints list: %w", err)
		}

		refTable := ""
		if referencedTable != nil {
			refTable = *referencedTable
		}

		items, found := constraints[table]
		if !found {
			constraints[table] = []constraint{{Kind: constraintType, ReferencedTable: refTable}}
			continue
		}

		items = append(items, constraint{Kind: constraintType, ReferencedTable: refTable})
		constraints[table] = items
	}

	if err := qRows.Err(); err != nil {
		return fmt.Errorf("listing constraints: %w", err)
	}

	// order the sourceTables by referenced table.
	// The idea is to put referenced tables in front of the corresponding referee table.
	// like bubble sort: the referenced table is swap in place with the referee one.
	// Ex: if the sourceTables = [a,b] and a has a FK to b, the resulting list is [b,a]
	// It does not handle FK cycles. a -> b and b->a
	changed := true
	for changed {
		changed = false
		for i := 0; i < len(sourceTables); i++ {
			t := sourceTables[i]
			refTables := constraints.getReferencedTables(t)
			if len(refTables) == 0 {
				continue
			}

			// find the first "referee" after the current position swap it
			for j := i + 1; j < len(sourceTables); j++ {
				if slices.Index(refTables, sourceTables[j]) >= 0 {
					temp := sourceTables[i]
					sourceTables[i] = sourceTables[j]
					sourceTables[j] = temp
					changed = true
					break
				}
			}
		}
	}

	for _, table := range sourceTables {
		if !destinationTables[table] {
			continue
		}

		quotedTable := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
		copyQuery := sq.Insert("main." + quotedTable + " BY NAME").
			Select(sq.Select("*").From(sourceName + ".main." + quotedTable))

		if constraints.HasConstraint(table, "PRIMARY KEY") || constraints.HasConstraint(table, "UNIQUE") {
			copyQuery = copyQuery.Suffix("ON CONFLICT DO NOTHING")
		}

		q, args, err := copyQuery.ToSql()
		if err != nil {
			return fmt.Errorf("building copy query for table %q: %w", table, err)
		}

		if _, err := st.Querier().ExecContext(ctx, q, args...); err != nil {
			return fmt.Errorf("copying table %q: %w", table, err)
		}
	}

	return nil
}

// Clone exports this database and imports it into a new database at dstPath.
// The source connection stays open and is not modified.
func (d *Database) Clone(ctx context.Context) (*Database, error) {
	d.mu.Lock()
	if d.connection == nil {
		conn, err := newDatabase(NewDefaultExtentionLoader(), d.Path, d.memoryLimit, d.accessMode)
		if err != nil {
			d.mu.Unlock()
			return nil, err
		}
		d.connection = conn
	}
	conn := d.connection
	d.mu.Unlock()

	exportDir, err := os.MkdirTemp(filepath.Dir(d.Path), "duckdb-export-*")
	if err != nil {
		return nil, fmt.Errorf("creating export dir: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(exportDir); err != nil {
			zap.S().Warnw("failed to remove backup folder when cloning db", "id", d.ID, "error", err)
		}
	}()

	if _, err := conn.ExecContext(ctx, fmt.Sprintf("EXPORT DATABASE '%s'", exportDir)); err != nil {
		return nil, fmt.Errorf("exporting database: %w", err)
	}

	cloneID := string(uuid.NewString()[:6])
	clonePath := filepath.Join(filepath.Dir(d.Path), fmt.Sprintf("%s-%s.duckdb", filepath.Base(d.Path), cloneID))
	dstConn, err := newDatabase(NewDefaultExtentionLoader(), clonePath, d.memoryLimit, ReadWriteDatabase)
	if err != nil {
		return nil, fmt.Errorf("opening destination database: %w", err)
	}

	if _, err := dstConn.ExecContext(ctx, fmt.Sprintf("IMPORT DATABASE '%s'", exportDir)); err != nil {
		return nil, fmt.Errorf("importing database: %w", err)
	}

	clone := Database{
		ID:          cloneID,
		Path:        clonePath,
		CreatedAt:   time.Now(),
		connection:  dstConn,
		accessMode:  ReadWriteDatabase,
		memoryLimit: d.memoryLimit,
	}

	return &clone, nil
}

// Migrate migrates the database.
// The migration schema depends on the target database: main or collection therefore it is caller responsibility to pass the right migration fn
func (d *Database) Migrate(ctx context.Context, fn func(ctx context.Context, db *sql.DB) error) error {
	d.mu.Lock()
	if d.connection == nil {
		conn, err := newDatabase(NewDefaultExtentionLoader(), d.Path, d.memoryLimit, d.accessMode)
		if err != nil {
			d.mu.Unlock()
			return err
		}
		d.connection = conn
	}
	d.mu.Unlock()

	return fn(ctx, d.connection)
}

type Pool struct {
	mu                       sync.Mutex
	databases                map[string]*Database
	cleanupTimer             *time.Timer
	cleanupInterval          time.Duration
	latestCollectionDatabase atomic.Pointer[Database] // this is the hot path for services asking for the latest collection db
}

func NewPool(cleanupInterval time.Duration) *Pool {
	nextCleanup := time.NewTimer(cleanupInterval)
	return &Pool{
		databases:       map[string]*Database{},
		cleanupTimer:    nextCleanup,
		cleanupInterval: cleanupInterval,
	}
}

func (p *Pool) NewDatabase(id string, dbPath string, createdAt time.Time, initType ConnectionInitilizationType, memoryLimit int, accessMode DatabaseAccessMode) (*Database, error) {
	db := &Database{
		ID:          id,
		Path:        dbPath,
		CreatedAt:   createdAt,
		memoryLimit: memoryLimit,
		accessMode:  accessMode,
	}

	if initType == EagerConnectionInitilization {
		conn, err := newDatabase(NewDefaultExtentionLoader(), dbPath, memoryLimit, accessMode)
		if err != nil {
			return nil, err
		}
		db.connection = conn
		db.store = newStore(filepath.Base(dbPath), pkgstore.NewQueryInterceptor(conn), pkgstore.NewTransactor(conn))
	}

	return db, nil
}

func (p *Pool) Add(db *Database) {
	if db.ID != MainDatabaseID {
		p.latestCollectionDatabase.Store(db)
	}

	p.mu.Lock()
	if _, ok := p.databases[db.ID]; !ok {
		p.databases[db.ID] = db
	}
	p.mu.Unlock()
}

func (p *Pool) Get(id string) (*Database, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	select {
	case <-p.cleanupTimer.C:
		p.cleanupTimer.Reset(p.cleanupInterval)
		p.cleanup()
	default:
	}

	db, ok := p.databases[id]
	if !ok {
		return nil, errors.NewResourceNotFoundError("database", id)
	}

	return db, nil
}

func (p *Pool) List() []*Database {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.list()
}

func (p *Pool) Latest() (*Database, error) {
	db := p.latestCollectionDatabase.Load()
	if db != nil {
		return db, nil
	}

	return nil, errors.NewResourceNotFoundError("collection", "latest")
}

// All returns an iterator on sorted list of databases based on createdAt
func (p *Pool) All() iter.Seq[*Database] {
	dbs := p.List()
	slices.SortFunc(dbs, func(a *Database, b *Database) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})

	return func(yield func(*Database) bool) {
		for _, db := range dbs {
			if !yield(db) {
				return
			}
		}
	}
}

func (p *Pool) Delete(id string) error {
	db, err := p.Get(id)
	if err != nil {
		if errors.IsResourceNotFoundError(err) {
			return nil
		}
		return err
	}

	if err := db.Close(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.databases, id)

	if p.latestCollectionDatabase.Load() == db {
		dbs := p.list()

		p.latestCollectionDatabase.Store(nil)
		// the latestCollectionDatabase must point to the latest db except main
		slices.SortFunc(dbs, func(a *Database, b *Database) int {
			return b.CreatedAt.Compare(a.CreatedAt)
		})

		for _, db := range dbs {
			if db.ID == MainDatabaseID {
				continue
			}
			p.latestCollectionDatabase.Store(db)
			break
		}
	}

	return os.Remove(db.Path)
}

func (p *Pool) Close() {
	p.cleanupTimer.Stop()

	p.mu.Lock()
	defer p.mu.Unlock()

	for id, db := range p.databases {
		if err := db.Close(); err != nil {
			zap.S().Errorw("failed to close db connection on pool shutdown", "db_id", id, "error", err)
		}
	}
}

func (p *Pool) list() []*Database {
	databases := make([]*Database, 0, len(p.databases))
	for _, db := range p.databases {
		databases = append(databases, db)
	}
	return databases
}

func (p *Pool) cleanup() {
	for _, db := range p.databases {
		if db.ID == MainDatabaseID {
			continue
		}

		if db == p.latestCollectionDatabase.Load() {
			continue
		}

		if db.LastAccess() == 0 {
			continue
		}

		if time.Since(time.Unix(0, db.LastAccess())) < p.cleanupInterval {
			continue
		}

		if err := db.Close(); err != nil {
			zap.S().Errorw("failed to close idle db connection", "db_id", db.ID, "error", err)
		}
	}
}
