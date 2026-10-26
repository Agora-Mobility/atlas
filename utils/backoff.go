// Package utils holds small, dependency-free helpers.
package utils

import (
	"context"
	"time"
)

// Backoff returns an exponential delay for the given attempt (starting at 0),
// capped at maxDelay.
func Backoff(attempt int, base, maxDelay time.Duration) time.Duration {
	d := base
	for i := 0; i < attempt && d < maxDelay; i++ {
		d *= 2
	}
	if d > maxDelay {
		return maxDelay
	}
	return d
}

// Sleep waits for d or until ctx is done. It returns false if ctx ended first.
func Sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T {
	return &v
}
