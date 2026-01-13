package models

import "time"

// Credentials represents stored vCenter credentials.
type Credentials struct {
	URL                  string
	Username             string
	Password             string
	IsDataSharingAllowed bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}
