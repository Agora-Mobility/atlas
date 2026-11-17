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

var (
	ErrPushTokenInvalid = errors.New("push token is no longer valid")
	ErrPushRejected     = errors.New("push rejected for good")
)

type PushMessage struct {
	Token        string
	Title        string
	Body         string
	Data         map[string]string
	HighPriority bool
	TTLSeconds   int
}

type IPushSender interface {
	Send(ctx context.Context, message PushMessage) (string, error)
}
