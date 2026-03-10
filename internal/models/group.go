package models

import (
	"time"
)

type Group struct {
	ID          int
	Name        string
	Description string
	Filter      string
	Tags        []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
