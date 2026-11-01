CREATE TABLE service_tariffs (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id           UUID        NOT NULL REFERENCES services (id),
    zone_id              UUID        REFERENCES service_zones (id),
    currency             CHAR(3)     NOT NULL,
    base_fee_cents       BIGINT      NOT NULL CHECK (base_fee_cents >= 0),
    per_km_fee_cents     BIGINT      NOT NULL CHECK (per_km_fee_cents >= 0),
    per_minute_fee_cents BIGINT      NOT NULL CHECK (per_minute_fee_cents >= 0),
    minimum_fee_cents    BIGINT      NOT NULL CHECK (minimum_fee_cents >= 0),
    zone_fee_cents       BIGINT      NOT NULL DEFAULT 0 CHECK (zone_fee_cents >= 0),
    night_fee_cents      BIGINT      NOT NULL DEFAULT 0 CHECK (night_fee_cents >= 0),
    night_start_hour     SMALLINT    NOT NULL DEFAULT 22 CHECK (night_start_hour BETWEEN 0 AND 23),
    night_end_hour       SMALLINT    NOT NULL DEFAULT 6 CHECK (night_end_hour BETWEEN 0 AND 23),
    free_wait_seconds    INTEGER     NOT NULL DEFAULT 180 CHECK (free_wait_seconds >= 0),
    commission_bps       INTEGER     NOT NULL CHECK (commission_bps BETWEEN 0 AND 10000),
    valid_from           TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_until          TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX service_tariffs_current_key
    ON service_tariffs (service_id, COALESCE(zone_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE valid_until IS NULL;

CREATE FUNCTION service_tariffs_only_close() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'service_tariffs rows are never deleted';
    END IF;
    IF OLD.valid_until IS NOT NULL OR NEW.valid_until IS NULL
        OR (to_jsonb(NEW) - 'valid_until') IS DISTINCT FROM (to_jsonb(OLD) - 'valid_until') THEN
        RAISE EXCEPTION 'a tariff only changes by closing it and creating a new version';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER service_tariffs_immutable
    BEFORE UPDATE OR DELETE ON service_tariffs
    FOR EACH ROW EXECUTE FUNCTION service_tariffs_only_close();

CREATE TABLE fare_estimates (
    id               UUID             PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id      UUID             NOT NULL REFERENCES users (id),
    tariff_id        UUID             NOT NULL REFERENCES service_tariffs (id),
    service_id       UUID             NOT NULL REFERENCES services (id),
    origin_lat       DOUBLE PRECISION NOT NULL,
    origin_lng       DOUBLE PRECISION NOT NULL,
    destination_lat  DOUBLE PRECISION,
    destination_lng  DOUBLE PRECISION,
    stops            JSONB            NOT NULL DEFAULT '[]',
    distance_m       INTEGER          NOT NULL,
    duration_s       INTEGER          NOT NULL,
    route_source     TEXT             NOT NULL CHECK (route_source IN ('mapbox', 'fallback')),
    total_cents      BIGINT           NOT NULL,
    commission_cents BIGINT           NOT NULL,
    currency         CHAR(3)          NOT NULL,
    expires_at       TIMESTAMPTZ      NOT NULL,
    created_at       TIMESTAMPTZ      NOT NULL DEFAULT now()
);

CREATE INDEX fare_estimates_customer_idx ON fare_estimates (customer_id, created_at DESC);

CREATE TABLE eta_observations (
    origin_h3_r8      BIGINT   NOT NULL,
    destination_h3_r8 BIGINT   NOT NULL,
    hour_band         SMALLINT NOT NULL CHECK (hour_band BETWEEN 0 AND 23),
    samples           INTEGER  NOT NULL,
    total_seconds     BIGINT   NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (origin_h3_r8, destination_h3_r8, hour_band)
);
