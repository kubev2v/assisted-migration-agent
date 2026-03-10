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
	groupColTags        = "tags"
	groupColCreatedAt   = "created_at"
	groupColUpdatedAt   = "updated_at"

	groupMatchesTable      = "group_matches"
	groupMatchesColGroupID = "group_id"
	groupMatchesColVMIDs   = "vm_ids"
)

var (
	selectStm = sq.Select(
		groupColID,
		groupColName,
		groupColDescription,
		groupColFilter,
		groupColTags,
		groupColCreatedAt,
		groupColUpdatedAt).
		From(groupTable)

	returningSuffix = fmt.Sprintf("RETURNING %s, %s, %s, %s, %s, %s, %s",
		groupColID, groupColName, groupColDescription, groupColFilter, groupColTags, groupColCreatedAt, groupColUpdatedAt)
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
		var tags StringArray
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.Filter, &tags, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning group row: %w", err)
		}
		g.Tags = tags
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
	var tags StringArray
	err = row.Scan(&g.ID, &g.Name, &g.Description, &g.Filter, &tags, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, srvErrors.NewResourceNotFoundError("group", fmt.Sprintf("%d", id))
	}
	if err != nil {
		return nil, fmt.Errorf("scanning group: %w", err)
	}
	g.Tags = tags

	return &g, nil
}

// Create inserts a new group and returns it with the generated ID and timestamps.
func (s *GroupStore) Create(ctx context.Context, group models.Group) (*models.Group, error) {
	now := time.Now()

	tags := group.Tags
	if tags == nil {
		tags = []string{}
	}

	query, args, err := sq.Insert(groupTable).
		Columns(groupColName, groupColDescription, groupColFilter, groupColTags, groupColCreatedAt, groupColUpdatedAt).
		Values(group.Name, group.Description, group.Filter, tags, now, now).
		Suffix(returningSuffix).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building create query: %w", err)
	}

	row := s.db.QueryRowContext(ctx, query, args...)

	var g models.Group
	var scannedTags StringArray
	err = row.Scan(&g.ID, &g.Name, &g.Description, &g.Filter, &scannedTags, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if isUniqueConstraintError(err) {
			return nil, srvErrors.NewDuplicateResourceError("group", "name", group.Name)
		}
		return nil, fmt.Errorf("creating group: %w", err)
	}
	g.Tags = scannedTags

	return &g, nil
}

// Update updates an existing group by ID.
func (s *GroupStore) Update(ctx context.Context, id int, group models.Group) (*models.Group, error) {
	tags := group.Tags
	if tags == nil {
		tags = []string{}
	}

	query, args, err := sq.Update(groupTable).
		Set(groupColName, group.Name).
		Set(groupColDescription, group.Description).
		Set(groupColFilter, group.Filter).
		Set(groupColTags, tags).
		Set(groupColUpdatedAt, time.Now()).
		Where(sq.Eq{groupColID: id}).
		Suffix(returningSuffix).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building update query: %w", err)
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	var g models.Group
	var scannedTags StringArray
	err = row.Scan(&g.ID, &g.Name, &g.Description, &g.Filter, &scannedTags, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, srvErrors.NewResourceNotFoundError("group", fmt.Sprintf("%d", id))
		}
		if isUniqueConstraintError(err) {
			return nil, srvErrors.NewDuplicateResourceError("group", "name", group.Name)
		}
		return nil, fmt.Errorf("updating group: %w", err)
	}
	g.Tags = scannedTags

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

// RefreshMatches rebuilds group_matches rows by evaluating each group's filter
// against the VM data. When groupIDs are provided, only those groups are
// refreshed. When none are provided, all groups are refreshed.
func (s *GroupStore) RefreshMatches(ctx context.Context, groupIDs ...int) error {
	var groups []models.Group

	if len(groupIDs) == 0 {
		var err error
		groups, err = s.List(ctx, nil, 0, 0)
		if err != nil {
			return fmt.Errorf("fetching groups: %w", err)
		}

		delQuery, _, err := sq.Delete(groupMatchesTable).ToSql()
		if err != nil {
			return fmt.Errorf("building delete query: %w", err)
		}
		if _, err := s.db.ExecContext(ctx, delQuery); err != nil {
			return fmt.Errorf("clearing group_matches: %w", err)
		}
	} else {
		for _, id := range groupIDs {
			g, err := s.Get(ctx, id)
			if err != nil {
				return fmt.Errorf("fetching group %d: %w", id, err)
			}
			groups = append(groups, *g)
		}

		delQuery, delArgs, err := sq.Delete(groupMatchesTable).
			Where(sq.Eq{groupMatchesColGroupID: groupIDs}).
			ToSql()
		if err != nil {
			return fmt.Errorf("building delete query: %w", err)
		}
		if _, err := s.db.ExecContext(ctx, delQuery, delArgs...); err != nil {
			return fmt.Errorf("clearing group_matches for ids: %w", err)
		}
	}

	for _, g := range groups {
		filterSQL := ByFilter(g.Filter)
		if filterSQL == nil {
			continue
		}

		subquery := vmFilterSubquery.Where(filterSQL)
		subSQL, subArgs, err := subquery.ToSql()
		if err != nil {
			return fmt.Errorf("building filter query for group %d: %w", g.ID, err)
		}

		insertQuery, insertArgs, err := sq.Insert(groupMatchesTable).
			Columns(groupMatchesColGroupID, groupMatchesColVMIDs).
			Values(g.ID, sq.Expr(fmt.Sprintf(`(SELECT list("VM ID") FROM (%s))`, subSQL), subArgs...)).
			ToSql()
		if err != nil {
			return fmt.Errorf("building insert query for group %d: %w", g.ID, err)
		}

		if _, err := s.db.ExecContext(ctx, insertQuery, insertArgs...); err != nil {
			return fmt.Errorf("inserting matches for group %d: %w", g.ID, err)
		}
	}

	return nil
}

// DeleteMatches removes the group_matches row for a given group ID.
func (s *GroupStore) DeleteMatches(ctx context.Context, groupID int) error {
	query, args, err := sq.Delete(groupMatchesTable).
		Where(sq.Eq{groupMatchesColGroupID: groupID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building delete query: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("deleting matches for group %d: %w", groupID, err)
	}
	return nil
}

// GetMatchedIDs returns the pre-computed VM IDs for a group.
func (s *GroupStore) GetMatchedIDs(ctx context.Context, groupID int) ([]string, error) {
	query, args, err := sq.Select(fmt.Sprintf("COALESCE(%s, [])", groupMatchesColVMIDs)).
		From(groupMatchesTable).
		Where(sq.Eq{groupMatchesColGroupID: groupID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building query: %w", err)
	}

	var vmIDs StringArray
	err = s.db.QueryRowContext(ctx, query, args...).Scan(&vmIDs)
	if errors.Is(err, sql.ErrNoRows) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching matched VM IDs for group %d: %w", groupID, err)
	}
	return vmIDs, nil
}
