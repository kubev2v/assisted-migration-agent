package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// CredentialsStore handles credentials storage using DuckDB.
type CredentialsStore struct {
	db *sql.DB
}

// NewCredentialsStore creates a new credentials store.
func NewCredentialsStore(db *sql.DB) *CredentialsStore {
	return &CredentialsStore{db: db}
}

// Get retrieves the stored credentials.
func (s *CredentialsStore) Get(ctx context.Context) (*models.Credentials, error) {
	row := s.db.QueryRowContext(ctx, queryGetCredentials)

	var c models.Credentials
	err := row.Scan(&c.URL, &c.Username, &c.Password, &c.IsDataSharingAllowed, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, srvErrors.NewCredentialsNotFoundError()
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Save stores or updates the credentials.
func (s *CredentialsStore) Save(ctx context.Context, creds *models.Credentials) error {
	_, err := s.db.ExecContext(ctx, queryUpsertCredentials,
		creds.URL, creds.Username, creds.Password, creds.IsDataSharingAllowed)
	return err
}

// Delete removes the stored credentials.
func (s *CredentialsStore) Delete(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, queryDeleteCredentials)
	return err
}
