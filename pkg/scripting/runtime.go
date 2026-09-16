package scripting

import (
	"context"
	"encoding/json"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
)

type runtime struct {
	pool *store.Pool
}

func (r *runtime) ListCollections() []CollectionInfo {
	var result []CollectionInfo
	for _, db := range r.pool.List() {
		if db.ID == store.MainDatabaseID {
			continue
		}
		result = append(result, CollectionInfo{ID: db.ID, CreatedAt: db.CreatedAt})
	}
	return result
}

func (r *runtime) ListVirtualMachines(ctx context.Context, collectionID, expression string) ([]models.VM, error) {
	st, err := r.getStore(collectionID)
	if err != nil {
		return nil, err
	}
	return st.VM().ListDetailedVirtualMachines(ctx, store.ByFilter(expression))
}

func (r *runtime) ListGroups(ctx context.Context, collectionID string) ([]models.Group, error) {
	st, err := r.getStore(collectionID)
	if err != nil {
		return nil, err
	}
	groups, err := st.Group().List(ctx, nil, 0, 0)
	if err != nil {
		return nil, err
	}
	return groups, nil
}

func (r *runtime) GetInventory(ctx context.Context, collectionID string) (*models.Inventory, error) {
	st, err := r.getStore(collectionID)
	if err != nil {
		return nil, err
	}
	return st.Inventory().Get(ctx)
}

func (r *runtime) BuildInventory(ctx context.Context, collectionID string, vmIDs []string) ([]byte, error) {
	st, err := r.getStore(collectionID)
	if err != nil {
		return nil, err
	}
	parser := duckdb_parser.New(st.Querier(), nil)
	inv, err := parser.BuildInventory(ctx, vmIDs)
	if err != nil {
		return nil, err
	}
	return json.Marshal(inv)
}

func (r *runtime) getStore(collectionID string) (*store.Store, error) {
	db, err := r.pool.Get(collectionID)
	if err != nil {
		return nil, err
	}
	return db.Store()
}
