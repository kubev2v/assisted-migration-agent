package models

import (
	"time"

	"github.com/kubev2v/migration-planner/pkg/inventory"
)

type Group struct {
	ID          int
	Name        string
	Description string
	Filter      string
	Inventory   *inventory.Inventory
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
