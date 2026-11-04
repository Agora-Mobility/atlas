package entities

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	maxMessageBody = 1000
	maxSOSNote     = 500

	SOSOpen     = "open"
	SOSResolved = "resolved"
)

var (
	ErrChatClosed        = errors.New("the chat only works while the request has a provider and is not finished")
	ErrShareNotAvailable = errors.New("only an active request can be shared")
	ErrShareNotFound     = errors.New("this link does not exist or expired")
	ErrSOSNotFound       = errors.New("alert not found")
	ErrNoETA             = errors.New("there is no position to calculate the arrival")
	ErrCallNotAvailable  = errors.New("a call needs an active request with a provider")
)

func (s RequestStatus) HasLiveProvider() bool {
	return s == RequestAccepted || s == RequestProviderEnRoute || s == RequestInProgress
}

type RequestMessage struct {
	ID           uuid.UUID
	RequestID    uuid.UUID
	SenderID     uuid.UUID
	SenderType   string
	Body         string
	QuickReplyID *uuid.UUID
	ReadAt       *time.Time
	CreatedAt    time.Time
}

func NormalizeMessage(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" || len([]rune(body)) > maxMessageBody {
		return "", validationError("body", "A mensagem é obrigatória e tem no máximo 1000 caracteres.")
	}
	return body, nil
}

type SOSAlert struct {
	ID         uuid.UUID
	RequestID  uuid.UUID
	UserID     uuid.UUID
	Point      *GeoPoint
	Note       string
	Status     string
	ResolvedAt *time.Time
	CreatedAt  time.Time
}

func (a *SOSAlert) Normalize() error {
	a.Note = strings.TrimSpace(a.Note)
	if len([]rune(a.Note)) > maxSOSNote {
		return validationError("note", "A nota tem no máximo 500 caracteres.")
	}
	if a.Point != nil {
		return a.Point.Validate("position")
	}
	return nil
}
