-- Service catalog (ADR 0008): categories and the services inside them are
-- data managed from the backoffice. Rows are never deleted, only
-- deactivated, so requests keep their references.
CREATE TABLE service_categories (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    slug          TEXT        NOT NULL,
    name          TEXT        NOT NULL,
    description   TEXT        NOT NULL DEFAULT '',
    icon          TEXT        NOT NULL DEFAULT '',
    display_order INTEGER     NOT NULL DEFAULT 0,
    is_active     BOOLEAN     NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX service_categories_slug_key ON service_categories (slug);

-- Fees are in cêntimos of kwanza (1 Kz = 100), so money never goes through
-- floating point.
CREATE TABLE services (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id          UUID        NOT NULL REFERENCES service_categories (id),
    slug                 TEXT        NOT NULL,
    name                 TEXT        NOT NULL,
    description          TEXT        NOT NULL DEFAULT '',
    icon                 TEXT        NOT NULL DEFAULT '',
    requires_destination BOOLEAN     NOT NULL,
    base_fee_cents       BIGINT      NOT NULL DEFAULT 0 CHECK (base_fee_cents >= 0),
    per_km_fee_cents     BIGINT      NOT NULL DEFAULT 0 CHECK (per_km_fee_cents >= 0),
    per_minute_fee_cents BIGINT      NOT NULL DEFAULT 0 CHECK (per_minute_fee_cents >= 0),
    display_order        INTEGER     NOT NULL DEFAULT 0,
    is_active            BOOLEAN     NOT NULL DEFAULT true,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX services_slug_key ON services (slug);
CREATE INDEX services_category_idx ON services (category_id, display_order);

-- The MVP 1 categories. Their slugs are stable: code may reference them.
INSERT INTO service_categories (slug, name, description, icon, display_order) VALUES
    ('taxi', 'Táxi', 'Transporte de passageiros até ao destino.', 'taxi', 1),
    ('maintenance', 'Manutenção', 'Assistência e reparação no local.', 'maintenance', 2),
    ('towing', 'Reboque', 'Reboque de viaturas até ao destino.', 'towing', 3),
    ('removals', 'Mudanças', 'Transporte de bens e mudanças.', 'removals', 4);

-- One base service per category. The fees start at zero until the backoffice
-- sets the tariff.
INSERT INTO services (category_id, slug, name, description, icon, requires_destination, display_order)
SELECT c.id, s.slug, s.name, s.description, s.icon, s.requires_destination, 1
FROM (VALUES
    ('taxi', 'taxi', 'Táxi', 'Viagem até ao destino indicado.', 'taxi', true),
    ('maintenance', 'maintenance', 'Manutenção', 'Um técnico desloca-se ao local indicado.', 'maintenance', false),
    ('towing', 'towing', 'Reboque', 'Reboque da viatura até ao destino indicado.', 'towing', true),
    ('removals', 'removals', 'Mudanças', 'Transporte dos bens até ao destino indicado.', 'removals', true)
) AS s (category_slug, slug, name, description, icon, requires_destination)
JOIN service_categories c ON c.slug = s.category_slug;
