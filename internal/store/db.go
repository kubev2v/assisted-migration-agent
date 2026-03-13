package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/duckdb/duckdb-go/v2"
	"go.uber.org/zap"
)

// Extension defines a DuckDB extension to be loaded.
type Extension struct {
	Name string // Extension name used for INSTALL/LOAD (e.g., "sqlite")
	File string // Local file name to look for (e.g., "sqlite_scanner.duckdb_extension")
}

// ExtensionLoader handles loading DuckDB extensions from local files or remote repository.
type ExtensionLoader struct {
	extensions []Extension
}

func NewDefaultExtentionLoader() *ExtensionLoader {
	defaultExt := []Extension{
		{Name: "sqlite", File: "sqlite_scanner.duckdb_extension"},
	}
	return NewExtensionLoader(defaultExt...)
}

// NewExtensionLoader creates a new ExtensionLoader for the given directory.
func NewExtensionLoader(extensions ...Extension) *ExtensionLoader {
	return &ExtensionLoader{
		extensions: extensions,
	}
}

// Load installs and loads all configured extensions.
// For each extension, it first checks if the local file exists in extDir.
// If found, it loads from the local path. Otherwise, it downloads using the extension name.
func (l *ExtensionLoader) Load(conn *sql.DB, extDir string) error {
	for _, ext := range l.extensions {
		if err := l.loadExtension(conn, extDir, ext); err != nil {
			return err
		}
	}
	return nil
}

func (l *ExtensionLoader) loadExtension(conn *sql.DB, extDir string, ext Extension) error {
	extPath := filepath.Join(extDir, ext.File)

	if _, err := os.Stat(extPath); err == nil {
		// Local extension found, install and load from path
		zap.S().Infof("loading %s extension from local file: %s", ext.Name, extPath)
		if _, err := conn.Exec(fmt.Sprintf("INSTALL '%s'", extPath)); err != nil {
			return fmt.Errorf("installing %s extension from local path: %w", ext.Name, err)
		}
		if _, err := conn.Exec(fmt.Sprintf("LOAD '%s'", extPath)); err != nil {
			return fmt.Errorf("loading %s extension from local path: %w", ext.Name, err)
		}
	} else {
		// Local extension not found, download from DuckDB repository
		zap.S().Infof("loading %s extension from DuckDB repository", ext.Name)
		if _, err := conn.Exec(fmt.Sprintf("INSTALL %s", ext.Name)); err != nil {
			return fmt.Errorf("installing %s extension: %w", ext.Name, err)
		}
		if _, err := conn.Exec(fmt.Sprintf("LOAD %s", ext.Name)); err != nil {
			return fmt.Errorf("loading %s extension: %w", ext.Name, err)
		}
	}
	return nil
}

// NewDB opens a DuckDB database at the given path.
// Use ":memory:" for an in-memory database (useful for testing).
func NewDB(loader *ExtensionLoader, path string) (*sql.DB, error) {
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

	if path == ":memory:" {
		return conn, nil
	}

	// Configure extension directory to the same folder as the database
	// This prevents DuckDB from trying to write to ~/.duckdb which may be read-only
	extDir := filepath.Dir(path)
	if _, err := conn.Exec(fmt.Sprintf("SET extension_directory = '%s'", extDir)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("setting extension directory: %w", err)
	}

	if err := loader.Load(conn, extDir); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return conn, nil
}
