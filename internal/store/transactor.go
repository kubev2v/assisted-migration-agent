package store

import (
	"context"
	"database/sql"
	"errors"
)

type txKeyT int

var txKey txKeyT = 0

type Transactor interface {
	WithTx(context.Context, func(context.Context) error) error
}

type DBTransactor struct {
	db *sql.DB
}

func newTransactor(db *sql.DB) *DBTransactor {
	return &DBTransactor{
		db: db,
	}
}

func (t *DBTransactor) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if ctx.Value(txKey) != nil {
		return errors.New("nested transactions not supported")
	}

	tx, err := t.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}

	txContext := context.WithValue(ctx, txKey, tx)

	if err := fn(txContext); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}
