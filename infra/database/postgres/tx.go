package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
	"github.com/Agora-Mobility/atlas/shared/tenant"
	"github.com/Agora-Mobility/atlas/shared/txhook"
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

	// Functions queued with txhook.AfterCommit run only once the commit
	// succeeds; a rollback drops them.
	txCtx, hooks := txhook.WithHooks(context.WithValue(ctx, txKey{}, tx))

	// err is the named result read by the deferred rollback; do not use :=.
	if err = fn(txCtx); err != nil { //nolint:gocritic // see above
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	hooks.Run()
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

// Scoped runs fn on the transaction in ctx, or on a new one, after setting
// the tenant of ctx (organization and user) for row-level security. The
// settings are local to the transaction, so a pooled connection never keeps
// the tenant of a previous request.
func Scoped(ctx context.Context, pool *pgxpool.Pool, fn func(ctx context.Context) error) error {
	return NewTxManager(pool).WithinTransaction(ctx, func(ctx context.Context) error {
		tx, ok := ctx.Value(txKey{}).(pgx.Tx)
		if !ok {
			return errors.New("scoped query without a transaction")
		}
		organizationID, userID := tenant.From(ctx).Settings()
		if _, err := tx.Exec(ctx, "SELECT set_config('atlas.organization_id', $1, true), set_config('atlas.user_id', $2, true)", organizationID, userID); err != nil {
			return fmt.Errorf("set tenant: %w", err)
		}
		return fn(ctx)
	})
}
