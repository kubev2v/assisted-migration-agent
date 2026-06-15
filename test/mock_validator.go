package test

import (
	"context"

	"github.com/kubev2v/migration-planner-common/pkg/duckdb_parser"
	"github.com/kubev2v/migration-planner-common/pkg/duckdb_parser/models"
)

// MockValidator implements duckdb_parser.Validator for testing.
type MockValidator struct {
	Concerns []models.Concern
	Err      error
}

// Validate returns the configured concerns and error.
func (m *MockValidator) Validate(ctx context.Context, vm models.VM) ([]models.Concern, error) {
	return m.Concerns, m.Err
}

// NewMockValidator creates a new MockValidator that returns no concerns.
func NewMockValidator() *MockValidator {
	return &MockValidator{}
}

// Ensure MockValidator implements duckdb_parser.Validator.
var _ duckdb_parser.Validator = (*MockValidator)(nil)
