package models

import (
	"time"
)

// Inventory represents inventory data stored in the database.
type Inventory struct {
	Data      []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}
