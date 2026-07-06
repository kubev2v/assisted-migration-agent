package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

const (
	collectionTable = "agent.main.collections"

	colCollectionDatabase = `"database"`
	colCollectionState    = "state"
	colCollectionError    = "error"
)

var collectionSelectColumns = []string{
	colCollectionDatabase,
	colCollectionState,
	colCollectionError,
}

type CollectionStore struct {
	db QueryInterceptor
}

func NewCollectionStore(db QueryInterceptor) *CollectionStore {
	return &CollectionStore{db: db}
}

// List returns all collections matching filter. Pass nil to return all rows.
// Use ListOption helpers (WithOrderBy, WithOffset, WithLimit) for sorting and pagination.
func (s *CollectionStore) List(ctx context.Context) ([]models.Collection, error) {
	builder := sq.Select(collectionSelectColumns...).From(collectionTable)

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list collections query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying collections: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var cols []models.Collection
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning collection: %w", err)
		}
		cols = append(cols, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating collection rows: %w", err)
	}
	return cols, nil
}

// Create inserts a new collection row and returns the persisted record.
// The id is assigned by the DB sequence; do not set col.ID.
func (s *CollectionStore) Create(ctx context.Context, database string) (*models.Collection, error) {
	query, args, err := sq.Insert(collectionTable).
		Columns(
			colCollectionDatabase,
			colCollectionState,
		).
		Values(
			database,
			"running",
		).
		Suffix(fmt.Sprintf("RETURNING %s", strings.Join(collectionSelectColumns, ", "))).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building create collection query: %w", err)
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	return scanCollection(row)
}

// MarkFailed sets state=failed, records the error message, and sets finished_at=now().
// Only matches collections in running state (WHERE state = 'running').
func (s *CollectionStore) MarkFailed(ctx context.Context, database string, errMsg string) error {
	query, args, err := sq.Update(collectionTable).
		Set(colCollectionState, string(models.CollectionStateFailed)).
		Set(colCollectionError, errMsg).
		Where(sq.Eq{colCollectionDatabase: database, colCollectionState: string(models.CollectionStateRunning)}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building mark failed query: %w", err)
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("marking collection failed: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("collection %s not found or not in running state", database)
	}

	return nil
}

func (s *CollectionStore) Delete(ctx context.Context, database string) error {
	query, args, err := sq.Delete(collectionTable).
		Where(sq.Eq{colCollectionDatabase: database}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building delete collection query: %w", err)
	}

	_, err = s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("deleting collection: %w", err)
	}

	return nil
}

// scanCollection scans one row into a *models.Collection.
// Column order must match collectionSelectColumns exactly.
func scanCollection(row rowScanner) (*models.Collection, error) {
	var (
		c      models.Collection
		errMsg sql.NullString
	)
	err := row.Scan(
		&c.Database,
		&c.State,
		&errMsg,
	)
	if err != nil {
		return nil, err
	}
	c.Error = errMsg.String
	return &c, nil
}
