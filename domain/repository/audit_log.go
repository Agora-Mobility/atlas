package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

// IAuditLogRepository appends to the audit trail (ADR 0013). Call Record
// inside ITransactionManager.WithinTransaction together with the change.
type IAuditLogRepository interface {
	Record(ctx context.Context, entry *entities.AuditEntry) error
}

type AuditFilter struct {
	ActorID    *uuid.UUID
	EntityType string
	EntityID   string
	Action     string
	From       *time.Time
	To         *time.Time
	Limit      int
	Offset     int
}

type AuditRecord struct {
	ID             uuid.UUID
	ActorID        *uuid.UUID
	ActorRole      string
	ActorName      string
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

type IAuditLogReader interface {
	List(ctx context.Context, filter AuditFilter) ([]AuditRecord, int64, error)
}
