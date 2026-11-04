package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

// Migrate initializes the main database and upgrades registered collections.
// Unusable collections are excluded from the pool, retaining their files for recovery.
func (p *Pool) Migrate(ctx context.Context) error {
	mainDB, err := p.Get(MainDatabaseID)
	if err != nil {
		return err
	}
	if err := mainDB.Migrate(ctx, migrations.RunMain); err != nil {
		return fmt.Errorf("failed to migrate main database: %w", err)
	}

	for _, db := range p.List() {
		if db.ID == MainDatabaseID {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(db.Path), ".duckdb")
		migrationErr := db.Migrate(ctx, func(ctx context.Context, conn *sql.DB) error {
			return migrations.RunCollection(ctx, conn, name)
		})
		err := errors.Join(migrationErr, db.Close())
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			zap.S().Errorw("failed to migrate collection database", "db_name", name, "error", err)
			if err := p.Unregister(db.ID); err != nil {
				return fmt.Errorf("unregistering collection database %s: %w", name, err)
			}
		}
	}

	return nil
}

// ValidateMigrations requires existing databases with the exact history expected by this image.
// It does not apply SQL migrations or initialize missing databases.
func (p *Pool) ValidateMigrations(ctx context.Context) error {
	mainDB, err := p.Get(MainDatabaseID)
	if err != nil {
		return err
	}
	info, err := os.Lstat(mainDB.Path)
	if err != nil {
		return fmt.Errorf("--auto-migrate=false requires an existing migrated main database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("--auto-migrate=false requires a regular main database file: %s", mainDB.Path)
	}

	// Discovery skips malformed names for automatic startup; strict startup must
	// reject them before validating or cleaning up any databases.
	entries, err := os.ReadDir(filepath.Dir(mainDB.Path))
	if err != nil {
		return fmt.Errorf("failed to list collection databases: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "collection_") || !strings.HasSuffix(name, ".duckdb") {
			continue
		}
		timestamp := strings.TrimSuffix(strings.TrimPrefix(name, "collection_"), ".duckdb")
		_, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			return fmt.Errorf("failed to validate collection filename %s: %w", name, err)
		}
	}

	if err := mainDB.Migrate(ctx, migrations.ValidateMain); err != nil {
		return fmt.Errorf("failed to validate main database: %w", err)
	}

	for _, db := range p.List() {
		if db.ID == MainDatabaseID {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(db.Path), ".duckdb")
		info, err := os.Lstat(db.Path)
		if err != nil {
			return fmt.Errorf("failed to inspect collection database %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("collection database must be a regular file: %s", db.Path)
		}
		validationErr := db.Migrate(ctx, func(ctx context.Context, conn *sql.DB) error {
			return migrations.ValidateCollection(ctx, conn, name)
		})
		if err := errors.Join(validationErr, db.Close()); err != nil {
			return fmt.Errorf("failed to validate collection database %s: %w", name, err)
		}
	}

	return nil
}
