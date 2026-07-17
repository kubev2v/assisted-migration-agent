package v2

import (
	"context"
	"slices"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

type EventService struct {
	pool *store.Pool
}

func NewEventService(pool *store.Pool) *EventService {
	return &EventService{pool: pool}
}

func (es *EventService) Events(ctx context.Context) ([]models.Event, error) {
	store := es.store()
	if store == nil {
		return []models.Event{}, nil
	}
	return store.Outbox().Get(ctx)
}

func (es *EventService) Delete(ctx context.Context, maxID int) error {
	store := es.store()
	if store == nil {
		return nil
	}
	return store.Outbox().Delete(ctx, maxID)
}

func (es *EventService) AddInventoryUpdateEvent(ctx context.Context, inventory []byte) error {
	store := es.store()
	if store == nil {
		return nil
	}
	return store.Outbox().Insert(ctx, models.Event{
		Kind: models.InventoryUpdateEvent,
		Data: inventory,
	})
}

func (es *EventService) store() *store.Store2 {
	// get events only from the latest database
	dbs := es.pool.List()

	if len(dbs) == 0 {
		return nil
	}

	slices.SortFunc(dbs, func(a *store.Database, b *store.Database) int {
		if a.CreatedAt.Before(b.CreatedAt) {
			return 1
		}
		if a.CreatedAt.After(b.CreatedAt) {
			return -1
		}
		return 0
	})

	st, err := dbs[0].Store()
	if err != nil {
		return nil
	}
	return st
}
