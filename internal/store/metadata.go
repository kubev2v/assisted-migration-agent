package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type MetadataStore struct {
	db QueryInterceptor
}

func NewMetadataStore(db QueryInterceptor) *MetadataStore {
	return &MetadataStore{db: db}
}

func (s *MetadataStore) SaveTagCategories(ctx context.Context, names map[string]string) error {
	if names == nil {
		names = map[string]string{}
	}
	data, err := json.Marshal(names)
	if err != nil {
		return fmt.Errorf("encoding tag category names: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `CREATE OR REPLACE TEMP TABLE tag_categories AS
		SELECT key AS id, value->>'$' AS name FROM json_each(?)`, string(data))
	if err != nil {
		return fmt.Errorf("saving tag category names: %w", err)
	}
	return nil
}

func metadataQuery(ctx context.Context, db QueryInterceptor, query string) (string, error) {
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_catalog = current_database() AND table_schema = 'main' AND table_name = 'vm_metadata'
    )`).Scan(&exists); err != nil {
		return "", fmt.Errorf("checking metadata table: %w", err)
	}
	if !exists {
		query = strings.ReplaceAll(query, "vm_metadata", "(SELECT NULL::VARCHAR AS vm_id, NULL::VARCHAR AS key, NULL::VARCHAR AS value WHERE false)")
	}
	return query, nil
}
