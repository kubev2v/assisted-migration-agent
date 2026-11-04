package migrations

import (
	"context"
	"fmt"
	"slices"
)

type SchemaKind string

const (
	MainSchema       SchemaKind = "main"
	CollectionSchema SchemaKind = "collection"
	manifestVersion             = 1
)

type ColumnState struct {
	Table    string `json:"table"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable string `json:"nullable"`
	Default  string `json:"default"`
}

type DatabaseState struct {
	Path     string        `json:"path"`
	Kind     SchemaKind    `json:"kind"`
	Versions []int         `json:"versions"`
	Columns  []ColumnState `json:"columns"`
}

type RegistryChecksums map[SchemaKind]string

type Manifest struct {
	Version        int               `json:"version"`
	AttemptID      string            `json:"attempt_id"`
	PreviousImage  string            `json:"previous_image"`
	CandidateImage string            `json:"candidate_image"`
	Registries     RegistryChecksums `json:"registries"`
	Databases      []DatabaseState   `json:"databases"`
}

type schemaObjects struct {
	Tables  map[string]bool
	Columns map[string]bool
}

func readState(ctx context.Context, db schemaQuerier, name string, kind SchemaKind) (DatabaseState, error) {
	state := DatabaseState{Path: name, Kind: kind}
	var err error
	state.Versions, err = appliedVersions(ctx, db, historyTable(kind))
	if err != nil {
		return state, err
	}
	rows, err := db.QueryContext(ctx, `SELECT table_name, column_name, data_type,
		is_nullable, coalesce(column_default, '') FROM information_schema.columns
		WHERE table_catalog = current_database() AND table_schema = 'main'
		ORDER BY table_name, ordinal_position`)
	if err != nil {
		return state, err
	}
	defer func() { _ = rows.Close() }()

	state.Columns = make([]ColumnState, 0)
	for rows.Next() {
		var column ColumnState
		if err := rows.Scan(&column.Table, &column.Name, &column.Type, &column.Nullable, &column.Default); err != nil {
			return state, err
		}
		state.Columns = append(state.Columns, column)
	}
	return state, rows.Err()
}

func (s DatabaseState) objects() schemaObjects {
	objects := schemaObjects{Tables: make(map[string]bool), Columns: make(map[string]bool)}
	for _, column := range s.Columns {
		objects.Tables[column.Table] = true
		objects.Columns[column.Table+"."+column.Name] = true
	}
	return objects
}

func (s DatabaseState) verify(current DatabaseState) error {
	if !slices.Equal(s.Versions, current.Versions) {
		return fmt.Errorf("%s: versions %v do not match baseline %v", s.Path, current.Versions, s.Versions)
	}
	if !slices.Equal(s.Columns, current.Columns) {
		return fmt.Errorf("%s: schema columns do not match baseline", s.Path)
	}
	return nil
}

func historyTable(kind SchemaKind) string {
	if kind == MainSchema {
		return `"main"."schema_migrations"`
	}
	return `"main"."collection_schema_migrations"`
}
