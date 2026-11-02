CREATE TABLE providers (
    user_id               UUID        PRIMARY KEY REFERENCES users (id),
    status                TEXT        NOT NULL DEFAULT 'draft',
    organization_id       UUID        REFERENCES organizations (id),
    requested_service_id  UUID        REFERENCES services (id),
    service_id            UUID        REFERENCES services (id),
    birth_date            DATE,
    id_number             TEXT        NOT NULL DEFAULT '',
    home_address_id       UUID        REFERENCES addresses (id),
    base_address_id       UUID        REFERENCES addresses (id),
    service_radius_km     DOUBLE PRECISION,
    status_reason         TEXT        NOT NULL DEFAULT '',
    submitted_at          TIMESTAMPTZ,
    reviewed_at           TIMESTAMPTZ,
    last_job_completed_at TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT providers_status_check CHECK (status IN ('draft', 'pending', 'approved', 'rejected', 'suspended')),
    CONSTRAINT providers_approved_has_service CHECK (status <> 'approved' OR service_id IS NOT NULL)
);

CREATE INDEX providers_service_idx ON providers (service_id);
CREATE INDEX providers_organization_idx ON providers (organization_id);
CREATE INDEX providers_review_idx ON providers (status, submitted_at);

CREATE TABLE vehicles (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id     UUID        UNIQUE REFERENCES providers (user_id) ON DELETE SET NULL,
    organization_id UUID        REFERENCES organizations (id),
    kind            TEXT        NOT NULL DEFAULT 'car' CHECK (kind IN ('car', 'motorcycle', 'van', 'truck', 'tow_truck')),
    brand           TEXT        NOT NULL,
    model           TEXT        NOT NULL,
    year            INTEGER,
    color           TEXT        NOT NULL,
    plate           TEXT        NOT NULL,
    seats           INTEGER,
    capacity_kg     INTEGER,
    is_active       BOOLEAN     NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX vehicles_plate_idx ON vehicles (plate);
CREATE INDEX vehicles_organization_idx ON vehicles (organization_id);
CREATE UNIQUE INDEX vehicles_organization_plate_key ON vehicles (organization_id, plate) WHERE organization_id IS NOT NULL;

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

CREATE TABLE service_provider_requirements (
    service_id        UUID    PRIMARY KEY REFERENCES services (id),
    requires_vehicle  BOOLEAN NOT NULL DEFAULT true,
    min_vehicle_seats INTEGER NOT NULL DEFAULT 0 CHECK (min_vehicle_seats >= 0),
    vehicle_kinds     TEXT[]  NOT NULL DEFAULT '{}',
    self_signup       BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE service_required_documents (
    service_id    UUID NOT NULL REFERENCES service_provider_requirements (service_id) ON DELETE CASCADE,
    document_type TEXT NOT NULL REFERENCES provider_document_types (code),
    PRIMARY KEY (service_id, document_type)
);

CREATE TABLE provider_documents (
    id               UUID        PRIMARY KEY,
    provider_id      UUID        NOT NULL REFERENCES providers (user_id) ON DELETE CASCADE,
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

CREATE TABLE rejection_reasons (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    scope         TEXT        NOT NULL CHECK (scope IN ('document', 'provider', 'suspension')),
    code          TEXT        NOT NULL,
    label         TEXT        NOT NULL,
    requires_note BOOLEAN     NOT NULL DEFAULT false,
    is_active     BOOLEAN     NOT NULL DEFAULT true,
    display_order INTEGER     NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (scope, code)
);

CREATE TABLE provider_rejection_reasons (
    provider_id UUID NOT NULL REFERENCES providers (user_id) ON DELETE CASCADE,
    reason_id   UUID NOT NULL REFERENCES rejection_reasons (id),
    PRIMARY KEY (provider_id, reason_id)
);

CREATE TABLE provider_document_rejection_reasons (
    provider_id   UUID NOT NULL,
    document_type TEXT NOT NULL,
    reason_id     UUID NOT NULL REFERENCES rejection_reasons (id),
    PRIMARY KEY (provider_id, document_type, reason_id),
    FOREIGN KEY (provider_id, document_type) REFERENCES provider_documents (provider_id, document_type) ON DELETE CASCADE
);

INSERT INTO rejection_reasons (scope, code, label, requires_note, display_order) VALUES
    ('document', 'illegible', 'Documento ilegível', false, 1),
    ('document', 'expired', 'Documento caducado', false, 2),
    ('document', 'wrong_person', 'O documento não corresponde à pessoa', false, 3),
    ('document', 'wrong_vehicle', 'O documento não corresponde à viatura', false, 4),
    ('document', 'incomplete', 'Documento incompleto', false, 5),
    ('document', 'other', 'Outro motivo', true, 99),
    ('provider', 'invalid_documents', 'Documentos inválidos', false, 1),
    ('provider', 'inconsistent_data', 'Dados inconsistentes', false, 2),
    ('provider', 'vehicle_not_eligible', 'A viatura não cumpre os requisitos', false, 3),
    ('provider', 'criminal_record', 'Registo criminal impeditivo', false, 4),
    ('provider', 'other', 'Outro motivo', true, 99),
    ('suspension', 'customer_complaints', 'Queixas de clientes', false, 1),
    ('suspension', 'expired_documents', 'Documentos caducados', false, 2),
    ('suspension', 'misconduct', 'Conduta inadequada', false, 3),
    ('suspension', 'other', 'Outro motivo', true, 99);

INSERT INTO service_provider_requirements (service_id, requires_vehicle, min_vehicle_seats, vehicle_kinds, self_signup)
SELECT s.id, r.requires_vehicle, r.seats, r.kinds, r.self_signup
FROM services s
JOIN (VALUES
    ('taxi', true, 4, ARRAY['car'], true),
    ('taxi-comfort', true, 4, ARRAY['car'], true),
    ('taxi-xl', true, 7, ARRAY['car', 'van'], true),
    ('towing', true, 0, ARRAY['tow_truck'], false),
    ('removals', true, 0, ARRAY['van', 'truck'], false),
    ('maintenance', false, 0, ARRAY[]::text[], false)
) AS r (slug, requires_vehicle, seats, kinds, self_signup) ON r.slug = s.slug;

INSERT INTO service_required_documents (service_id, document_type)
SELECT r.service_id, d.code
FROM service_provider_requirements r
CROSS JOIN provider_document_types d
WHERE r.requires_vehicle OR d.scope = 'personal';
