CREATE TABLE request_messages (
    id             UUID        PRIMARY KEY,
    request_id     UUID        NOT NULL REFERENCES service_requests (id),
    sender_id      UUID        NOT NULL REFERENCES users (id),
    sender_type    TEXT        NOT NULL CHECK (sender_type IN ('customer', 'provider')),
    body           TEXT        NOT NULL,
    quick_reply_id UUID        REFERENCES catalog_items (id),
    read_at        TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL
);

CREATE INDEX request_messages_request_idx ON request_messages (request_id, created_at);

CREATE TABLE request_shares (
    id         UUID        PRIMARY KEY,
    request_id UUID        NOT NULL REFERENCES service_requests (id),
    token_hash TEXT        NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX request_shares_request_idx ON request_shares (request_id);

CREATE TABLE sos_alerts (
    id          UUID             PRIMARY KEY,
    request_id  UUID             NOT NULL REFERENCES service_requests (id),
    user_id     UUID             NOT NULL REFERENCES users (id),
    lat         DOUBLE PRECISION,
    lng         DOUBLE PRECISION,
    note        TEXT             NOT NULL DEFAULT '',
    status      TEXT             NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
    resolved_by UUID,
    resolved_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ      NOT NULL
);

CREATE INDEX sos_alerts_status_idx ON sos_alerts (status, created_at DESC);
CREATE UNIQUE INDEX sos_alerts_one_open_key ON sos_alerts (request_id, user_id) WHERE status = 'open';

INSERT INTO platform_settings (key, value) VALUES ('share_ttl_minutes', '240');

INSERT INTO catalog_items (kind, code, label, attrs, display_order) VALUES
    ('quick_reply', 'customer_coming', 'Estou a chegar', '{"actor": "customer"}', 1),
    ('quick_reply', 'customer_here', 'Já estou no local', '{"actor": "customer"}', 2),
    ('quick_reply', 'customer_wait', 'Aguarde um momento, por favor', '{"actor": "customer"}', 3),
    ('quick_reply', 'provider_on_way', 'Estou a caminho', '{"actor": "provider"}', 1),
    ('quick_reply', 'provider_here', 'Já cheguei', '{"actor": "provider"}', 2),
    ('quick_reply', 'provider_where', 'Onde se encontra?', '{"actor": "provider"}', 3);
