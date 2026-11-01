package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
	"github.com/Agora-Mobility/atlas/infra/observability/prometheus"
	"github.com/Agora-Mobility/atlas/infra/queue"
)

// Publisher is the part of queue.Publisher the relay needs.
type Publisher interface {
	PublishBatch(ctx context.Context, msgs []queue.Message) (int, error)
}

// Relay polls the outbox and publishes pending events. Delivery is
// at-least-once: if the commit fails after a publish, the event is published
// again and consumers deduplicate it by event_id.
type Relay struct {
	pool      *pgxpool.Pool
	publisher Publisher
	interval  time.Duration
	batchSize int32
}

func NewRelay(pool *pgxpool.Pool, publisher Publisher, interval time.Duration, batchSize int32) *Relay {
	return &Relay{pool: pool, publisher: publisher, interval: interval, batchSize: batchSize}
}

// Run blocks until ctx is canceled.
func (r *Relay) Run(ctx context.Context) {
	logger.FromContext(ctx).Info("outbox relay started", zap.Duration("interval", r.interval))
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.FromContext(context.WithoutCancel(ctx)).Info("outbox relay stopped")
			return
		case <-ticker.C:
			// Drain full batches immediately instead of waiting a tick each.
			for {
				n, err := r.processBatch(ctx)
				if err != nil {
					if ctx.Err() == nil {
						logger.FromContext(ctx).Error("outbox relay batch failed", zap.Error(err))
					}
					break
				}
				if n < int(r.batchSize) || ctx.Err() != nil {
					break
				}
			}
			r.reportPending(ctx)
		}
	}
}

func (r *Relay) processBatch(ctx context.Context) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	q := gen.New(tx)
	rows, err := q.FetchPendingOutboxEvents(ctx, r.batchSize)
	if err != nil {
		return 0, fmt.Errorf("fetch pending: %w", err)
	}

	msgs := make([]queue.Message, 0, len(rows))
	for _, row := range rows {
		msgs = append(msgs, queue.Message{ID: row.ID.String(), Type: row.EventType, RoutingKey: row.RoutingKey, Body: row.Envelope})
	}
	published, pubErr := r.publisher.PublishBatch(ctx, msgs)

	ids := make([]uuid.UUID, 0, published)
	for _, row := range rows[:published] {
		ids = append(ids, row.ID)
		prometheus.EventsPublished.WithLabelValues(row.EventType).Inc()
	}
	if err := q.MarkOutboxEventsPublished(ctx, ids); err != nil {
		return 0, fmt.Errorf("mark published: %w", err)
	}
	if pubErr != nil {
		failed := rows[published]
		logger.FromContext(ctx).Warn("outbox publish failed",
			zap.String("event_id", failed.ID.String()), zap.String("routing_key", failed.RoutingKey), zap.Error(pubErr))
		errText := pubErr.Error()
		if err := q.MarkOutboxEventFailed(ctx, gen.MarkOutboxEventFailedParams{ID: failed.ID, LastError: &errText}); err != nil {
			return 0, fmt.Errorf("mark failed: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return published, nil
}

func (r *Relay) reportPending(ctx context.Context) {
	n, err := gen.New(r.pool).CountPendingOutboxEvents(ctx)
	if err != nil {
		return
	}
	prometheus.OutboxPending.Set(float64(n))
}
