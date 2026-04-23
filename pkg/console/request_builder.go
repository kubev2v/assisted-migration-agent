package console

import (
	"context"

	"github.com/google/uuid"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// RequestBuilder maps outbox events to console API calls.
//
// Adding a new event kind requires only a new case here.
// The console service treats every event as an opaque func(ctx) error.
//
// Downside: producers cannot react to backend responses. The outbox is
// fire-and-forget by design.
type RequestBuilder struct {
	client   *Client
	sourceID uuid.UUID
	agentID  uuid.UUID
}

func NewRequestBuilder(client *Client, sourceID, agentID uuid.UUID) *RequestBuilder {
	return &RequestBuilder{
		client:   client,
		sourceID: sourceID,
		agentID:  agentID,
	}
}

func (b *RequestBuilder) Build(event models.Event) (func(ctx context.Context) error, error) {
	switch event.Kind {
	case models.InventoryUpdateEvent:
		return func(ctx context.Context) error {
			return b.client.UpdateSourceStatus(ctx, b.sourceID, b.agentID, event.Data)
		}, nil
	default:
		return nil, errors.NewUnknownEventKindError(string(event.Kind))
	}
}
