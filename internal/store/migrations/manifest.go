package migrations

import (
	"fmt"
	"slices"
)

type SchemaKind string

const (
	MainSchema       SchemaKind = "main"
	CollectionSchema SchemaKind = "collection"
	manifestVersion             = 1
)

type DatabaseState struct {
	Path     string     `json:"path"`
	Kind     SchemaKind `json:"kind"`
	Versions []int      `json:"versions"`
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

func (s DatabaseState) verifyVersions(versions []int) error {
	if !slices.Equal(s.Versions, versions) {
		return fmt.Errorf("%s: versions %v do not match baseline %v", s.Path, versions, s.Versions)
	}
	return nil
}

func historyTable(kind SchemaKind) string {
	if kind == MainSchema {
		return `"main"."schema_migrations"`
	}
	return `"main"."collection_schema_migrations"`
}
