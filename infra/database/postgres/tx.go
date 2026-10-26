package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type txKey struct{}

// TxManager implements repository.ITransactionManager. The open transaction
// travels in the context, so every repository called inside the callback
// joins it without knowing about pgx.
type TxManager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

// WithinTransaction runs fn in a transaction, committing when fn returns nil
// and rolling back otherwise. Nested calls reuse the outer transaction.
func (m *TxManager) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}

	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
		if err != nil {
			if rbErr := tx.Rollback(context.WithoutCancel(ctx)); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				err = errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
			}
		}
	}()

	// err is the named result read by the deferred rollback; do not use :=.
	if err = fn(context.WithValue(ctx, txKey{}, tx)); err != nil { //nolint:gocritic // see above
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// Queries returns SQLC queries bound to the transaction in ctx, or to the
// pool when no transaction is open.
func Queries(ctx context.Context, pool *pgxpool.Pool) *gen.Queries {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return gen.New(tx)
	}
	return gen.New(pool)
}
