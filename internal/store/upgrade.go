package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// UpgradeSnapshot records successful store reads and retained assets without row contents.
type UpgradeSnapshot struct {
	Databases []string          `json:"databases"`
	Assets    map[string]string `json:"assets"`
	Records   map[string]int64  `json:"records"`
}

// ReadUpgradeSnapshot opens only existing databases, without migrations, extensions,
// cleanup, application services, or credential key initialization.
func ReadUpgradeSnapshot(ctx context.Context, folder string) (UpgradeSnapshot, error) {
	snapshot := UpgradeSnapshot{Assets: make(map[string]string), Records: make(map[string]int64)}
	root, err := os.OpenRoot(folder)
	if err != nil {
		return snapshot, err
	}
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return snapshot, err
	}
	snapshot.Databases = []string{"agent.duckdb"}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "collection_") && strings.HasSuffix(entry.Name(), ".duckdb") {
			timestamp := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "collection_"), ".duckdb")
			if _, err := strconv.ParseInt(timestamp, 10, 64); err != nil {
				return snapshot, fmt.Errorf("unsupported collection filename: %s", entry.Name())
			}
			snapshot.Databases = append(snapshot.Databases, entry.Name())
		}
	}
	slices.Sort(snapshot.Databases)
	for _, name := range snapshot.Databases {
		if err := ctx.Err(); err != nil {
			return snapshot, err
		}
		info, err := root.Lstat(name)
		if err != nil {
			return snapshot, fmt.Errorf("%s: %w", name, err)
		}
		if !info.Mode().IsRegular() || strings.ContainsAny(name, "?\x00") || strings.ContainsAny(folder, "?\x00") {
			return snapshot, fmt.Errorf("invalid upgrade database: %s", name)
		}
		db, err := newDatabase(nil, filepath.Join(folder, name), 256, ReadOnlyDatabase)
		if err != nil {
			return snapshot, fmt.Errorf("%s: open failed", name)
		}
		checkErr := validateUpgradeDatabase(ctx, db, root, name, &snapshot)
		if err := errors.Join(checkErr, db.Close()); err != nil {
			return snapshot, fmt.Errorf("%s: validation failed: %w", name, err)
		}
	}
	for _, asset := range []string{"credentials.key", "vddk"} {
		if _, err := root.Lstat(asset); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return snapshot, err
		}
		err := fs.WalkDir(root.FS(), asset, func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			file, err := root.Open(name)
			if err != nil {
				return err
			}
			hash := sha256.New()
			_, copyErr := io.Copy(hash, file)
			if err := errors.Join(copyErr, file.Close()); err != nil {
				return err
			}
			snapshot.Assets[name] = hex.EncodeToString(hash.Sum(nil))
			return nil
		})
		if err != nil {
			return snapshot, fmt.Errorf("retained asset %s is unreadable", asset)
		}
	}
	return snapshot, nil
}
