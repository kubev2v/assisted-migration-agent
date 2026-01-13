package store

import (
	"context"
	"database/sql"
	"errors"

	sq "github.com/Masterminds/squirrel"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type ConfigurationStore struct {
	db QueryInterceptor
}

func NewConfigurationStore(db QueryInterceptor) *ConfigurationStore {
	return &ConfigurationStore{db: db}
}

func (s *ConfigurationStore) Get(ctx context.Context) (*models.Configuration, error) {
	query, args, err := sq.Select("agent_mode").
		From("configuration").
		Where(sq.Eq{"id": 1}).
		ToSql()
	if err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	var agentMode string
	err = row.Scan(&agentMode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, srvErrors.NewConfigurationNotFoundError()
	}
	if err != nil {
		return nil, err
	}
	return &models.Configuration{
		AgentMode: models.AgentMode(agentMode),
	}, nil
}

func (s *ConfigurationStore) Save(ctx context.Context, cfg *models.Configuration) error {
	query, args, err := sq.Insert("configuration").
		Columns("id", "agent_mode").
		Values(1, string(cfg.AgentMode)).
		Suffix("ON CONFLICT (id) DO UPDATE SET agent_mode = EXCLUDED.agent_mode").
		ToSql()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}
