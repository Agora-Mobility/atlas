# ADR 0004 – Event-driven with transactional outbox and RabbitMQ

- Status: accepted
- Date: 2026-10-26

## Context

Modules must react to each other's changes (e.g. request accepted → notify) without tight coupling, and events must never be lost or published for changes that rolled back.

## Decision

Usecases write the change and the event to the `outbox` table in one transaction. A relay publishes pending rows to the `agora.events` topic exchange (routing key `<module>.<event>.v<version>`) with publisher confirms. Consumers use durable quorum queues, manual ack, a retry queue with per-message TTL, a DLQ after N attempts, and Redis deduplication by `event_id`.

## Consequences

At-least-once delivery: handlers must be idempotent. Small publish latency (poll interval). Event contracts are versioned; breaking changes create a new version.
