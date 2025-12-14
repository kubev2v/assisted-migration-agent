package credentials

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

const credentialsFileName = "credentials.json"

// DiskStore implements Store by persisting credentials to a JSON file on disk.
type DiskStore struct {
	dataFolder string
	mu         sync.RWMutex
}

// NewDiskStore creates a new disk-based credential store.
// The credentials file will be stored at {dataFolder}/credentials.json
func NewDiskStore(dataFolder string) *DiskStore {
	return &DiskStore{
		dataFolder: dataFolder,
	}
}

// filePath returns the full path to the credentials file.
func (s *DiskStore) filePath() string {
	return filepath.Join(s.dataFolder, credentialsFileName)
}

// Save persists the vCenter credentials to disk.
func (s *DiskStore) Save(creds models.VCenterCredentials) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure the data folder exists
	if err := os.MkdirAll(s.dataFolder, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	// Write with restrictive permissions (owner read/write only)
	return os.WriteFile(s.filePath(), data, 0600)
}

// Load retrieves the stored credentials from disk.
// Returns ErrNotFound if the credentials file does not exist.
func (s *DiskStore) Load() (*models.VCenterCredentials, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.filePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	var creds models.VCenterCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}

	return &creds, nil
}

// Delete removes the stored credentials file.
// Returns nil if the file does not exist.
func (s *DiskStore) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := os.Remove(s.filePath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Exists checks if the credentials file exists.
func (s *DiskStore) Exists() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, err := os.Stat(s.filePath())
	return err == nil
}
