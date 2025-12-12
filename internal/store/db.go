package store

import (
	"database/sql"

	_ "github.com/duckdb/duckdb-go/v2"
)

// NewDB opens a DuckDB database at the given path.
// Use ":memory:" for an in-memory database (useful for testing).
func NewDB(path string) (*sql.DB, error) {
	conn, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, err
	}

	// Verify connection works
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}
