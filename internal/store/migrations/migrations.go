package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

//go:embed main/up/*.sql main/down/*.sql
var mainMigrationFiles embed.FS

//go:embed collection/up/*.sql collection/down/*.sql
var collectionMigrationFiles embed.FS

func RunMain(ctx context.Context, db *sql.DB) error {
	return run(ctx, db, mainMigrationFiles, "main", `"agent"."main"."schema_migrations"`)
}

func RunCollection(ctx context.Context, db *sql.DB, database string) error {
	table := quoteIdentifier(database) + `."main"."collection_schema_migrations"`
	return run(ctx, db, collectionMigrationFiles, "collection", table)
}

func Run(ctx context.Context, db *sql.DB, database string) error {
	if err := RunMain(ctx, db); err != nil {
		return err
	}
	return RunCollection(ctx, db, database)
}

func ValidateMain(ctx context.Context, db *sql.DB) error {
	return validate(ctx, db, mainMigrationFiles, "main", `"agent"."main"."schema_migrations"`)
}

func ValidateCollection(ctx context.Context, db *sql.DB, database string) error {
	table := quoteIdentifier(database) + `."main"."collection_schema_migrations"`
	return validate(ctx, db, collectionMigrationFiles, "collection", table)
}

func validate(ctx context.Context, db *sql.DB, files embed.FS, dir, table string) error {
	r, err := loadRegistry(files, dir)
	if err != nil {
		return err
	}
	versions, err := appliedVersions(ctx, db, table)
	if err != nil {
		return fmt.Errorf("schema validation failed; apply migrations before starting with --auto-migrate=false: %w", err)
	}
	if err := r.validateHistory(versions); err != nil {
		return err
	}
	if len(versions) != len(r) {
		return fmt.Errorf("schema requires migrations (applied %v, expected version %d); apply migrations offline or enable --auto-migrate", versions, len(r))
	}
	return nil
}

func run(ctx context.Context, db *sql.DB, files embed.FS, dir, table string) error {
	r, err := loadRegistry(files, dir)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+table+` (
		version INTEGER PRIMARY KEY, applied_at TIMESTAMP DEFAULT now())`); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}
	versions, err := appliedVersions(ctx, db, table)
	if err != nil {
		return err
	}
	if err := r.validateHistory(versions); err != nil {
		return err
	}
	for _, m := range r[len(versions):] {
		if err := executeMigration(ctx, db, table, m.Version, m.Up, false); err != nil {
			return err
		}
		zap.S().Infof("applied migration: %03d_%s", m.Version, m.Name)
	}
	return nil
}

type schemaQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func appliedVersions(ctx context.Context, db schemaQuerier, table string) ([]int, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM `+table+` ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("reading migration history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	versions := make([]int, 0)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

func executeMigration(ctx context.Context, db *sql.DB, table string, version int, content string, down bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// SQL and history are one transaction so a retry cannot undo a partial step.
	if _, err := tx.ExecContext(ctx, content); err != nil {
		return fmt.Errorf("executing migration %d (down=%t): %w", version, down, err)
	}
	query := `INSERT INTO ` + table + ` (version) VALUES (?)`
	if down {
		query = `DELETE FROM ` + table + ` WHERE version = ?`
	}
	if _, err := tx.ExecContext(ctx, query, version); err != nil {
		return fmt.Errorf("recording migration %d: %w", version, err)
	}
	return tx.Commit()
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
