package services

import (
	"context"
	"errors"
	"time"
)

var ErrCallsUnavailable = errors.New("calls are not available")

type Call struct {
	Number    string
	Masked    bool
	ExpiresAt *time.Time
}

type ICallBridge interface {
	Connect(ctx context.Context, fromPhone, toPhone, reference string) (*Call, error)
}
