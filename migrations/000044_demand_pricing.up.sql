CREATE TABLE demand_pricing (
    id                  UUID        PRIMARY KEY,
    zone_id             UUID        NOT NULL REFERENCES service_zones (id),
    category_id         UUID        NOT NULL REFERENCES service_categories (id),
    mode                TEXT        NOT NULL DEFAULT 'off' CHECK (mode IN ('off', 'observe', 'on')),
    tiers               JSONB       NOT NULL DEFAULT '[]',
    max_factor_bps      INTEGER     NOT NULL CHECK (max_factor_bps BETWEEN 10000 AND 30000),
    commission_on_surge BOOLEAN     NOT NULL DEFAULT false,
    updated_by          UUID,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT demand_pricing_zone_category_key UNIQUE (zone_id, category_id)
);

CREATE TABLE demand_snapshots (
    id          BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    zone_id     UUID        NOT NULL REFERENCES service_zones (id),
    category_id UUID        NOT NULL REFERENCES service_categories (id),
    area_h3     BIGINT      NOT NULL,
    pending     INTEGER     NOT NULL,
    free        INTEGER     NOT NULL,
    ratio_bps   INTEGER     NOT NULL,
    factor_bps  INTEGER     NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX demand_snapshots_lookup_idx ON demand_snapshots (zone_id, category_id, recorded_at DESC);
CREATE INDEX demand_snapshots_date_idx ON demand_snapshots (recorded_at);

ALTER TABLE fare_estimates
    ADD COLUMN demand_factor_bps INTEGER NOT NULL DEFAULT 10000 CHECK (demand_factor_bps BETWEEN 10000 AND 30000),
    ADD COLUMN demand_cents BIGINT NOT NULL DEFAULT 0 CHECK (demand_cents >= 0),
    ADD COLUMN demand_commissioned BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE fare_adjustments DROP CONSTRAINT fare_adjustments_reason_check;
ALTER TABLE fare_adjustments ADD CONSTRAINT fare_adjustments_reason_check CHECK (reason IN ('boost', 'waiting', 'route_change', 'demand'));
