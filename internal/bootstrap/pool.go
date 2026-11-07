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

func InitPool(dataFolder string, autoMigrate bool) (*store.Pool, error) {
	pool := store.NewPool(5 * time.Minute)
	initialized := false
	defer func() {
		if !initialized {
			pool.Close()
		}
	}()

	// Existing migrations reference the DuckDB catalog name "agent". Each product
	// supplies its own data folder, so their databases remain isolated.
	mainPath := filepath.Join(dataFolder, "agent.duckdb")
	if !autoMigrate {
		info, err := os.Lstat(mainPath)
		if err != nil {
			return nil, fmt.Errorf("--auto-migrate=false requires an existing migrated main database: %w", err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("--auto-migrate=false requires a regular main database file: %s", mainPath)
		}
	}
	mainDB, err := pool.NewDatabase(store.MainDatabaseID, mainPath, time.Now(), store.EagerConnectionInitilization, 256, store.ReadWriteDatabase)
	if err != nil {
		return nil, fmt.Errorf("failed to create main database: %w", err)
	}

	pool.Add(mainDB)
	if autoMigrate {
		if err := mainDB.Migrate(context.Background(), migrations.RunMain); err != nil {
			return nil, fmt.Errorf("failed to migrate main database: %w", err)
		}
	} else {
		if err := mainDB.Migrate(context.Background(), migrations.ValidateMain); err != nil {
			return nil, fmt.Errorf("failed to validate main database: %w", err)
		}
	}

	zap.S().Infow("registered main database", "path", mainDB.Path)

	if autoMigrate {
		if err := cleanupStaleCollections(mainDB, dataFolder); err != nil {
			zap.S().Errorw("failed to cleanup stale collections", "error", err)
		}
	}

	matches, _ := filepath.Glob(filepath.Join(dataFolder, "collection_*.duckdb"))
	collections := make([]*store.Database, 0, len(matches))
	for _, match := range matches {
		name := strings.TrimSuffix(filepath.Base(match), ".duckdb")
		tsStr := strings.TrimPrefix(name, "collection_")
		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			if !autoMigrate {
				return nil, fmt.Errorf("failed to validate collection filename %s: %w", match, err)
			}
			zap.S().Warnw("skipping collection with unparseable timestamp", "file", match)
			continue
		}

		createdAt := time.Unix(ts, 0)
		if !autoMigrate {
			info, err := os.Lstat(match)
			if err != nil {
				return nil, fmt.Errorf("failed to inspect collection database %s: %w", name, err)
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("collection database must be a regular file: %s", match)
			}
		}
		hash := sha256.Sum256([]byte(match))
		id := hex.EncodeToString(hash[:])[:6]
		db, err := pool.NewDatabase(id, match, createdAt, store.LazyConnectionInitilization, 512, store.ReadWriteDatabase)
		if err != nil {
			if !autoMigrate {
				return nil, fmt.Errorf("failed to open collection database %s: %w", name, err)
			}
			zap.S().Warnw("skipping collection database", "file", match, "error", err)
			continue
		}

		if err := db.Migrate(context.Background(), func(ctx context.Context, db *sql.DB) error {
			if !autoMigrate {
				return migrations.ValidateCollection(ctx, db, name)
			}
			return migrations.RunCollection(ctx, db, name)
		}); err != nil {
			_ = db.Close()
			if !autoMigrate {
				return nil, fmt.Errorf("failed to validate collection database %s: %w", name, err)
			}
			zap.S().Errorw("failed to migrate collection database", "db_name", name, "error", err)
			continue
		}

		if err := db.Close(); err != nil {
			if !autoMigrate {
				return nil, fmt.Errorf("failed to close collection database %s: %w", name, err)
			}
			zap.S().Errorw("closing database", "db", db.ID, "error", err)
			continue
		}

		collections = append(collections, db)
	}

	// Check every schema before cleanup can delete files or lifecycle markers.
	if !autoMigrate {
		if err := cleanupStaleCollections(mainDB, dataFolder); err != nil {
			return nil, fmt.Errorf("failed to cleanup stale collections: %w", err)
		}
	}
	for _, db := range collections {
		if !autoMigrate {
			_, err := os.Stat(db.Path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("failed to inspect collection database %s: %w", db.Path, err)
			}
		}
		pool.Add(db)
		zap.S().Infow("registered collection database", "path", db.Path)
	}
	initialized = true
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
