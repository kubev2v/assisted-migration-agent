package store

import (
	"context"
	"database/sql"
	"iter"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kubev2v/assisted-migration-agent/pkg/errors"

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
// Database.Store() lazily initializes the connection and returns a *Store2.
// Calling Store() on a closed connection transparently reconnects.
//
// Idle eligibility is tracked by Store2 itself: every query updates a
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
//	// Get a *Store2 for any registered database.
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

type Database struct {
	ID          string
	Path        string
	CreatedAt   time.Time
	mu          sync.Mutex
	store       *Store2
	accessMode  DatabaseAccessMode
	connection  *sql.DB
	memoryLimit int
}

func (d *Database) Store() (*Store2, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.store != nil {
		return d.store, nil
	}

	conn, err := newDatabase(NewDefaultExtentionLoader(), d.Path, d.memoryLimit, d.accessMode)
	if err != nil {
		return nil, err
	}

	d.connection = conn
	d.store = newStore2(filepath.Base(d.Path), pkgstore.NewQueryInterceptor(conn), pkgstore.NewTransactor(conn))

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
		db.store = newStore2(filepath.Base(dbPath), pkgstore.NewQueryInterceptor(conn), pkgstore.NewTransactor(conn))
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

	databases := make([]*Database, 0, len(p.databases))
	for _, db := range p.databases {
		databases = append(databases, db)
	}
	return databases
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

func (p *Pool) cleanup() {
	for id, db := range p.databases {
		if db.LastAccess() == 0 {
			continue
		}

		if time.Since(time.Unix(0, db.LastAccess())) < p.cleanupInterval {
			continue
		}

		if err := db.Close(); err != nil {
			zap.S().Errorw("failed to close idle db connection", "db_id", id, "error", err)
		}
	}
}
