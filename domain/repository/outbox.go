package repository

import (
	"context"

	"github.com/Agora-Mobility/atlas/domain/events"
)

// IOutboxRepository stores events to be published. Call Save inside
// ITransactionManager.WithinTransaction together with the state change.
type IOutboxRepository interface {
	Save(ctx context.Context, env events.Envelope) error
}
