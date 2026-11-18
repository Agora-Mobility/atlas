// Package txhook defers work until the transaction in the context commits.
//
// Telemetry about a state change (a metric, a log line) must describe what
// was stored, not what was attempted: a counter incremented inside a
// transaction that later rolls back reports an event that never happened.
// AfterCommit queues a function on the open transaction; the transaction
// manager runs the queue after a successful commit and drops it on rollback.
package txhook

import (
	"context"
	"sync"
)

type hooksKey struct{}

// Hooks is the queue of one transaction.
type Hooks struct {
	mu  sync.Mutex
	fns []func()
}

// WithHooks returns a context that collects AfterCommit functions in a new
// queue. The transaction manager calls it when it opens a transaction.
func WithHooks(ctx context.Context) (context.Context, *Hooks) {
	h := &Hooks{}
	return context.WithValue(ctx, hooksKey{}, h), h
}

// AfterCommit runs fn once the transaction in ctx commits. Without a
// transaction (or in tests that fake one) it runs fn immediately, because
// the change is already stored.
func AfterCommit(ctx context.Context, fn func()) {
	if h, ok := ctx.Value(hooksKey{}).(*Hooks); ok && h != nil {
		h.mu.Lock()
		h.fns = append(h.fns, fn)
		h.mu.Unlock()
		return
	}
	safeRun(fn)
}

// Run executes the queued functions in order. A panic in one of them is
// swallowed so that it cannot fail an operation that has already committed.
func (h *Hooks) Run() {
	h.mu.Lock()
	fns := h.fns
	h.fns = nil
	h.mu.Unlock()
	for _, fn := range fns {
		safeRun(fn)
	}
}

func safeRun(fn func()) {
	defer func() { _ = recover() }()
	fn()
}
