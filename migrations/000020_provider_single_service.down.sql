CREATE TABLE provider_services (
    provider_id UUID NOT NULL REFERENCES provider_profiles (user_id) ON DELETE CASCADE,
    service_id  UUID NOT NULL REFERENCES services (id),
    PRIMARY KEY (provider_id, service_id)
);

CREATE INDEX provider_services_service_idx ON provider_services (service_id);

INSERT INTO provider_services (provider_id, service_id)
SELECT user_id, service_id FROM provider_profiles WHERE service_id IS NOT NULL;

ALTER TABLE provider_profiles DROP CONSTRAINT provider_profiles_approved_has_service;
ALTER TABLE provider_profiles DROP COLUMN service_id;
