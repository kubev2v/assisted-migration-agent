package credentials

import (
	"errors"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

// ErrNotFound is returned when credentials are not found in the store.
var ErrNotFound = errors.New("credentials not found")

// Store defines the interface for credential storage.
// Implementations can store credentials on disk.
type Store interface {
	// Save persists the vCenter credentials.
	Save(creds models.VCenterCredentials) error

	// Load retrieves the stored credentials.
	// Returns ErrNotFound if no credentials are stored.
	Load() (*models.VCenterCredentials, error)

	// Delete removes the stored credentials.
	// Returns nil if no credentials exist.
	Delete() error

	// Exists checks if credentials are stored.
	Exists() bool
}
