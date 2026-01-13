package store

import (
	"context"
	"database/sql"

	"go.uber.org/zap"
)

type queryInterceptor struct {
	db     *sql.DB
	logger *zap.SugaredLogger
}

func newQueryInterceptor(db *sql.DB) *queryInterceptor {
	return &queryInterceptor{
		db:     db,
		logger: zap.S().Named("store"),
	}
}

func (q *queryInterceptor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	q.logger.Debugw("query_row", "query", query, "args", args)
	return q.db.QueryRowContext(ctx, query, args...)
}

func (q *queryInterceptor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q.logger.Debugw("query", "query", query, "args", args)
	return q.db.QueryContext(ctx, query, args...)
}

func (q *queryInterceptor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	q.logger.Debugw("exec", "query", query, "args", args)
	return q.db.ExecContext(ctx, query, args...)
}
