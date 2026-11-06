ALTER TABLE service_categories ADD COLUMN code_prefix TEXT NOT NULL DEFAULT 'AG';
UPDATE service_categories SET code_prefix = 'TX' WHERE slug = 'taxi';
UPDATE service_categories SET code_prefix = 'MN' WHERE slug = 'maintenance';
UPDATE service_categories SET code_prefix = 'RB' WHERE slug = 'towing';
UPDATE service_categories SET code_prefix = 'MD' WHERE slug = 'removals';

CREATE SEQUENCE service_request_code_seq START 10000;

ALTER TABLE service_requests ADD COLUMN public_code TEXT;
UPDATE service_requests r SET public_code = c.code_prefix || '-' || nextval('service_request_code_seq')
FROM service_categories c WHERE c.id = r.category_id;
ALTER TABLE service_requests ALTER COLUMN public_code SET NOT NULL;
CREATE UNIQUE INDEX service_requests_public_code_key ON service_requests (public_code);

ALTER TABLE service_request_stops ADD COLUMN arrived_at TIMESTAMPTZ;

CREATE SEQUENCE support_ticket_code_seq START 1000;

CREATE TABLE support_tickets (
    id           UUID        PRIMARY KEY,
    code         TEXT        NOT NULL UNIQUE,
    user_id      UUID        NOT NULL REFERENCES users (id),
    request_id   UUID        REFERENCES service_requests (id),
    topic_id     UUID        REFERENCES catalog_items (id),
    subject      TEXT        NOT NULL,
    status       TEXT        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'answered', 'closed')),
    created_at   TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX support_tickets_user_idx ON support_tickets (user_id, created_at DESC);
CREATE INDEX support_tickets_status_idx ON support_tickets (status, created_at DESC);

CREATE TABLE support_messages (
    id          BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticket_id   UUID        NOT NULL REFERENCES support_tickets (id),
    sender_type TEXT        NOT NULL CHECK (sender_type IN ('user', 'staff')),
    sender_id   UUID        NOT NULL,
    body        TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX support_messages_ticket_idx ON support_messages (ticket_id, id);
