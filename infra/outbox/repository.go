// Package outbox implements the transactional outbox: the repository that
// writes events and the relay that publishes them to RabbitMQ.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type Repository struct {
	pool *pgxpool.Pool
}

var _ repository.IOutboxRepository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Save(ctx context.Context, env events.Envelope) error {
	raw, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}

	var correlationID *string
	if env.CorrelationID != "" {
		correlationID = &env.CorrelationID
	}

	err = postgres.Queries(ctx, r.pool).InsertOutboxEvent(ctx, gen.InsertOutboxEventParams{
		ID:            env.EventID,
		AggregateType: env.AggregateType,
		AggregateID:   env.AggregateID,
		EventType:     env.EventType,
		EventVersion:  int32(env.Version), // #nosec G115 -- event versions are small
		RoutingKey:    env.RoutingKey(),
		Envelope:      raw,
		CorrelationID: correlationID,
		OccurredAt:    env.OccurredAt,
	})
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}
