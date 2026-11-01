CREATE TABLE provider_profiles (
    user_id               UUID        PRIMARY KEY REFERENCES users (id),
    status                TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'suspended')),
    vehicle_plate         TEXT        NOT NULL DEFAULT '',
    vehicle_description   TEXT        NOT NULL DEFAULT '',
    base_lat              DOUBLE PRECISION,
    base_lng              DOUBLE PRECISION,
    service_radius_km     DOUBLE PRECISION,
    last_job_completed_at TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE provider_services (
    provider_id UUID NOT NULL REFERENCES provider_profiles (user_id) ON DELETE CASCADE,
    service_id  UUID NOT NULL REFERENCES services (id),
    PRIMARY KEY (provider_id, service_id)
);

CREATE INDEX provider_services_service_idx ON provider_services (service_id);

CREATE TABLE provider_coverage (
    category_id UUID   NOT NULL REFERENCES service_categories (id),
    h3_r6       BIGINT NOT NULL,
    provider_id UUID   NOT NULL,
    PRIMARY KEY (category_id, h3_r6, provider_id)
);

CREATE INDEX provider_coverage_provider_idx ON provider_coverage (provider_id);

CREATE TABLE device_tokens (
    token      TEXT        PRIMARY KEY,
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    platform   TEXT        NOT NULL CHECK (platform IN ('android', 'ios')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX device_tokens_user_idx ON device_tokens (user_id);
