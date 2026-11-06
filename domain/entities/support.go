package entities

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	TicketOpen     = "open"
	TicketAnswered = "answered"
	TicketClosed   = "closed"

	TicketSenderUser  = "user"
	TicketSenderStaff = "staff"

	maxTicketSubject = 160
	maxTicketBody    = 4000
)

var (
	ErrTicketNotFound = errors.New("ticket not found")
	ErrTicketClosed   = errors.New("this ticket is closed")
)

type SupportTicket struct {
	ID        uuid.UUID
	Code      string
	UserID    uuid.UUID
	RequestID *uuid.UUID
	TopicID   *uuid.UUID
	Subject   string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type SupportMessage struct {
	TicketID   uuid.UUID
	SenderType string
	SenderID   uuid.UUID
	Body       string
	CreatedAt  time.Time
}

func NormalizeTicketText(subject, body string) (string, string, error) {
	subject, body = strings.TrimSpace(subject), strings.TrimSpace(body)
	if subject == "" || len([]rune(subject)) > maxTicketSubject {
		return "", "", validationError("subject", "O assunto é obrigatório e tem no máximo 160 caracteres.")
	}
	if body == "" || len([]rune(body)) > maxTicketBody {
		return "", "", validationError("message", "A mensagem é obrigatória e tem no máximo 4000 caracteres.")
	}
	return subject, body, nil
}
