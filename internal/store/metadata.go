package store

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"
)

const (
	tagCategoriesTable   = "tag_categories"
	tagCategoriesColID   = "id"
	tagCategoriesColName = "name"
)

type MetadataStore struct {
	db QueryInterceptor
}

func NewMetadataStore(db QueryInterceptor) *MetadataStore {
	return &MetadataStore{db: db}
}

func (s *MetadataStore) ListUsedTagCategoryIDs(ctx context.Context, sqlitePath string) ([]string, error) {
	query, args, err := sq.Select("DISTINCT tag.value->>'categoryID'").
		Prefix("WITH inventory AS (SELECT Tags, IsTemplate FROM sqlite_scan(?, 'VM'))", sqlitePath).
		From("inventory vm, json_each(COALESCE(NULLIF(trim(vm.Tags), ''), '[]')) tag").
		Where("vm.IsTemplate = 0").
		Where("trim(COALESCE(tag.value->>'categoryID', '')) != ''").
		Where("trim(COALESCE(tag.value->>'name', '')) != ''").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building used tag category IDs query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("reading used tag category IDs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		err = rows.Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("scanning used tag category ID: %w", err)
		}
		ids = append(ids, id)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("reading used tag category IDs: %w", err)
	}
	return ids, nil
}

func (s *MetadataStore) SaveTagCategories(ctx context.Context, names map[string]string) error {
	if len(names) == 0 {
		return nil
	}

	builder := sq.Insert(tagCategoriesTable).Columns(tagCategoriesColID, tagCategoriesColName)
	for id, name := range names {
		builder = builder.Values(id, name)
	}
	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("building tag category names query: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("saving tag category names: %w", err)
	}
	return nil
}
