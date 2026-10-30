package services

import (
	"context"
	"errors"
)

var (
	ErrSMSUnavailable   = errors.New("sms provider unavailable")
	ErrEmailUnavailable = errors.New("email provider unavailable")
)

type ISMSSender interface {
	Send(ctx context.Context, phone, message string) error
}

type Email struct {
	To             string
	Subject        string
	HTML           string
	Text           string
	IdempotencyKey string
}

type IEmailSender interface {
	Send(ctx context.Context, email Email) error
}
