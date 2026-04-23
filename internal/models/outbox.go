package models

type EventKind string

const (
	InventoryUpdateEvent EventKind = "inventory_update"
)

type Event struct {
	ID   int       `db:"id"`
	Kind EventKind `db:"event_type"`
	Data []byte    `db:"payload"`
}
