CREATE TYPE client_type AS ENUM ('customer_app', 'provider_app', 'partner_panel', 'backoffice');

CREATE TABLE clients (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id  TEXT        NOT NULL,
    name       TEXT        NOT NULL,
    type       client_type NOT NULL,
    is_active  BOOLEAN     NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX clients_client_id_key ON clients (client_id);

INSERT INTO clients (client_id, name, type) VALUES
    ('agora-customer-app', 'AGORA – app do cliente', 'customer_app'),
    ('agora-provider-app', 'AGORA – app do prestador', 'provider_app'),
    ('agora-partner-panel', 'AGORA – painel do parceiro', 'partner_panel'),
    ('agora-backoffice', 'AGORA – backoffice', 'backoffice');
