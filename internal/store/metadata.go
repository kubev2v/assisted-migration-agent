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
