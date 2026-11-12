package services

import (
	"context"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/filter"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type ApplianceService struct {
	store *store.Store
}

func NewApplianceService(st *store.Store) (*ApplianceService, error) {
	return &ApplianceService{store: st}, nil
}

// List returns appliances detected from vApp product metadata, optionally
// narrowed by a filter expression.
func (s *ApplianceService) List(ctx context.Context, byExpression string) ([]models.Appliance, error) {
	var expression sq.Sqlizer
	if byExpression != "" {
		parsed, err := filter.ParseWithApplianceMap([]byte(byExpression))
		if err != nil {
			return nil, errors.NewInvalidFilterError(byExpression, err.Error())
		}
		expression = parsed
	}

	return s.store.Appliance().List(ctx, expression)
}
