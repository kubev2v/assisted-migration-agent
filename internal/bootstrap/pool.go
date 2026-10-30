package bootstrap

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

func InitPool(dataFolder string) (*store.Pool, error) {
	pool := store.NewPool(5 * time.Minute)

	// Existing migrations reference the DuckDB catalog name "agent". Each product
	// supplies its own data folder, so their databases remain isolated.
	mainPath := filepath.Join(dataFolder, "agent.duckdb")
	mainDB, err := pool.NewDatabase(store.MainDatabaseID, mainPath, time.Now(), store.EagerConnectionInitilization, 256, store.ReadWriteDatabase)
	if err != nil {
		return nil, fmt.Errorf("failed to create main database: %w", err)
	}

	if err := mainDB.Migrate(context.Background(), migrations.RunMain); err != nil {
		return nil, fmt.Errorf("failed to migrate main database: %w", err)
	}

	pool.Add(mainDB)
	zap.S().Infow("registered main database", "path", mainDB.Path)

	if err := cleanupStaleCollections(mainDB, dataFolder); err != nil {
		zap.S().Errorw("failed to cleanup stale collections", "error", err)
	}

	matches, _ := filepath.Glob(filepath.Join(dataFolder, "collection_*.duckdb"))
	for _, match := range matches {
		name := strings.TrimSuffix(filepath.Base(match), ".duckdb")
		tsStr := strings.TrimPrefix(name, "collection_")
		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			zap.S().Warnw("skipping collection with unparseable timestamp", "file", match)
			continue
		}

		createdAt := time.Unix(ts, 0)
		hash := sha256.Sum256([]byte(match))
		id := hex.EncodeToString(hash[:])[:6]
		// A collection that cannot be opened or migrated fails startup instead of silently
		// disappearing from the UI; after an update this triggers the image rollback.
		db, err := pool.NewDatabase(id, match, createdAt, store.LazyConnectionInitilization, 512, store.ReadWriteDatabase)
		if err != nil {
			pool.Close()
			return nil, fmt.Errorf("opening collection database %s: %w", name, err)
		}

		if err := db.Migrate(context.Background(), func(ctx context.Context, db *sql.DB) error {
			return migrations.RunCollection(ctx, db, name)
		}); err != nil {
			_ = db.Close()
			pool.Close()
			return nil, fmt.Errorf("migrating collection database %s: %w", name, err)
		}

		if err := db.Close(); err != nil {
			zap.S().Errorw("closing database", "db", db.ID, "error", err)
			continue
		}

		pool.Add(db)
		zap.S().Infow("registered collection database", "name", name, "path", match)
	}

	return pool, nil
}

func cleanupStaleCollections(mainDB *store.Database, dataFolder string) error {
	st, err := mainDB.Store()
	if err != nil {
		return fmt.Errorf("failed to get main store: %w", err)
	}

	collections, err := st.Collection().List(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list stale collections: %w", err)
	}

	for _, col := range collections {
		dbFile := filepath.Join(dataFolder, col.Database+".duckdb")
		if err := os.Remove(dbFile); err != nil && !os.IsNotExist(err) {
			zap.S().Warnw("failed to remove stale collection file", "file", dbFile, "error", err)
			continue
		}

		if err := st.Collection().Delete(context.Background(), col.Database); err != nil {
			zap.S().Warnw("failed to delete stale collection marker", "database", col.Database, "error", err)
			continue
		}

		zap.S().Infow("cleaned up stale collection", "database", col.Database, "state", col.State)
	}

	return nil
}
