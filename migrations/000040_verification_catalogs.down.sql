DROP TABLE verification_reminders;

DELETE FROM service_required_documents;

INSERT INTO service_required_documents (service_id, document_type)
SELECT r.service_id, d.document_type::provider_document_type
FROM service_provider_requirements r
JOIN (VALUES
    ('profile_photo', false),
    ('id_card', false),
    ('driving_license', false),
    ('criminal_record', false),
    ('vehicle_photo', true),
    ('vehicle_registration', true),
    ('vehicle_ownership', true),
    ('vehicle_insurance', true),
    ('vehicle_inspection', true)
) AS d (document_type, needs_vehicle) ON r.requires_vehicle OR NOT d.needs_vehicle;

UPDATE service_provider_requirements r
SET self_signup = false
FROM services s
WHERE r.service_id = s.id AND s.slug = 'maintenance';

ALTER TABLE service_provider_requirements
    DROP COLUMN max_vehicle_age_years,
    DROP COLUMN driving_categories,
    DROP COLUMN verification_kind;

DROP TABLE insurers;
DROP TABLE banks;
