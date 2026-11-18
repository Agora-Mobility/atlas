//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/shared/txhook"
)

func TestAfterCommitHooksRunOnlyOnCommit(t *testing.T) {
	url := startPostgres(t)
	tx := postgres.NewTxManager(connect(t, url))
	ctx := context.Background()

	var ran []string
	require.NoError(t, tx.WithinTransaction(ctx, func(ctx context.Context) error {
		txhook.AfterCommit(ctx, func() { ran = append(ran, "outer") })
		// A nested call joins the outer transaction and its queue.
		return tx.WithinTransaction(ctx, func(ctx context.Context) error {
			txhook.AfterCommit(ctx, func() { ran = append(ran, "nested") })
			assert.Empty(t, ran, "nothing runs before the commit")
			return nil
		})
	}))
	assert.Equal(t, []string{"outer", "nested"}, ran)

	ran = nil
	err := tx.WithinTransaction(ctx, func(ctx context.Context) error {
		txhook.AfterCommit(ctx, func() { ran = append(ran, "rolled back") })
		return errors.New("fail")
	})
	require.Error(t, err)
	assert.Empty(t, ran, "a rollback drops the queue")
}
