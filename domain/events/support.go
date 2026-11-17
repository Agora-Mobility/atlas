package events

import (
	"time"

	"github.com/google/uuid"
)

const (
	SupportTicketAggregate = "support_ticket"

	TicketOpened = "support.ticket-opened"
)

type TicketOpenedV1 struct {
	TicketID   uuid.UUID  `json:"ticket_id"`
	Code       string     `json:"code"`
	UserID     uuid.UUID  `json:"user_id"`
	RequestID  *uuid.UUID `json:"request_id,omitempty"`
	Subject    string     `json:"subject"`
	OccurredAt time.Time  `json:"occurred_at"`
}

func (TicketOpenedV1) EventType() string     { return TicketOpened }
func (TicketOpenedV1) EventVersion() int     { return 1 }
func (TicketOpenedV1) AggregateType() string { return SupportTicketAggregate }
func (e TicketOpenedV1) AggregateID() string { return e.TicketID.String() }
