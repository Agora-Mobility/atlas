ALTER TABLE provider_profiles DROP CONSTRAINT provider_profiles_status_check;

UPDATE provider_profiles SET status = 'draft' WHERE status = 'pending';

ALTER TABLE provider_profiles
    ALTER COLUMN status SET DEFAULT 'draft',
    ADD CONSTRAINT provider_profiles_status_check CHECK (status IN ('draft', 'pending', 'approved', 'rejected', 'suspended')),
    ADD COLUMN requested_service_id UUID REFERENCES services (id),
    ADD COLUMN birth_date           DATE,
    ADD COLUMN id_number            TEXT NOT NULL DEFAULT '',
    ADD COLUMN address              TEXT NOT NULL DEFAULT '',
    ADD COLUMN vehicle_brand        TEXT NOT NULL DEFAULT '',
    ADD COLUMN vehicle_model        TEXT NOT NULL DEFAULT '',
    ADD COLUMN vehicle_year         INTEGER,
    ADD COLUMN vehicle_color        TEXT NOT NULL DEFAULT '',
    ADD COLUMN vehicle_seats        INTEGER,
    ADD COLUMN status_reason        TEXT NOT NULL DEFAULT '',
    ADD COLUMN submitted_at         TIMESTAMPTZ,
    ADD COLUMN reviewed_at          TIMESTAMPTZ;

CREATE INDEX provider_profiles_review_idx ON provider_profiles (status, submitted_at);

CREATE TABLE provider_document_types (
    code                  TEXT    PRIMARY KEY,
    name                  TEXT    NOT NULL,
    scope                 TEXT    NOT NULL CHECK (scope IN ('personal', 'vehicle')),
    allowed_content_types TEXT[]  NOT NULL,
    max_size_bytes        BIGINT  NOT NULL CHECK (max_size_bytes > 0),
    display_order         INTEGER NOT NULL DEFAULT 0
);

INSERT INTO provider_document_types (code, name, scope, allowed_content_types, max_size_bytes, display_order)
SELECT d.code, d.name, d.scope, d.types, 10485760, d.position
FROM (VALUES
    ('profile_photo', 'Foto de perfil', 'personal', ARRAY['image/jpeg', 'image/png', 'image/webp'], 1),
    ('id_card', 'Bilhete de identidade', 'personal', ARRAY['image/jpeg', 'image/png', 'image/webp', 'application/pdf'], 2),
    ('driving_license', 'Carta de condução', 'personal', ARRAY['image/jpeg', 'image/png', 'image/webp', 'application/pdf'], 3),
    ('criminal_record', 'Registo criminal', 'personal', ARRAY['image/jpeg', 'image/png', 'image/webp', 'application/pdf'], 4),
    ('vehicle_registration', 'Livrete', 'vehicle', ARRAY['image/jpeg', 'image/png', 'image/webp', 'application/pdf'], 5),
    ('vehicle_ownership', 'Título de propriedade', 'vehicle', ARRAY['image/jpeg', 'image/png', 'image/webp', 'application/pdf'], 6),
    ('vehicle_insurance', 'Seguro', 'vehicle', ARRAY['image/jpeg', 'image/png', 'image/webp', 'application/pdf'], 7),
    ('vehicle_inspection', 'Inspeção', 'vehicle', ARRAY['image/jpeg', 'image/png', 'image/webp', 'application/pdf'], 8),
    ('vehicle_photo_front', 'Foto da frente', 'vehicle', ARRAY['image/jpeg', 'image/png', 'image/webp'], 9),
    ('vehicle_photo_rear', 'Foto da traseira', 'vehicle', ARRAY['image/jpeg', 'image/png', 'image/webp'], 10),
    ('vehicle_photo_side', 'Foto da lateral', 'vehicle', ARRAY['image/jpeg', 'image/png', 'image/webp'], 11),
    ('vehicle_photo_interior', 'Foto do interior', 'vehicle', ARRAY['image/jpeg', 'image/png', 'image/webp'], 12)
) AS d (code, name, scope, types, position);

CREATE TABLE service_onboarding (
    service_id        UUID    PRIMARY KEY REFERENCES services (id),
    requires_vehicle  BOOLEAN NOT NULL DEFAULT true,
    min_vehicle_seats INTEGER NOT NULL DEFAULT 0 CHECK (min_vehicle_seats >= 0)
);

CREATE TABLE service_required_documents (
    service_id    UUID NOT NULL REFERENCES service_onboarding (service_id) ON DELETE CASCADE,
    document_type TEXT NOT NULL REFERENCES provider_document_types (code),
    PRIMARY KEY (service_id, document_type)
);

CREATE TABLE provider_documents (
    id               UUID        PRIMARY KEY,
    provider_id      UUID        NOT NULL REFERENCES provider_profiles (user_id) ON DELETE CASCADE,
    document_type    TEXT        NOT NULL REFERENCES provider_document_types (code),
    object_key       TEXT        NOT NULL,
    content_type     TEXT        NOT NULL,
    size_bytes       BIGINT      NOT NULL,
    status           TEXT        NOT NULL CHECK (status IN ('uploaded', 'approved', 'rejected')),
    rejection_reason TEXT        NOT NULL DEFAULT '',
    reviewed_by      UUID,
    reviewed_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider_id, document_type)
);

UPDATE services SET name = 'Económico', description = 'Viatura ligeira até 4 passageiros.' WHERE slug = 'taxi';

INSERT INTO services (category_id, slug, name, description, icon, requires_destination, display_order)
SELECT c.id, s.slug, s.name, s.description, 'taxi', true, s.position
FROM (VALUES
    ('taxi-comfort', 'Conforto', 'Viaturas recentes, até 4 passageiros.', 2),
    ('taxi-xl', 'XL', 'Viatura grande, até 6 passageiros.', 3)
) AS s (slug, name, description, position)
JOIN service_categories c ON c.slug = 'taxi'
ON CONFLICT (slug) DO UPDATE SET is_active = true;

INSERT INTO service_onboarding (service_id, requires_vehicle, min_vehicle_seats)
SELECT s.id, true, CASE s.slug WHEN 'taxi-xl' THEN 7 ELSE 4 END
FROM services s WHERE s.slug IN ('taxi', 'taxi-comfort', 'taxi-xl');

INSERT INTO service_required_documents (service_id, document_type)
SELECT o.service_id, d.code FROM service_onboarding o CROSS JOIN provider_document_types d;
