package store

import (
	"context"
	"database/sql"
	"sync"

	"go.uber.org/zap"
)

type queryInterceptor struct {
	db     *sql.DB
	logger *zap.SugaredLogger
	mu     sync.Mutex
}

func newQueryInterceptor(db *sql.DB) *queryInterceptor {
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
	q.mu.Lock()
	defer q.mu.Unlock()

	q.logger.Debugw("exec", "query", query, "args", args)

	// need to be aware of the transaction
	tx, ok := q.txFromContext(ctx)
	if ok {
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return result, err
		}
		return result, nil
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
