package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const (
	accessPasswordTable    = "agent.main.master_password"
	accessPasswordColumn   = "password"
	accessPasswordRecordID = 1
)

// AccessPasswordStore persists the agent-wide API access password hash.
type AccessPasswordStore struct {
	db QueryInterceptor
}

func NewAccessPasswordStore(db QueryInterceptor) *AccessPasswordStore {
	return &AccessPasswordStore{db: db}
}

func (s *AccessPasswordStore) Get(ctx context.Context) (string, error) {
	query, args, err := sq.Select(accessPasswordColumn).
		From(accessPasswordTable).
		Where(sq.Eq{"id": accessPasswordRecordID}).
		ToSql()
	if err != nil {
		return "", fmt.Errorf("building get access password query: %w", err)
	}

	var password string
	err = s.db.QueryRowContext(ctx, query, args...).Scan(&password)
	if errors.Is(err, sql.ErrNoRows) {
		return "", srvErrors.NewResourceNotFoundError("master_password", "")
	}
	if err != nil {
		return "", fmt.Errorf("scanning access password: %w", err)
	}

	return password, nil
}

// Create stores a password only when one does not already exist.
func (s *AccessPasswordStore) Create(ctx context.Context, password string) (bool, error) {
	query, args, err := sq.Insert(accessPasswordTable).
		Columns("id", accessPasswordColumn).
		Values(accessPasswordRecordID, password).
		Suffix("ON CONFLICT (id) DO NOTHING").
		ToSql()
	if err != nil {
		return false, fmt.Errorf("building create access password query: %w", err)
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("creating access password: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("reading access password create result: %w", err)
	}
	return rows == 1, nil
}

// Replace updates an existing password. It never creates a password row.
func (s *AccessPasswordStore) Replace(ctx context.Context, password string) error {
	query, args, err := sq.Update(accessPasswordTable).
		Set(accessPasswordColumn, password).
		Where(sq.Eq{"id": accessPasswordRecordID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building replace access password query: %w", err)
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("replacing access password: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("reading access password replace result: %w", err)
	}
	if rows == 0 {
		return srvErrors.NewResourceNotFoundError("master_password", "")
	}
	return nil
}
