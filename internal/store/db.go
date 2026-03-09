package store

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "github.com/duckdb/duckdb-go/v2"
)

// StringArray is a scanner for DuckDB VARCHAR[] arrays.
// DuckDB returns arrays as []interface{}, this converts them to []string.
type StringArray []string

// Scan implements sql.Scanner for DuckDB VARCHAR[] arrays.
func (s *StringArray) Scan(src any) error {
	if src == nil {
		*s = []string{}
		return nil
	}

	switch v := src.(type) {
	case []interface{}:
		result := make([]string, len(v))
		for i, elem := range v {
			if elem == nil {
				result[i] = ""
			} else if str, ok := elem.(string); ok {
				result[i] = str
			} else {
				result[i] = fmt.Sprintf("%v", elem)
			}
		}
		*s = result
		return nil
	case []string:
		*s = v
		return nil
	default:
		return fmt.Errorf("unsupported type for StringArray: %T", src)
	}
}

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
