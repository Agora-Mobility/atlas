ALTER TABLE provider_profiles RENAME TO providers;
ALTER TABLE providers RENAME CONSTRAINT provider_profiles_status_check TO providers_status_check;
ALTER TABLE providers RENAME CONSTRAINT provider_profiles_approved_has_service TO providers_approved_has_service;
ALTER INDEX provider_profiles_service_idx RENAME TO providers_service_idx;
ALTER INDEX provider_profiles_review_idx RENAME TO providers_review_idx;

ALTER TABLE service_onboarding RENAME TO service_provider_requirements;

CREATE TABLE vehicles (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id UUID        NOT NULL UNIQUE REFERENCES providers (user_id) ON DELETE CASCADE,
    brand       TEXT        NOT NULL,
    model       TEXT        NOT NULL,
    year        INTEGER,
    color       TEXT        NOT NULL,
    plate       TEXT        NOT NULL,
    seats       INTEGER,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX vehicles_plate_idx ON vehicles (plate);

INSERT INTO vehicles (provider_id, brand, model, year, color, plate, seats)
SELECT user_id, COALESCE(NULLIF(vehicle_brand, ''), vehicle_description), vehicle_model, vehicle_year, vehicle_color, vehicle_plate, vehicle_seats
FROM providers
WHERE vehicle_plate <> '';

ALTER TABLE providers
    DROP COLUMN vehicle_plate,
    DROP COLUMN vehicle_description,
    DROP COLUMN vehicle_brand,
    DROP COLUMN vehicle_model,
    DROP COLUMN vehicle_year,
    DROP COLUMN vehicle_color,
    DROP COLUMN vehicle_seats;
