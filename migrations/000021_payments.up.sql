ALTER TABLE service_requests
    ADD COLUMN payment_method TEXT NOT NULL DEFAULT 'cash' CHECK (payment_method IN ('cash', 'gpo', 'reference')),
    ADD COLUMN payment_status TEXT NOT NULL DEFAULT 'unpaid' CHECK (payment_status IN ('unpaid', 'pending', 'paid', 'refunded'));

CREATE INDEX service_requests_outstanding_idx ON service_requests (customer_id)
    WHERE status = 'completed' AND payment_method <> 'cash' AND payment_status IN ('unpaid', 'pending');

CREATE TABLE category_payment_methods (
    category_id   UUID        NOT NULL REFERENCES service_categories (id) ON DELETE CASCADE,
    method        TEXT        NOT NULL CHECK (method IN ('cash', 'gpo', 'reference')),
    is_enabled    BOOLEAN     NOT NULL DEFAULT true,
    display_order INTEGER     NOT NULL DEFAULT 0,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (category_id, method)
);

INSERT INTO category_payment_methods (category_id, method, is_enabled, display_order)
SELECT c.id, m.method, m.method <> 'reference' OR c.slug IN ('maintenance', 'removals'), m.position
FROM service_categories c
CROSS JOIN (VALUES ('cash', 1), ('gpo', 2), ('reference', 3)) AS m (method, position);

CREATE TABLE payments (
    id                 UUID        PRIMARY KEY,
    request_id         UUID        NOT NULL REFERENCES service_requests (id),
    customer_id        UUID        NOT NULL REFERENCES users (id),
    method             TEXT        NOT NULL CHECK (method IN ('cash', 'gpo', 'reference')),
    status             TEXT        NOT NULL CHECK (status IN ('pending', 'paid', 'failed', 'expired')),
    amount_cents       BIGINT      NOT NULL CHECK (amount_cents > 0),
    refunded_cents     BIGINT      NOT NULL DEFAULT 0 CHECK (refunded_cents >= 0),
    currency           CHAR(3)     NOT NULL,
    merchant_id        TEXT        NOT NULL UNIQUE,
    provider_charge_id TEXT        UNIQUE,
    payer_phone_masked TEXT        NOT NULL DEFAULT '',
    reference_number   TEXT        NOT NULL DEFAULT '',
    reference_entity   TEXT        NOT NULL DEFAULT '',
    failure_message    TEXT        NOT NULL DEFAULT '',
    expires_at         TIMESTAMPTZ,
    next_check_at      TIMESTAMPTZ,
    paid_at            TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX payments_one_pending_key ON payments (request_id) WHERE status = 'pending';
CREATE INDEX payments_request_idx ON payments (request_id, created_at DESC);
CREATE INDEX payments_due_idx ON payments (next_check_at) WHERE status = 'pending';
CREATE INDEX payments_created_idx ON payments (created_at DESC);

CREATE TABLE payment_events (
    id          BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    payment_id  UUID        NOT NULL REFERENCES payments (id) ON DELETE CASCADE,
    source      TEXT        NOT NULL,
    status      TEXT        NOT NULL,
    detail      TEXT        NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX payment_events_payment_idx ON payment_events (payment_id, id);

CREATE TABLE payment_refunds (
    id                 UUID        PRIMARY KEY,
    payment_id         UUID        NOT NULL REFERENCES payments (id),
    amount_cents       BIGINT      NOT NULL CHECK (amount_cents > 0),
    reason             TEXT        NOT NULL,
    provider_refund_id TEXT        NOT NULL DEFAULT '',
    actor_id           UUID,
    created_at         TIMESTAMPTZ NOT NULL
);

CREATE INDEX payment_refunds_payment_idx ON payment_refunds (payment_id, created_at);
