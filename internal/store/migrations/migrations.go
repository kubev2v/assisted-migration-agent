package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kubev2v/migration-planner-common/pkg/duckdb_parser"

	"go.uber.org/zap"
)

//go:embed sql/*.sql
var migrationFiles embed.FS

// Run executes all pending migrations in order.
func Run(ctx context.Context, db *sql.DB) error {
	if err := duckdb_parser.New(db, nil).Init(); err != nil {
		return err
	}

	// Ensure migrations tracking table exists
	if err := createMigrationsTable(ctx, db); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	// Get already applied versions
	applied, err := getAppliedVersions(ctx, db)
	if err != nil {
		return fmt.Errorf("getting applied versions: %w", err)
	}

	// Get migration files
	files, err := getMigrationFiles()
	if err != nil {
		return fmt.Errorf("getting migration files: %w", err)
	}

	// Run pending migrations
	for _, file := range files {
		version := extractVersion(file)
		if version == 0 {
			zap.S().Warnf("skipping invalid migration file: %s", file)
			continue
		}

		if applied[version] {
			zap.S().Debugf("migration %03d already applied, skipping", version)
			continue
		}

		if err := runMigration(ctx, db, file, version); err != nil {
			return fmt.Errorf("migration %s failed: %w", file, err)
		}
		zap.S().Infof("applied migration: %s", file)
	}

	return nil
}

func createMigrationsTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT now()
		)
	`)
	return err
}

func getAppliedVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func getMigrationFiles() ([]string, error) {
	var files []string
	err := fs.WalkDir(migrationFiles, "sql", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".sql") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func extractVersion(filename string) int {
	base := filepath.Base(filename)
	parts := strings.SplitN(base, "_", 2)
	if len(parts) < 1 {
		return 0
	}
	v, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	return v
}

func runMigration(ctx context.Context, db *sql.DB, file string, version int) error {
	content, err := migrationFiles.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading migration file: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Execute migration SQL
	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		return fmt.Errorf("executing migration: %w", err)
	}

	// Record migration as applied
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
		return fmt.Errorf("recording migration: %w", err)
	}

	return tx.Commit()
}
