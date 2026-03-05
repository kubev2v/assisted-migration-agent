package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const (
	groupTable          = "groups"
	groupColID          = "id"
	groupColName        = "name"
	groupColDescription = "description"
	groupColFilter      = "filter"
	groupColCreatedAt   = "created_at"
	groupColUpdatedAt   = "updated_at"
)

var (
	selectStm = sq.Select(
		groupColID,
		groupColName,
		groupColDescription,
		groupColFilter,
		groupColCreatedAt,
		groupColUpdatedAt).
		From(groupTable)

	returningSuffix = fmt.Sprintf("RETURNING %s, %s, %s, %s, %s, %s",
		groupColID, groupColName, groupColDescription, groupColFilter, groupColCreatedAt, groupColUpdatedAt)
)

type GroupStore struct {
	db QueryInterceptor
}

func NewGroupStore(db QueryInterceptor) *GroupStore {
	return &GroupStore{db: db}
}

// List returns groups with optional filters and pagination.
func (s *GroupStore) List(ctx context.Context, filters []sq.Sqlizer, limit, offset uint64) ([]models.Group, error) {
	builder := selectStm.OrderBy(groupColID + " ASC")

	for _, f := range filters {
		builder = builder.Where(f)
	}
	if limit > 0 {
		builder = builder.Limit(limit)
	}
	if offset > 0 {
		builder = builder.Offset(offset)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("executing list query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var groups []models.Group
	for rows.Next() {
		var g models.Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.Filter, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning group row: %w", err)
		}
		groups = append(groups, g)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating group rows: %w", err)
	}

	return groups, nil
}

// Count returns the total number of groups matching the filters.
func (s *GroupStore) Count(ctx context.Context, filters ...sq.Sqlizer) (int, error) {
	builder := sq.Select("COUNT(*)").From(groupTable)

	for _, f := range filters {
		builder = builder.Where(f)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return 0, fmt.Errorf("building count query: %w", err)
	}

	var count int
	err = s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("executing count query: %w", err)
	}
	return count, nil
}

// Get returns a group by ID.
func (s *GroupStore) Get(ctx context.Context, id int) (*models.Group, error) {
	query, args, err := selectStm.Where(sq.Eq{groupColID: id}).ToSql()
	if err != nil {
		return nil, fmt.Errorf("building get query: %w", err)
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	var g models.Group
	err = row.Scan(&g.ID, &g.Name, &g.Description, &g.Filter, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, srvErrors.NewResourceNotFoundError("group", fmt.Sprintf("%d", id))
	}
	if err != nil {
		return nil, fmt.Errorf("scanning group: %w", err)
	}

	return &g, nil
}

// Create inserts a new group and returns it with the generated ID and timestamps.
func (s *GroupStore) Create(ctx context.Context, group models.Group) (*models.Group, error) {
	now := time.Now()

	query, args, err := sq.Insert(groupTable).
		Columns(groupColName, groupColDescription, groupColFilter, groupColCreatedAt, groupColUpdatedAt).
		Values(group.Name, group.Description, group.Filter, now, now).
		Suffix(returningSuffix).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building create query: %w", err)
	}

	row := s.db.QueryRowContext(ctx, query, args...)

	var g models.Group
	err = row.Scan(&g.ID, &g.Name, &g.Description, &g.Filter, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if isUniqueConstraintError(err) {
			return nil, srvErrors.NewDuplicateResourceError("group", "name", group.Name)
		}
		return nil, fmt.Errorf("creating group: %w", err)
	}

	return &g, nil
}

// Update updates an existing group by ID.
func (s *GroupStore) Update(ctx context.Context, id int, group models.Group) (*models.Group, error) {
	query, args, err := sq.Update(groupTable).
		Set(groupColName, group.Name).
		Set(groupColDescription, group.Description).
		Set(groupColFilter, group.Filter).
		Set(groupColUpdatedAt, time.Now()).
		Where(sq.Eq{groupColID: id}).
		Suffix(returningSuffix).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building update query: %w", err)
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	var g models.Group
	err = row.Scan(&g.ID, &g.Name, &g.Description, &g.Filter, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, srvErrors.NewResourceNotFoundError("group", fmt.Sprintf("%d", id))
		}
		if isUniqueConstraintError(err) {
			return nil, srvErrors.NewDuplicateResourceError("group", "name", group.Name)
		}
		return nil, fmt.Errorf("updating group: %w", err)
	}

	return &g, nil
}

// Delete removes a group by ID.
func (s *GroupStore) Delete(ctx context.Context, id int) error {
	query, args, err := sq.Delete(groupTable).
		Where(sq.Eq{groupColID: id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building delete query: %w", err)
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("executing delete: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("getting rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return srvErrors.NewResourceNotFoundError("group", fmt.Sprintf("%d", id))
	}

	return nil
}

func isUniqueConstraintError(err error) bool {
	return strings.Contains(err.Error(), "Constraint Error") &&
		strings.Contains(err.Error(), "Duplicate key")
}
