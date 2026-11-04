package services

import (
	"context"
	"errors"
	"time"
)

type MessageClass string

const (
	ClassCritical  MessageClass = "critical"
	ClassState     MessageClass = "state"
	ClassEphemeral MessageClass = "ephemeral"
	ClassControl   MessageClass = "control"
)

var ErrMailboxCursorLost = errors.New("the cursor is older than the mailbox")

type MailboxMessage struct {
	EventID   string
	Type      string
	Class     MessageClass
	Data      []byte
	ExpiresAt *time.Time
}

type MailboxEntry struct {
	ID string
	MailboxMessage
}

type IMailbox interface {
	Deliver(ctx context.Context, userID string, message MailboxMessage) error
	Read(ctx context.Context, userID, afterID string, count int64) ([]MailboxEntry, error)
	CheckCursor(ctx context.Context, userID, cursor string) error
}

type IPositionFeed interface {
	PublishPosition(ctx context.Context, userID, requestID string, payload []byte) error
	LastPosition(ctx context.Context, requestID string) ([]byte, error)
}
