package services

import (
	"context"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

type EventService struct {
	st *store.Store
}

func NewEventService(st *store.Store) *EventService {
	return &EventService{st: st}
}

func (es *EventService) Events(ctx context.Context) ([]models.Event, error) {
	return es.st.Outbox().Get(ctx)
}

func (es *EventService) Delete(ctx context.Context, maxID int) error {
	return es.st.Outbox().Delete(ctx, maxID)
}

func (es *EventService) AddInventoryUpdateEvent(ctx context.Context, inventory []byte) error {
	return es.st.Outbox().Insert(ctx, models.Event{
		Kind: models.InventoryUpdateEvent,
		Data: inventory,
	})
}
