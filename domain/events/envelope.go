// Package events defines the domain event contracts and the envelope every
// event travels in, from the outbox table to RabbitMQ consumers.
package events

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Event is implemented by every domain event payload.
type Event interface {
	// EventType is "<module>.<event>", e.g. "example.created".
	EventType() string
	EventVersion() int
	AggregateType() string
	AggregateID() string
}

// Envelope is the wire format of every event.
type Envelope struct {
	EventID       uuid.UUID       `json:"event_id"`
	EventType     string          `json:"event_type"`
	Version       int             `json:"version"`
	OccurredAt    time.Time       `json:"occurred_at"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	Payload       json.RawMessage `json:"payload"`
}

// NewEnvelope wraps evt with a fresh event id and timestamp.
func NewEnvelope(evt Event, correlationID string) (Envelope, error) {
	payload, err := json.Marshal(evt)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal %s payload: %w", evt.EventType(), err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Envelope{}, fmt.Errorf("generate event id: %w", err)
	}
	return Envelope{
		EventID:       id,
		EventType:     evt.EventType(),
		Version:       evt.EventVersion(),
		OccurredAt:    time.Now().UTC(),
		CorrelationID: correlationID,
		AggregateType: evt.AggregateType(),
		AggregateID:   evt.AggregateID(),
		Payload:       payload,
	}, nil
}

// RoutingKey is "<module>.<event>.v<version>", e.g. "example.created.v1".
func (e Envelope) RoutingKey() string {
	return RoutingKey(e.EventType, e.Version)
}

func RoutingKey(eventType string, version int) string {
	return fmt.Sprintf("%s.v%d", eventType, version)
}

// DecodePayload unmarshals the payload into dest.
func (e Envelope) DecodePayload(dest any) error {
	if err := json.Unmarshal(e.Payload, dest); err != nil {
		return fmt.Errorf("decode %s payload: %w", e.EventType, err)
	}
	return nil
}
