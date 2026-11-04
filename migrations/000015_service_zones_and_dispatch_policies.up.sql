CREATE TABLE service_zones (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    country_code CHAR(2)     NOT NULL,
    name         TEXT        NOT NULL,
    geom         geography(MultiPolygon, 4326) NOT NULL,
    timezone     TEXT        NOT NULL,
    currency     CHAR(3)     NOT NULL,
    is_active    BOOLEAN     NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX service_zones_geom_idx ON service_zones USING GIST (geom);

CREATE TABLE dispatch_policies (
    id                   UUID             PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id          UUID             NOT NULL REFERENCES service_categories (id),
    zone_id              UUID             REFERENCES service_zones (id),
    candidate_source     TEXT             NOT NULL CHECK (candidate_source IN ('live', 'coverage')),
    h3_resolution        SMALLINT         NOT NULL CHECK (h3_resolution BETWEEN 0 AND 15),
    radii_km             DOUBLE PRECISION[] NOT NULL CHECK (cardinality(radii_km) > 0),
    offer_mode           TEXT             NOT NULL CHECK (offer_mode IN ('sequential', 'parallel', 'quote')),
    batch_size           SMALLINT         NOT NULL CHECK (batch_size > 0),
    offer_ttl_seconds    INTEGER          NOT NULL CHECK (offer_ttl_seconds > 0),
    search_ttl_seconds   INTEGER          NOT NULL CHECK (search_ttl_seconds > 0),
    ack_timeout_seconds  INTEGER          NOT NULL CHECK (ack_timeout_seconds > 0),
    ack_giveup_seconds   INTEGER          NOT NULL DEFAULT 0 CHECK (ack_giveup_seconds >= 0),
    initial_channels     TEXT[]           NOT NULL,
    fallback_channels    TEXT[]           NOT NULL,
    ranking              TEXT             NOT NULL CHECK (ranking IN ('distance_idle', 'distance', 'base_distance', 'customer_choice')),
    tracking_statuses    TEXT[]           NOT NULL,
    on_exhausted         TEXT             NOT NULL CHECK (on_exhausted IN ('suggest_retry', 'alert_operator', 'operator_queue', 'extend_or_queue')),
    reserve_provider     BOOLEAN          NOT NULL,
    schedule_max_days    INTEGER          NOT NULL DEFAULT 0 CHECK (schedule_max_days >= 0),
    search_lead_seconds  INTEGER          NOT NULL DEFAULT 0 CHECK (search_lead_seconds >= 0),
    pricing_mode         TEXT             NOT NULL CHECK (pricing_mode IN ('fixed', 'call_out', 'quote')),
    allow_boost          BOOLEAN          NOT NULL DEFAULT false,
    rank_by_eta          BOOLEAN          NOT NULL DEFAULT false,
    sms_on_accept        BOOLEAN          NOT NULL DEFAULT false,
    created_at           TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ      NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX dispatch_policies_scope_key
    ON dispatch_policies (category_id, COALESCE(zone_id, '00000000-0000-0000-0000-000000000000'::uuid));

INSERT INTO dispatch_policies (
    category_id, candidate_source, h3_resolution, radii_km, offer_mode, batch_size, offer_ttl_seconds, search_ttl_seconds,
    ack_timeout_seconds, ack_giveup_seconds, initial_channels, fallback_channels, ranking, tracking_statuses, on_exhausted,
    reserve_provider, schedule_max_days, search_lead_seconds, pricing_mode, allow_boost, sms_on_accept)
SELECT c.id, p.source, p.resolution, p.radii, p.mode, p.batch, p.offer_ttl, p.search_ttl, p.ack, p.giveup, p.initial, p.fallback,
       p.ranking, p.tracking, p.exhausted, p.reserve, p.schedule_days, p.lead, p.pricing, p.boost, p.sms
FROM (VALUES
    ('taxi', 'live', 7, ARRAY[3, 5, 10]::float8[], 'sequential', 1, 20, 180, 3, 8, ARRAY['sse'], ARRAY['fcm'], 'distance_idle',
     ARRAY['provider_en_route', 'in_progress'], 'suggest_retry', true, 7, 1800, 'fixed', true, false),
    ('towing', 'live', 5, ARRAY[10, 30, 80]::float8[], 'parallel', 3, 45, 600, 5, 30, ARRAY['sse'], ARRAY['fcm', 'sms'], 'distance',
     ARRAY['provider_en_route', 'in_progress'], 'alert_operator', true, 0, 0, 'fixed', false, true),
    ('maintenance', 'coverage', 6, ARRAY[30]::float8[], 'parallel', 5, 600, 7200, 60, 0, ARRAY['fcm', 'sms', 'sse'], ARRAY[]::text[], 'base_distance',
     ARRAY['provider_en_route'], 'operator_queue', false, 30, 7200, 'call_out', false, false),
    ('removals', 'coverage', 6, ARRAY[50]::float8[], 'quote', 10, 7200, 86400, 300, 0, ARRAY['fcm', 'sms', 'sse'], ARRAY[]::text[], 'customer_choice',
     ARRAY['provider_en_route', 'in_progress'], 'extend_or_queue', false, 60, 86400, 'quote', false, false)
) AS p (slug, source, resolution, radii, mode, batch, offer_ttl, search_ttl, ack, giveup, initial, fallback, ranking, tracking, exhausted, reserve, schedule_days, lead, pricing, boost, sms)
JOIN service_categories c ON c.slug = p.slug;
