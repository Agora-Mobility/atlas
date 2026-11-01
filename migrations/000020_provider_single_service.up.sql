ALTER TABLE provider_profiles ADD COLUMN service_id UUID REFERENCES services (id);

UPDATE provider_profiles p
SET service_id = (
    SELECT ps.service_id
    FROM provider_services ps
    JOIN services s ON s.id = ps.service_id
    JOIN service_categories c ON c.id = s.category_id
    WHERE ps.provider_id = p.user_id
    ORDER BY c.display_order, s.display_order, s.slug
    LIMIT 1
);

UPDATE provider_profiles SET status = 'pending' WHERE status = 'approved' AND service_id IS NULL;

ALTER TABLE provider_profiles
    ADD CONSTRAINT provider_profiles_approved_has_service CHECK (status <> 'approved' OR service_id IS NOT NULL);

CREATE INDEX provider_profiles_service_idx ON provider_profiles (service_id);

DROP TABLE provider_services;
