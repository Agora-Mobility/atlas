package queue

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Deduplicator remembers processed event ids in Redis so redelivered events
// are handled once per consumer queue.
type Deduplicator struct {
	client *goredis.Client
	ttl    time.Duration
}

func NewDeduplicator(client *goredis.Client, ttl time.Duration) *Deduplicator {
	return &Deduplicator{client: client, ttl: ttl}
}

func dedupKey(queue, eventID string) string {
	return fmt.Sprintf("atlas:events:processed:%s:%s", queue, eventID)
}

// IsProcessed reports whether this queue already handled the event.
func (d *Deduplicator) IsProcessed(ctx context.Context, queue, eventID string) (bool, error) {
	n, err := d.client.Exists(ctx, dedupKey(queue, eventID)).Result()
	if err != nil {
		return false, fmt.Errorf("dedup check: %w", err)
	}
	return n > 0, nil
}

// MarkProcessed records a successful handling. It is called only after the
// handler succeeds, so a crash mid-handler never hides a redelivery.
func (d *Deduplicator) MarkProcessed(ctx context.Context, queue, eventID string) error {
	if err := d.client.Set(ctx, dedupKey(queue, eventID), 1, d.ttl).Err(); err != nil {
		return fmt.Errorf("dedup mark: %w", err)
	}
	return nil
}
