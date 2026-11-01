ALTER TABLE providers
    ADD COLUMN vehicle_plate       TEXT NOT NULL DEFAULT '',
    ADD COLUMN vehicle_description TEXT NOT NULL DEFAULT '',
    ADD COLUMN vehicle_brand       TEXT NOT NULL DEFAULT '',
    ADD COLUMN vehicle_model       TEXT NOT NULL DEFAULT '',
    ADD COLUMN vehicle_year        INTEGER,
    ADD COLUMN vehicle_color       TEXT NOT NULL DEFAULT '',
    ADD COLUMN vehicle_seats       INTEGER;

UPDATE providers p
SET vehicle_plate = v.plate, vehicle_description = trim(v.brand || ' ' || v.model || ' ' || v.color), vehicle_brand = v.brand, vehicle_model = v.model,
    vehicle_year = v.year, vehicle_color = v.color, vehicle_seats = v.seats
FROM vehicles v WHERE v.provider_id = p.user_id;

DROP TABLE vehicles;

ALTER TABLE service_provider_requirements RENAME TO service_onboarding;

ALTER INDEX providers_review_idx RENAME TO provider_profiles_review_idx;
ALTER INDEX providers_service_idx RENAME TO provider_profiles_service_idx;
ALTER TABLE providers RENAME CONSTRAINT providers_approved_has_service TO provider_profiles_approved_has_service;
ALTER TABLE providers RENAME CONSTRAINT providers_status_check TO provider_profiles_status_check;
ALTER TABLE providers RENAME TO provider_profiles;
