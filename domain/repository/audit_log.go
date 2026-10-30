package repository

import (
	"context"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

// IAuditLogRepository appends to the audit trail (ADR 0013). Call Record
// inside ITransactionManager.WithinTransaction together with the change.
type IAuditLogRepository interface {
	Record(ctx context.Context, entry *entities.AuditEntry) error
}
