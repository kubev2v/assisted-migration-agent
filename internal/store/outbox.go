package store

import (
	"context"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

type OutboxStore struct {
	db QueryInterceptor
}

func NewOutboxStore(db QueryInterceptor) *OutboxStore {
	return &OutboxStore{db: db}
}

func (s *OutboxStore) Get(ctx context.Context) ([]models.Event, error) {
	query, args, err := sq.Select("id", "event_type", "payload").
		From("outbox").
		OrderBy("id ASC").
		ToSql()
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var events []models.Event
	for rows.Next() {
		var event models.Event
		if err := rows.Scan(&event.ID, &event.Kind, &event.Data); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return events, nil
}

func (s *OutboxStore) Insert(ctx context.Context, event models.Event) error {
	query, args, err := sq.Insert("outbox").
		Columns("event_type", "payload").
		Values(event.Kind, event.Data).
		ToSql()
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *OutboxStore) Delete(ctx context.Context, maxID int) error {
	query, args, err := sq.Delete("outbox").Where("id <= ?", maxID).ToSql()
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}
