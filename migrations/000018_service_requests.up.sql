CREATE TABLE service_requests (
    id                   UUID             PRIMARY KEY,
    customer_id          UUID             NOT NULL REFERENCES users (id),
    category_id          UUID             NOT NULL REFERENCES service_categories (id),
    category_slug        TEXT             NOT NULL,
    service_id           UUID             NOT NULL REFERENCES services (id),
    zone_id              UUID             NOT NULL REFERENCES service_zones (id),
    policy_id            UUID             NOT NULL REFERENCES dispatch_policies (id),
    status               TEXT             NOT NULL CHECK (status IN ('created', 'scheduled', 'searching', 'offered', 'awaiting_choice', 'accepted', 'provider_en_route', 'in_progress', 'completed', 'expired', 'cancelled')),
    version              INTEGER          NOT NULL DEFAULT 1,
    pickup_address_id    UUID             NOT NULL REFERENCES addresses (id),
    contact_phone        TEXT             NOT NULL DEFAULT '',
    pickup_h3_r8         BIGINT           NOT NULL,
    destination_address_id UUID           REFERENCES addresses (id),
    destination_h3_r8    BIGINT,
    passenger_name       TEXT             NOT NULL DEFAULT '',
    passenger_phone      TEXT             NOT NULL DEFAULT '',
    details              JSONB            NOT NULL DEFAULT '{}',
    scheduled_for        TIMESTAMPTZ,
    search_round         SMALLINT         NOT NULL DEFAULT 0,
    search_extended      BOOLEAN          NOT NULL DEFAULT false,
    search_deadline_at   TIMESTAMPTZ,
    next_action_at       TIMESTAMPTZ,
    reminder_at          TIMESTAMPTZ,
    provider_id          UUID             REFERENCES users (id),
    organization_id      UUID             REFERENCES organizations (id),
    fare_estimate_id     UUID             REFERENCES fare_estimates (id),
    fare_cents           BIGINT,
    commission_cents     BIGINT,
    boost_bps            INTEGER          NOT NULL DEFAULT 0,
    currency             CHAR(3)          NOT NULL,
    needs_operator       BOOLEAN          NOT NULL DEFAULT false,
    operator_reason      TEXT             NOT NULL DEFAULT '',
    idempotency_key      TEXT             NOT NULL,
    cancel_reason        TEXT             NOT NULL DEFAULT '',
    cancelled_by         TEXT             NOT NULL DEFAULT '',
    accepted_at          TIMESTAMPTZ,
    arrived_at           TIMESTAMPTZ,
    started_at           TIMESTAMPTZ,
    completed_at         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ      NOT NULL DEFAULT now(),
    CONSTRAINT service_requests_idempotency_key UNIQUE (customer_id, idempotency_key)
);

CREATE INDEX service_requests_due_idx ON service_requests (next_action_at) WHERE next_action_at IS NOT NULL;
CREATE INDEX service_requests_reminder_idx ON service_requests (reminder_at) WHERE reminder_at IS NOT NULL;
CREATE INDEX service_requests_customer_idx ON service_requests (customer_id, created_at DESC);
CREATE INDEX service_requests_provider_idx ON service_requests (provider_id, created_at DESC) WHERE provider_id IS NOT NULL;
CREATE INDEX service_requests_organization_idx ON service_requests (organization_id, created_at DESC) WHERE organization_id IS NOT NULL;
CREATE INDEX service_requests_status_idx ON service_requests (status, created_at DESC);
CREATE INDEX service_requests_operator_idx ON service_requests (created_at) WHERE needs_operator;
CREATE INDEX service_requests_h3_idx ON service_requests (pickup_h3_r8, created_at);

CREATE TABLE service_request_stops (
    request_id UUID    NOT NULL REFERENCES service_requests (id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,
    address_id UUID    NOT NULL REFERENCES addresses (id),
    PRIMARY KEY (request_id, position)
);

CREATE TABLE service_request_events (
    id          BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    request_id  UUID        NOT NULL REFERENCES service_requests (id),
    from_status TEXT        NOT NULL,
    to_status   TEXT        NOT NULL,
    version     INTEGER     NOT NULL,
    actor_type  TEXT        NOT NULL,
    actor_id    UUID,
    reason      TEXT        NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX service_request_events_request_idx ON service_request_events (request_id, id);
CREATE INDEX service_request_events_date_idx ON service_request_events (occurred_at);

CREATE TABLE request_offers (
    id                UUID        PRIMARY KEY,
    request_id        UUID        NOT NULL REFERENCES service_requests (id),
    provider_id       UUID        NOT NULL REFERENCES users (id),
    status            TEXT        NOT NULL CHECK (status IN ('sent', 'delivered', 'accepted', 'rejected', 'expired', 'no_response', 'quoted', 'cancelled')),
    sent_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at      TIMESTAMPTZ,
    delivered_via     TEXT,
    expires_at        TIMESTAMPTZ NOT NULL,
    responded_at      TIMESTAMPTZ,
    next_action_at    TIMESTAMPTZ,
    fallback_step     SMALLINT    NOT NULL DEFAULT 0,
    quote_cents       BIGINT,
    quote_note        TEXT        NOT NULL DEFAULT '',
    distance_m        INTEGER,
    net_cents         BIGINT,
    boost_bps         INTEGER     NOT NULL DEFAULT 0,
    UNIQUE (request_id, provider_id)
);

CREATE INDEX request_offers_due_idx ON request_offers (next_action_at) WHERE next_action_at IS NOT NULL;
CREATE INDEX request_offers_provider_idx ON request_offers (provider_id, sent_at DESC);

CREATE TABLE fare_adjustments (
    id           BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    request_id   UUID        NOT NULL REFERENCES service_requests (id),
    reason       TEXT        NOT NULL CHECK (reason IN ('boost', 'waiting', 'route_change')),
    amount_cents BIGINT      NOT NULL,
    actor_id     UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX fare_adjustments_request_idx ON fare_adjustments (request_id, id);

CREATE TABLE provider_location_samples (
    id          BIGINT           GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider_id UUID             NOT NULL,
    request_id  UUID             NOT NULL,
    lat         DOUBLE PRECISION NOT NULL,
    lng         DOUBLE PRECISION NOT NULL,
    recorded_at TIMESTAMPTZ      NOT NULL
);

CREATE INDEX provider_location_samples_request_idx ON provider_location_samples (request_id, recorded_at);
CREATE INDEX provider_location_samples_date_idx ON provider_location_samples (recorded_at);
