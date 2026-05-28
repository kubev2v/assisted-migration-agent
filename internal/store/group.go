package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/migration-planner/pkg/inventory"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const (
	groupTable            = "groups"
	groupColID            = "id"
	groupColName          = "name"
	groupColDescription   = "description"
	groupColFilter        = "filter"
	groupColInventoryData = "inventory_data"
	groupColCreatedAt     = "created_at"
	groupColUpdatedAt     = "updated_at"

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
		groupColInventoryData,
		groupColCreatedAt,
		groupColUpdatedAt).
		From(groupTable)

	returningSuffix = fmt.Sprintf("RETURNING %s, %s, %s, %s, %s, %s, %s",
		groupColID, groupColName, groupColDescription, groupColFilter, groupColInventoryData, groupColCreatedAt, groupColUpdatedAt)
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
		var inventoryData []byte
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.Filter, &inventoryData, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning group row: %w", err)
		}
		inv, err := unmarshalInventory(inventoryData)
		if err != nil {
			return nil, fmt.Errorf("unmarshaling inventory for group %d: %w", g.ID, err)
		}
		g.Inventory = inv
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
	var inventoryData []byte
	err = row.Scan(&g.ID, &g.Name, &g.Description, &g.Filter, &inventoryData, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, srvErrors.NewResourceNotFoundError("group", fmt.Sprintf("%d", id))
	}
	if err != nil {
		return nil, fmt.Errorf("scanning group: %w", err)
	}

	inv, err := unmarshalInventory(inventoryData)
	if err != nil {
		return nil, fmt.Errorf("unmarshaling inventory for group %d: %w", id, err)
	}
	g.Inventory = inv

	return &g, nil
}

// Create inserts a new group and returns it with the generated ID and timestamps.
func (s *GroupStore) Create(ctx context.Context, group models.Group) (*models.Group, error) {
	now := time.Now()

	inventoryData, err := marshalInventory(group.Inventory)
	if err != nil {
		return nil, fmt.Errorf("marshaling inventory: %w", err)
	}

	query, args, err := sq.Insert(groupTable).
		Columns(groupColName, groupColDescription, groupColFilter, groupColInventoryData, groupColCreatedAt, groupColUpdatedAt).
		Values(group.Name, group.Description, group.Filter, inventoryData, now, now).
		Suffix(returningSuffix).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building create query: %w", err)
	}

	row := s.db.QueryRowContext(ctx, query, args...)

	var g models.Group
	var returnedInventoryData []byte
	err = row.Scan(&g.ID, &g.Name, &g.Description, &g.Filter, &returnedInventoryData, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if isUniqueConstraintError(err) {
			return nil, srvErrors.NewDuplicateResourceError("group", "name", group.Name)
		}
		return nil, fmt.Errorf("creating group: %w", err)
	}

	inv, err := unmarshalInventory(returnedInventoryData)
	if err != nil {
		return nil, fmt.Errorf("unmarshaling returned inventory: %w", err)
	}
	g.Inventory = inv

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
	var inventoryData []byte
	err = row.Scan(&g.ID, &g.Name, &g.Description, &g.Filter, &inventoryData, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, srvErrors.NewResourceNotFoundError("group", fmt.Sprintf("%d", id))
		}
		if isUniqueConstraintError(err) {
			return nil, srvErrors.NewDuplicateResourceError("group", "name", group.Name)
		}
		return nil, fmt.Errorf("updating group: %w", err)
	}

	inv, err := unmarshalInventory(inventoryData)
	if err != nil {
		return nil, fmt.Errorf("unmarshaling inventory for updated group: %w", err)
	}
	g.Inventory = inv

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

// GetGroupsContainingVM returns all group IDs that contain the specified VM.
// It queries the group_matches table for groups whose vm_ids array contains the given VM ID.
func (s *GroupStore) GetGroupsContainingVM(ctx context.Context, vmID string) ([]int, error) {
	query, args, err := sq.Select(groupMatchesColGroupID).
		From(groupMatchesTable).
		Where(sq.Expr("list_contains("+groupMatchesColVMIDs+", ?)", vmID)).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying groups containing VM %s: %w", vmID, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			err = fmt.Errorf("closing rows: %w", closeErr)
		}
	}()

	var groupIDs []int
	for rows.Next() {
		var groupID int
		if err := rows.Scan(&groupID); err != nil {
			return nil, fmt.Errorf("scanning group ID: %w", err)
		}
		groupIDs = append(groupIDs, groupID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	return groupIDs, nil
}

// UpdateInventory updates the inventory_data for a group by ID.
func (s *GroupStore) UpdateInventory(ctx context.Context, id int, inv *inventory.Inventory) error {
	inventoryData, err := marshalInventory(inv)
	if err != nil {
		return fmt.Errorf("marshaling inventory: %w", err)
	}

	query, args, err := sq.Update(groupTable).
		Set(groupColInventoryData, inventoryData).
		Set(groupColUpdatedAt, time.Now()).
		Where(sq.Eq{groupColID: id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building update inventory query: %w", err)
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("updating inventory: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return srvErrors.NewResourceNotFoundError("group", fmt.Sprintf("%d", id))
	}

	return nil
}

// marshalInventory converts an inventory model to JSON bytes for DB storage.
// Stores the internal format (not API format).
func marshalInventory(inv *inventory.Inventory) ([]byte, error) {
	if inv == nil {
		return nil, nil
	}
	return json.Marshal(inv)
}

// unmarshalInventory converts JSON bytes from DB to inventory model.
func unmarshalInventory(data []byte) (*inventory.Inventory, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var inv inventory.Inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return nil, fmt.Errorf("unmarshaling inventory: %w", err)
	}
	return &inv, nil
}
