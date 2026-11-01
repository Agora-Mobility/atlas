DROP TABLE provider_documents;
DROP TABLE service_required_documents;
DROP TABLE service_onboarding;
DROP TABLE provider_document_types;

UPDATE services SET is_active = false WHERE slug IN ('taxi-comfort', 'taxi-xl');
UPDATE services SET name = 'Táxi', description = 'Viagem até ao destino indicado.' WHERE slug = 'taxi';

DROP INDEX provider_profiles_review_idx;

UPDATE provider_profiles SET status = 'pending' WHERE status IN ('draft', 'rejected');

ALTER TABLE provider_profiles
    DROP CONSTRAINT provider_profiles_status_check,
    ALTER COLUMN status SET DEFAULT 'pending',
    ADD CONSTRAINT provider_profiles_status_check CHECK (status IN ('pending', 'approved', 'suspended')),
    DROP COLUMN requested_service_id,
    DROP COLUMN birth_date,
    DROP COLUMN id_number,
    DROP COLUMN address,
    DROP COLUMN vehicle_brand,
    DROP COLUMN vehicle_model,
    DROP COLUMN vehicle_year,
    DROP COLUMN vehicle_color,
    DROP COLUMN vehicle_seats,
    DROP COLUMN status_reason,
    DROP COLUMN submitted_at,
    DROP COLUMN reviewed_at;
