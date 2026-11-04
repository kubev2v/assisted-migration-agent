package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

// InitPool discovers databases without opening connections, migrating, or cleaning up files.
func InitPool(dataFolder string) (*store.Pool, error) {
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
	mainDB, err := pool.NewDatabase(store.MainDatabaseID, mainPath, time.Now(), store.LazyConnectionInitilization, 256, store.ReadWriteDatabase)
	if err != nil {
		return nil, fmt.Errorf("failed to register main database: %w", err)
	}
	pool.Add(mainDB)

	entries, err := os.ReadDir(dataFolder)
	if err != nil {
		return nil, fmt.Errorf("failed to list collection databases: %w", err)
	}
	for _, entry := range entries {
		filename := entry.Name()
		if !strings.HasPrefix(filename, "collection_") || !strings.HasSuffix(filename, ".duckdb") {
			continue
		}
		match := filepath.Join(dataFolder, filename)
		name := strings.TrimSuffix(filename, ".duckdb")
		tsStr := strings.TrimPrefix(name, "collection_")
		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			zap.S().Warnw("skipping malformed collection filename", "file", match, "error", err)
			continue
		}

		hash := sha256.Sum256([]byte(match))
		id := hex.EncodeToString(hash[:])[:6]
		db, err := pool.NewDatabase(id, match, time.Unix(ts, 0), store.LazyConnectionInitilization, 512, store.ReadWriteDatabase)
		if err != nil {
			return nil, fmt.Errorf("failed to register collection database %s: %w", name, err)
		}
		pool.Add(db)
	}

	initialized = true
	return pool, nil
}

// CleanupStaleCollections runs after migrations or history validation, before services start.
func CleanupStaleCollections(ctx context.Context, pool *store.Pool, dataFolder string) error {
	mainDB, err := pool.Get(store.MainDatabaseID)
	if err != nil {
		return err
	}
	st, err := mainDB.Store()
	if err != nil {
		return fmt.Errorf("failed to get main store: %w", err)
	}

	collections, err := st.Collection().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list stale collections: %w", err)
	}

	var cleanupErrors []error
	for _, col := range collections {
		dbFile := filepath.Join(dataFolder, col.Database+".duckdb")
		for _, db := range pool.List() {
			if db.ID == store.MainDatabaseID || db.Path != dbFile {
				continue
			}
			if err := pool.Unregister(db.ID); err != nil {
				return fmt.Errorf("unregistering stale collection %s: %w", col.Database, err)
			}
		}
		if err := os.Remove(dbFile); err != nil && !os.IsNotExist(err) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("failed to remove stale collection file %s: %w", dbFile, err))
			continue
		}

		if err := st.Collection().Delete(ctx, col.Database); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("failed to delete stale collection marker %s: %w", col.Database, err))
			continue
		}

		zap.S().Infow("cleaned up stale collection", "database", col.Database, "state", col.State)
	}

	return errors.Join(cleanupErrors...)
}
