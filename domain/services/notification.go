package services

import (
	"context"
	"errors"
	"time"
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

const (
	LiveActivityUpdate = "update"
	LiveActivityEnd    = "end"
)

// LiveActivityMessage updates or ends an iOS Live Activity through APNs. ContentState
// is encoded as the "content-state" of the activity and has to match the
// ContentState of the app's ActivityAttributes.
type LiveActivityMessage struct {
	Token        string
	Event        string
	ContentState any
	Timestamp    time.Time
	StaleAt      *time.Time
	DismissAt    *time.Time
	AlertTitle   string
	AlertBody    string
	HighPriority bool
	TTLSeconds   int
}

type ILiveActivitySender interface {
	Send(ctx context.Context, message LiveActivityMessage) (string, error)
}
