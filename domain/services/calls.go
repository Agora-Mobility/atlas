package services

import (
	"context"
	"errors"
	"time"
)

var ErrCallsUnavailable = errors.New("masked calls are not available")

type MaskedCall struct {
	ProxyNumber string
	ExpiresAt   *time.Time
}

type ICallBridge interface {
	Enabled() bool
	Connect(ctx context.Context, fromPhone, toPhone, reference string) (*MaskedCall, error)
}
