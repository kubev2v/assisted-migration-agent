package store

import (
	"context"
	"database/sql"
	"errors"

	sq "github.com/Masterminds/squirrel"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type CredentialsStore struct {
	db QueryInterceptor
}

func NewCredentialsStore(db QueryInterceptor) *CredentialsStore {
	return &CredentialsStore{db: db}
}

func (s *CredentialsStore) Get(ctx context.Context) (*models.Credentials, error) {
	query, args, err := sq.Select("url", "username", "password").
		From("credentials").
		Where(sq.Eq{"id": 1}).
		ToSql()
	if err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	var c models.Credentials
	err = row.Scan(&c.URL, &c.Username, &c.Password)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, srvErrors.NewCredentialsNotFoundError()
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *CredentialsStore) Insert(ctx context.Context, creds *models.Credentials) error {
	query, args, err := sq.Insert("credentials").
		Columns("id", "url", "username", "password").
		Values(1, creds.URL, creds.Username, creds.Password).
		Suffix("ON CONFLICT (id) DO UPDATE SET url = EXCLUDED.url, username = EXCLUDED.username, password = EXCLUDED.password").
		ToSql()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}
