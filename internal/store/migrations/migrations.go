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

	"go.uber.org/zap"
)

const (
	mainDatabase = "agent.main"
)

//go:embed main/*.sql
var mainMigrationFiles embed.FS

//go:embed collection/*.sql
var collectionMigrationFiles embed.FS

func RunMain(ctx context.Context, db *sql.DB) error {
	return run(ctx, db, mainMigrationFiles, "main", fmt.Sprintf("%s.schema_migrations", mainDatabase))
}

func RunCollection(ctx context.Context, db *sql.DB, database string) error {
	return run(ctx, db, collectionMigrationFiles, "collection", fmt.Sprintf("%s.main.collection_schema_migrations", database))
}

func Run(ctx context.Context, db *sql.DB, database string) error {
	if err := RunMain(ctx, db); err != nil {
		return err
	}
	return RunCollection(ctx, db, database)
}

func run(ctx context.Context, db *sql.DB, files embed.FS, dir, migrationsTable string) error {
	if err := createMigrationsTable(ctx, db, migrationsTable); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	applied, err := getAppliedVersions(ctx, db, migrationsTable)
	if err != nil {
		return fmt.Errorf("getting applied versions: %w", err)
	}

	sqlFiles, err := getMigrationFiles(files, dir)
	if err != nil {
		return fmt.Errorf("getting migration files: %w", err)
	}

	for _, file := range sqlFiles {
		version := extractVersion(file)
		if version == 0 {
			zap.S().Warnf("skipping invalid migration file: %s", file)
			continue
		}

		if applied[version] {
			zap.S().Debugf("migration %03d already applied, skipping", version)
			continue
		}

		if err := runMigration(ctx, db, files, file, version, migrationsTable); err != nil {
			return fmt.Errorf("migration %s failed: %w", file, err)
		}
		zap.S().Infof("applied migration: %s", file)
	}

	return nil
}

func createMigrationsTable(ctx context.Context, db *sql.DB, table string) error {
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT now()
		)
	`, table))
	return err
}

func getAppliedVersions(ctx context.Context, db *sql.DB, table string) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT version FROM %s`, table))
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

func getMigrationFiles(files embed.FS, dir string) ([]string, error) {
	var result []string
	err := fs.WalkDir(files, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".sql") {
			result = append(result, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(result)
	return result, nil
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

func runMigration(ctx context.Context, db *sql.DB, files embed.FS, file string, version int, migrationsTable string) error {
	content, err := files.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading migration file: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		return fmt.Errorf("executing migration: %w", err)
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (version) VALUES (?)`, migrationsTable), version); err != nil {
		return fmt.Errorf("recording migration: %w", err)
	}

	return tx.Commit()
}
