package v2

import "github.com/kubev2v/assisted-migration-agent/internal/store"

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
