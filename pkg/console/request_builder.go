package console

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	v1 "github.com/kubev2v/migration-planner/api/v1alpha1"

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
			// UpdateSourceStatus updates the source's main inventory
			// Note: UpdateSource is for metadata only (name, labels, proxy settings)
			return b.client.UpdateSourceStatus(ctx, b.sourceID, b.agentID, event.Data)
		}, nil

	case models.GroupInventoryUpsertEvent:
		return b.buildGroupInventoryRequest(event)

	case models.GroupInventoryDeleteEvent:
		return b.buildGroupDeleteRequest(event)

	default:
		return nil, errors.NewUnknownEventKindError(string(event.Kind))
	}
}

func (b *RequestBuilder) buildGroupInventoryRequest(event models.Event) (func(ctx context.Context) error, error) {
	var payload struct {
		GroupID   string       `json:"groupID"`
		GroupName string       `json:"groupName"`
		Inventory v1.Inventory `json:"inventory"`
	}

	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return nil, fmt.Errorf("unmarshaling group inventory event: %w", err)
	}

	// Parse UUID directly (no generation needed)
	subsetID, err := uuid.Parse(payload.GroupID)
	if err != nil {
		return nil, fmt.Errorf("parsing group UUID: %w", err)
	}

	// Extract vmsCount from inventory by summing Total VMs across all clusters
	vmsCount := 0
	for _, cluster := range payload.Inventory.Clusters {
		vmsCount += cluster.Vms.Total
	}

	// If group has 0 VMs, delete it from backend instead of upserting.
	// This handles both "created empty" and "updated to empty" cases.
	// DELETE is idempotent (404 = success), so this works whether the
	// subset exists in the backend or not.
	if vmsCount == 0 {
		return func(ctx context.Context) error {
			return b.client.DeleteSourceSubset(ctx, b.sourceID, subsetID)
		}, nil
	}

	// Group has VMs - upsert to backend (creates if missing, updates if exists)
	return func(ctx context.Context) error {
		return b.client.UpdateSourceSubset(ctx, b.sourceID, subsetID, payload.GroupName, vmsCount, payload.Inventory)
	}, nil
}

func (b *RequestBuilder) buildGroupDeleteRequest(event models.Event) (func(ctx context.Context) error, error) {
	var payload struct {
		GroupID string `json:"groupID"`
	}

	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return nil, fmt.Errorf("unmarshaling group delete event: %w", err)
	}

	// Parse UUID directly (no generation needed)
	subsetID, err := uuid.Parse(payload.GroupID)
	if err != nil {
		return nil, fmt.Errorf("parsing group UUID: %w", err)
	}

	return func(ctx context.Context) error {
		return b.client.DeleteSourceSubset(ctx, b.sourceID, subsetID)
	}, nil
}
