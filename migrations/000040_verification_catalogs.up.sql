CREATE TABLE banks (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    code          CHAR(4)     NOT NULL UNIQUE CHECK (code ~ '^[0-9]{4}$'),
    short_name    TEXT        NOT NULL,
    name          TEXT        NOT NULL,
    is_active     BOOLEAN     NOT NULL DEFAULT true,
    display_order INTEGER     NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO banks (code, short_name, name, display_order) VALUES
    ('0040', 'BAI', 'Banco Angolano de Investimentos', 1),
    ('0006', 'BFA', 'Banco de Fomento Angola', 2),
    ('0051', 'BIC', 'Banco BIC', 3),
    ('0055', 'Millennium Atlântico', 'Banco Millennium Atlântico', 4),
    ('0010', 'BPC', 'Banco de Poupança e Crédito', 5),
    ('0060', 'Standard Bank', 'Standard Bank Angola', 6),
    ('0044', 'Banco Sol', 'Banco Sol', 7),
    ('0047', 'Banco Keve', 'Banco Keve', 8),
    ('0052', 'BNI', 'Banco de Negócios Internacional', 9);

CREATE TABLE insurers (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT        NOT NULL UNIQUE,
    is_active     BOOLEAN     NOT NULL DEFAULT true,
    display_order INTEGER     NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO insurers (name, display_order) VALUES
    ('ENSA', 1),
    ('Fidelidade Angola', 2),
    ('NOSSA Seguros', 3),
    ('Global Seguros', 4),
    ('Sanlam Angola', 5),
    ('Protteja', 6),
    ('Garantia', 7),
    ('Mundial', 8);

ALTER TABLE service_provider_requirements
    ADD COLUMN verification_kind     provider_verification_kind NOT NULL DEFAULT 'driver',
    ADD COLUMN driving_categories    TEXT[]                     NOT NULL DEFAULT '{}',
    ADD COLUMN max_vehicle_age_years INTEGER                    CHECK (max_vehicle_age_years > 0);

UPDATE service_provider_requirements r
SET driving_categories = d.categories
FROM services s
JOIN (VALUES
    ('taxi', ARRAY['B']),
    ('taxi-comfort', ARRAY['B']),
    ('taxi-xl', ARRAY['B']),
    ('towing', ARRAY['C1', 'C']),
    ('removals', ARRAY['C1', 'C'])
) AS d (slug, categories) ON d.slug = s.slug
WHERE r.service_id = s.id;

UPDATE service_provider_requirements r
SET verification_kind = 'workshop', self_signup = true
FROM services s
WHERE r.service_id = s.id AND s.slug = 'maintenance';

DELETE FROM service_required_documents;

INSERT INTO service_required_documents (service_id, document_type)
SELECT r.service_id, d.document_type::provider_document_type
FROM service_provider_requirements r
JOIN (VALUES
    ('driver', 'id_card'),
    ('driver', 'selfie'),
    ('driver', 'driving_license'),
    ('driver', 'criminal_record'),
    ('driver', 'vehicle_photo'),
    ('driver', 'vehicle_registration'),
    ('driver', 'vehicle_insurance'),
    ('driver', 'vehicle_inspection'),
    ('driver', 'bank_account'),
    ('workshop', 'business_license'),
    ('workshop', 'commercial_certificate'),
    ('workshop', 'tax_id'),
    ('workshop', 'workshop_photo'),
    ('workshop', 'bank_account')
) AS d (kind, document_type) ON d.kind = r.verification_kind::text;

CREATE TABLE verification_reminders (
    id          UUID               PRIMARY KEY,
    provider_id UUID               NOT NULL REFERENCES providers (user_id) ON DELETE CASCADE,
    kind        TEXT               NOT NULL CHECK (kind IN ('criminal_record', 'document_expiry')),
    document_id UUID               REFERENCES provider_documents (id) ON DELETE CASCADE,
    expires_on  DATE,
    due_at      TIMESTAMPTZ        NOT NULL,
    sent_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ        NOT NULL DEFAULT now(),
    CONSTRAINT verification_reminders_expiry_has_document CHECK (kind <> 'document_expiry' OR (document_id IS NOT NULL AND expires_on IS NOT NULL))
);

CREATE UNIQUE INDEX verification_reminders_expiry_key ON verification_reminders (document_id, expires_on) WHERE kind = 'document_expiry';
CREATE UNIQUE INDEX verification_reminders_pending_key ON verification_reminders (provider_id, kind) WHERE kind = 'criminal_record' AND sent_at IS NULL;
CREATE INDEX verification_reminders_due_idx ON verification_reminders (due_at) WHERE sent_at IS NULL;
