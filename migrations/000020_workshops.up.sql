CREATE TABLE workshops (
    id                     UUID        PRIMARY KEY,
    organization_id        UUID        NOT NULL REFERENCES organizations (id),
    address_id             UUID        NOT NULL REFERENCES addresses (id),
    name                   TEXT        NOT NULL,
    phone                  TEXT        NOT NULL DEFAULT '',
    opening_hours          TEXT        NOT NULL DEFAULT '',
    offers_home_service    BOOLEAN     NOT NULL DEFAULT false,
    home_service_fee_cents BIGINT      NOT NULL DEFAULT 0 CHECK (home_service_fee_cents >= 0),
    currency               CHAR(3)     NOT NULL,
    is_active              BOOLEAN     NOT NULL DEFAULT true,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX workshops_organization_idx ON workshops (organization_id);

CREATE TABLE workshop_services (
    id               UUID        PRIMARY KEY,
    workshop_id      UUID        NOT NULL REFERENCES workshops (id) ON DELETE CASCADE,
    name             TEXT        NOT NULL,
    description      TEXT        NOT NULL DEFAULT '',
    price_cents      BIGINT      NOT NULL CHECK (price_cents >= 0),
    duration_minutes INTEGER     NOT NULL DEFAULT 0 CHECK (duration_minutes >= 0),
    is_active        BOOLEAN     NOT NULL DEFAULT true,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX workshop_services_workshop_idx ON workshop_services (workshop_id);
