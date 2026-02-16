package store

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "github.com/duckdb/duckdb-go/v2"
)

// NewDB opens a DuckDB database at the given path.
// Use ":memory:" for an in-memory database (useful for testing).
func NewDB(path string) (*sql.DB, error) {
	conn, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, err
	}

	// DuckDB is single-writer; a single connection prevents idle pool
	// connections from blocking WAL checkpointing.
	conn.SetMaxOpenConns(1)

	// Verify connection works
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	// Configure extension directory to the same folder as the database
	// This prevents DuckDB from trying to write to ~/.duckdb which may be read-only
	if path != ":memory:" {
		extDir := filepath.Dir(path)
		if _, err := conn.Exec(fmt.Sprintf("SET extension_directory = '%s'", extDir)); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("setting extension directory: %w", err)
		}
	}

	return conn, nil
}
