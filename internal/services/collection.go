package services

import (
	"context"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

type CollectionService struct {
	pool *store.Pool
}

func NewCollectionService(pool *store.Pool) *CollectionService {
	return &CollectionService{pool: pool}
}

func (s *CollectionService) List() []*store.Database {
	result := make([]*store.Database, 0)
	for _, db := range s.pool.List() {
		if db.ID == store.MainDatabaseID {
			continue
		}
		result = append(result, db)
	}
	return result
}

func (s *CollectionService) DeleteAll(ctx context.Context) error {
	type entry struct {
		id     string
		dbName string
	}

	entries := make([]entry, 0, 10)
	for _, db := range s.pool.List() {
		if db.ID == store.MainDatabaseID {
			continue
		}
		dbName := strings.TrimSuffix(filepath.Base(db.Path), ".duckdb")
		entries = append(entries, entry{id: db.ID, dbName: dbName})
	}

	mainDB, err := s.pool.Get(store.MainDatabaseID)
	if err != nil {
		return err
	}

	mainStore, err := mainDB.Store()
	if err != nil {
		return err
	}

	for _, e := range entries {
		err := s.pool.Delete(e.id)
		if err == nil {
			continue
		}

		// fail path
		zap.S().Errorw("failed to delete database", "id", e.id, "error", err)

		if err := mainStore.Collection().MarkPendingDelete(ctx, e.dbName); err != nil {
			zap.S().Errorw("failed to mark collection as pending delete", "database", e.dbName, "error", err)
		}
	}

	return nil
}
