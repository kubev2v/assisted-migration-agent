package store

import (
	"context"
	"database/sql"
	"errors"
)

type contextKey int

const txKey contextKey = 0

type Transactor interface {
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type dbTransactor struct {
	db *sql.DB
}

func NewTransactor(db *sql.DB) Transactor {
	return &dbTransactor{db: db}
}

func (t *dbTransactor) WithTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if ctx.Value(txKey) != nil {
		return errors.New("nested transactions not supported")
	}

	tx, err := t.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}

	var committed bool
	defer func() {
		if p := recover(); p != nil {
			if !committed {
				if rbErr := tx.Rollback(); rbErr != nil {
					err = errors.Join(errors.New("panic during transaction"), rbErr)
				} else {
					err = errors.New("panic during transaction")
				}
			}
			panic(p)
		} else if err != nil && !committed {
			if rbErr := tx.Rollback(); rbErr != nil {
				err = errors.Join(err, rbErr)
			}
		}
	}()

	txContext := context.WithValue(ctx, txKey, tx)

	if err = fn(txContext); err != nil {
		return err
	}

	committed = true
	if err = tx.Commit(); err != nil {
		return err
	}

	return nil
}
