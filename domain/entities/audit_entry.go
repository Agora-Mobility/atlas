package entities

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

// Actor is who performed an audited action. A nil ID is the system.
type Actor struct {
	ID             *uuid.UUID
	Role           enums.Role
	OrganizationID *uuid.UUID
	IPAddress      string
	UserAgent      string
}

func SystemActor() Actor {
	return Actor{}
}

// AuditEntry is one append-only record of a state change (ADR 0013).
type AuditEntry struct {
	ID             uuid.UUID
	Actor          Actor
	OrganizationID *uuid.UUID
	Action         string
	EntityType     string
	EntityID       string
	Before         json.RawMessage
	After          json.RawMessage
	Reason         string
	CorrelationID  string
	CreatedAt      time.Time
}

// NewAuditEntry snapshots before and after as JSON. Pass values without
// secrets (for users, the public view, never the entity with its hash).
func NewAuditEntry(actor Actor, action, entityType, entityID string, before, after any, reason, correlationID string) (*AuditEntry, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	beforeJSON, err := snapshot(before)
	if err != nil {
		return nil, fmt.Errorf("audit before snapshot: %w", err)
	}
	afterJSON, err := snapshot(after)
	if err != nil {
		return nil, fmt.Errorf("audit after snapshot: %w", err)
	}
	return &AuditEntry{
		ID:             id,
		Actor:          actor,
		OrganizationID: actor.OrganizationID,
		Action:         action,
		EntityType:     entityType,
		EntityID:       entityID,
		Before:         beforeJSON,
		After:          afterJSON,
		Reason:         reason,
		CorrelationID:  correlationID,
		CreatedAt:      time.Now().UTC(),
	}, nil
}

func (e *AuditEntry) InOrganization(organizationID uuid.UUID) *AuditEntry {
	e.OrganizationID = &organizationID
	return e
}

func snapshot(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}
