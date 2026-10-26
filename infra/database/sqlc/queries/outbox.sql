-- name: InsertOutboxEvent :exec
INSERT INTO outbox (
    id, aggregate_type, aggregate_id, event_type, event_version,
    routing_key, envelope, correlation_id, occurred_at
) VALUES (
    @id, @aggregate_type, @aggregate_id, @event_type, @event_version,
    @routing_key, @envelope, sqlc.narg(correlation_id), @occurred_at
);

-- name: FetchPendingOutboxEvents :many
-- Locks a batch of unpublished events. SKIP LOCKED lets several relays run
-- concurrently without publishing the same event twice.
SELECT id, routing_key, envelope, event_type, attempts
FROM outbox
WHERE published_at IS NULL
ORDER BY created_at
LIMIT @batch_size
FOR UPDATE SKIP LOCKED;

-- name: MarkOutboxEventPublished :exec
UPDATE outbox
SET published_at = now(), attempts = attempts + 1, last_error = NULL
WHERE id = @id;

-- name: MarkOutboxEventFailed :exec
UPDATE outbox
SET attempts = attempts + 1, last_error = @last_error
WHERE id = @id;

-- name: CountPendingOutboxEvents :one
SELECT count(*) FROM outbox WHERE published_at IS NULL;
