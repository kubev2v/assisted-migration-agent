package store

import (
	"context"
	"database/sql"

	"go.uber.org/zap"
)

// QueryInterceptor provides database query methods with transaction awareness.
// Implementations route queries through an active transaction if present in context.
//
// The default implementation is DuckDB-specific: it issues FORCE CHECKPOINT
// after non-transactional writes to flush the WAL to the main file.
type QueryInterceptor interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type queryInterceptor struct {
	db     *sql.DB
	logger *zap.SugaredLogger
}

func NewQueryInterceptor(db *sql.DB) QueryInterceptor {
	return &queryInterceptor{
		db:     db,
		logger: zap.S().Named("store"),
	}
}

func (q *queryInterceptor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	q.logger.Debugw("query_row", "query", query, "args", args)

	tx, ok := q.txFromContext(ctx)
	if ok {
		return tx.QueryRowContext(ctx, query, args...)
	}

	return q.db.QueryRowContext(ctx, query, args...)
}

func (q *queryInterceptor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q.logger.Debugw("query", "query", query, "args", args)

	tx, ok := q.txFromContext(ctx)
	if ok {
		return tx.QueryContext(ctx, query, args...)
	}

	return q.db.QueryContext(ctx, query, args...)
}

func (q *queryInterceptor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	q.logger.Debugw("exec", "query", query, "args", args)

	tx, ok := q.txFromContext(ctx)
	if ok {
		return tx.ExecContext(ctx, query, args...)
	}

	result, err := q.db.ExecContext(ctx, query, args...)
	if err != nil {
		return result, err
	}

	if _, cpErr := q.db.ExecContext(ctx, "FORCE CHECKPOINT"); cpErr != nil {
		q.logger.Warnw("checkpoint failed", "error", cpErr)
	}
	return result, nil
}

func (q *queryInterceptor) txFromContext(ctx context.Context) (*sql.Tx, bool) {
	tx, ok := ctx.Value(txKey).(*sql.Tx)
	return tx, ok
}
