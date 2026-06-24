package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const (
	credentialsTable       = "credentials"
	credentialsColID       = "id"
	credentialsColURL      = "url"
	credentialsColUsername = "username"
	credentialsColPassword = "password"
	credentialsColSkipTLS  = "skip_tls"
	credentialsColCACert   = "ca_cert"

	masterPasswordTable       = "master_password"
	masterPasswordColPassword = "password"
)

type CredentialsStore struct {
	db QueryInterceptor
}

func NewCredentialsStore(db QueryInterceptor) *CredentialsStore {
	return &CredentialsStore{db: db}
}

func (s *CredentialsStore) List(ctx context.Context) ([]string, error) {
	query, args, err := sq.Select(credentialsColID).
		From(credentialsTable).
		OrderBy(credentialsColID + " ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("executing list query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning credential id: %w", err)
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

func (s *CredentialsStore) Get(ctx context.Context, id string) (models.Credentials, error) {
	query, args, err := sq.Select(credentialsColURL, credentialsColUsername, credentialsColPassword, credentialsColSkipTLS, credentialsColCACert).
		From(credentialsTable).
		Where(sq.Eq{credentialsColID: id}).
		ToSql()
	if err != nil {
		return models.Credentials{}, fmt.Errorf("building get query: %w", err)
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	var creds models.Credentials
	var caCert string
	err = row.Scan(&creds.URL, &creds.Username, &creds.Password, &creds.SkipTLS, &caCert)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Credentials{}, srvErrors.NewCredentialsNotFoundError(id)
	}
	if err != nil {
		return models.Credentials{}, fmt.Errorf("scanning credentials: %w", err)
	}
	if caCert != "" {
		creds.CACert = []byte(caCert)
	}

	return creds, nil
}

func (s *CredentialsStore) Save(ctx context.Context, id string, creds models.Credentials) error {
	query, args, err := sq.Insert(credentialsTable).
		Columns(credentialsColID, credentialsColURL, credentialsColUsername, credentialsColPassword, credentialsColSkipTLS, credentialsColCACert).
		Values(id, creds.URL, creds.Username, creds.Password, creds.SkipTLS, string(creds.CACert)).
		Suffix("ON CONFLICT (id) DO UPDATE SET url = EXCLUDED.url, username = EXCLUDED.username, password = EXCLUDED.password, skip_tls = EXCLUDED.skip_tls, ca_cert = EXCLUDED.ca_cert").
		ToSql()
	if err != nil {
		return fmt.Errorf("building save query: %w", err)
	}

	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *CredentialsStore) GetPassword(ctx context.Context) (string, error) {
	query, args, err := sq.Select(masterPasswordColPassword).
		From(masterPasswordTable).
		Where(sq.Eq{"id": 1}).
		ToSql()
	if err != nil {
		return "", fmt.Errorf("building get query: %w", err)
	}

	var password string
	err = s.db.QueryRowContext(ctx, query, args...).Scan(&password)
	if errors.Is(err, sql.ErrNoRows) {
		return "", srvErrors.NewResourceNotFoundError("master_password", "")
	}
	if err != nil {
		return "", fmt.Errorf("scanning master password: %w", err)
	}

	return password, nil
}

func (s *CredentialsStore) SavePassword(ctx context.Context, password string) error {
	query, args, err := sq.Insert(masterPasswordTable).
		Columns("id", masterPasswordColPassword).
		Values(1, password).
		Suffix("ON CONFLICT (id) DO UPDATE SET password = EXCLUDED.password").
		ToSql()
	if err != nil {
		return fmt.Errorf("building save query: %w", err)
	}

	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *CredentialsStore) Delete(ctx context.Context, id string) error {
	query, args, err := sq.Delete(credentialsTable).
		Where(sq.Eq{credentialsColID: id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building delete query: %w", err)
	}

	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *CredentialsStore) DeleteAll(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM "+credentialsTable)
	return err
}
