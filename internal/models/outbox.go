package models

import "encoding/json"

type EventKind string

const (
	InventoryUpdateEvent      EventKind = "inventory_update"
	GroupInventoryUpsertEvent EventKind = "group_inventory_upsert"
	GroupInventoryDeleteEvent EventKind = "group_inventory_delete"
)

type Event struct {
	ID   int       `db:"id"`
	Kind EventKind `db:"event_type"`
	Data []byte    `db:"payload"`
}

// GroupInventoryEventPayload represents the payload for group inventory upsert events.
// Fields like vmsCount and vCenterID are extracted from inventory when processing the event.
type GroupInventoryEventPayload struct {
	GroupID   string          `json:"groupID"`
	GroupName string          `json:"groupName"`
	Inventory json.RawMessage `json:"inventory"` // API-formatted inventory JSON, or null
}

// GroupInventoryDeleteEventPayload represents the payload for group inventory delete events.
type GroupInventoryDeleteEventPayload struct {
	GroupID   string `json:"groupID"`
	GroupName string `json:"groupName"`
}
