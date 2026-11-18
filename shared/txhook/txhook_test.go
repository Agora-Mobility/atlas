package txhook_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Agora-Mobility/atlas/shared/txhook"
)

func TestAfterCommitWithoutTransactionRunsNow(t *testing.T) {
	ran := false
	txhook.AfterCommit(context.Background(), func() { ran = true })
	assert.True(t, ran)
}

func TestAfterCommitWaitsForRun(t *testing.T) {
	ctx, hooks := txhook.WithHooks(context.Background())
	var order []int
	txhook.AfterCommit(ctx, func() { order = append(order, 1) })
	txhook.AfterCommit(ctx, func() { order = append(order, 2) })
	assert.Empty(t, order)

	hooks.Run()
	assert.Equal(t, []int{1, 2}, order)

	hooks.Run()
	assert.Equal(t, []int{1, 2}, order, "a queue runs once")
}

func TestRunSurvivesAPanickingHook(t *testing.T) {
	ctx, hooks := txhook.WithHooks(context.Background())
	ran := false
	txhook.AfterCommit(ctx, func() { panic("boom") })
	txhook.AfterCommit(ctx, func() { ran = true })

	assert.NotPanics(t, hooks.Run)
	assert.True(t, ran)
}

func TestAfterCommitWithoutTransactionSurvivesPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		txhook.AfterCommit(context.Background(), func() { panic("boom") })
	})
}
