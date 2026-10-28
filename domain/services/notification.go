package services

import (
	"context"
	"errors"
)

var ErrSMSUnavailable = errors.New("sms provider unavailable")

type ISMSSender interface {
	Send(ctx context.Context, phone, message string) error
}
