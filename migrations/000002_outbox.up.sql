-- Transactional outbox: domain events are written here in the same
-- transaction as the state change, then published to RabbitMQ by the relay.
CREATE TABLE outbox (
    id             UUID        PRIMARY KEY,           -- event_id
    aggregate_type TEXT        NOT NULL,
    aggregate_id   TEXT        NOT NULL,
    event_type     TEXT        NOT NULL,              -- e.g. example.created
    event_version  INTEGER     NOT NULL,
    routing_key    TEXT        NOT NULL,              -- <module>.<event>.v<version>
    envelope       JSONB       NOT NULL,              -- full event envelope as published
    correlation_id TEXT,
    occurred_at    TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at   TIMESTAMPTZ,
    attempts       INTEGER     NOT NULL DEFAULT 0,
    last_error     TEXT
);

CREATE INDEX outbox_pending_idx ON outbox (created_at) WHERE published_at IS NULL;
CREATE INDEX outbox_aggregate_idx ON outbox (aggregate_type, aggregate_id);
