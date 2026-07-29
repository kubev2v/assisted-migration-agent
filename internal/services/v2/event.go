package v2

import (
	"context"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

type EventService struct {
	store *store.Store2
}

func NewEventService(st *store.Store2) *EventService {
	return &EventService{store: st}
}

func (es *EventService) Events(ctx context.Context) ([]models.Event, error) {
	return es.store.Outbox().Get(ctx)
}

func (es *EventService) Delete(ctx context.Context, maxID int) error {
	return es.store.Outbox().Delete(ctx, maxID)
}

func (es *EventService) AddInventoryUpdateEvent(ctx context.Context, inventory []byte) error {
	return es.store.Outbox().Insert(ctx, models.Event{
		Kind: models.InventoryUpdateEvent,
		Data: inventory,
	})
}

func (es *EventService) AddGroupInventoryEvent(ctx context.Context, data []byte) error {
	return es.store.Outbox().Insert(ctx, models.Event{
		Kind: models.GroupInventoryUpsertEvent,
		Data: data,
	})
}

func (es *EventService) AddGroupInventoryDeleteEvent(ctx context.Context, data []byte) error {
	return es.store.Outbox().Insert(ctx, models.Event{
		Kind: models.GroupInventoryDeleteEvent,
		Data: data,
	})
}
