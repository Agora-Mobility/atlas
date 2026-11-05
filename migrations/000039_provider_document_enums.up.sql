CREATE TYPE provider_document_type AS ENUM (
    'profile_photo',
    'id_card',
    'selfie',
    'driving_license',
    'criminal_record',
    'vehicle_photo',
    'vehicle_registration',
    'vehicle_ownership',
    'vehicle_owner_declaration',
    'vehicle_insurance',
    'vehicle_inspection',
    'bank_account',
    'business_license',
    'commercial_certificate',
    'tax_id',
    'workshop_photo'
);

CREATE TYPE provider_document_slot AS ENUM (
    'main',
    'front',
    'back',
    'renewal_receipt',
    'page_1',
    'page_2',
    'rear',
    'side',
    'interior',
    'plate',
    'facade',
    'work_area',
    'equipment',
    'reception',
    'extra'
);

CREATE TYPE provider_document_subject AS ENUM ('provider', 'legal_representative', 'vehicle_owner');

CREATE TYPE provider_verification_kind AS ENUM ('driver', 'workshop');

CREATE TABLE provider_verifications (
    id                      UUID                       PRIMARY KEY,
    provider_id             UUID                       NOT NULL UNIQUE REFERENCES providers (user_id) ON DELETE CASCADE,
    kind                    provider_verification_kind NOT NULL,
    workshop                JSONB                      NOT NULL DEFAULT '{}',
    declaration_accepted_at TIMESTAMPTZ,
    submitted_at            TIMESTAMPTZ,
    created_at              TIMESTAMPTZ                NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ                NOT NULL DEFAULT now()
);

INSERT INTO provider_verifications (id, provider_id, kind, submitted_at)
SELECT gen_random_uuid(), p.user_id, 'driver', p.submitted_at
FROM providers p
WHERE EXISTS (SELECT 1 FROM provider_documents d WHERE d.provider_id = p.user_id);

ALTER TABLE provider_documents DROP CONSTRAINT provider_documents_document_type_fkey;
ALTER TABLE service_required_documents DROP CONSTRAINT service_required_documents_document_type_fkey;
ALTER TABLE provider_document_rejection_reasons DROP CONSTRAINT provider_document_rejection_reas_provider_id_document_type_fkey;

CREATE TABLE provider_document_files (
    id              UUID                   PRIMARY KEY,
    document_id     UUID                   NOT NULL REFERENCES provider_documents (id) ON DELETE CASCADE,
    provider_id     UUID                   NOT NULL REFERENCES providers (user_id) ON DELETE CASCADE,
    slot            provider_document_slot NOT NULL,
    version         INTEGER                NOT NULL CHECK (version > 0),
    status          TEXT                   NOT NULL CHECK (status IN ('pending_upload', 'processing', 'accepted', 'quality_failed', 'superseded')),
    object_key      TEXT                   NOT NULL UNIQUE,
    content_type    TEXT                   NOT NULL,
    size_bytes      BIGINT                 NOT NULL CHECK (size_bytes > 0),
    sha256          TEXT                   NOT NULL DEFAULT '',
    width           INTEGER,
    height          INTEGER,
    quality_checks  JSONB                  NOT NULL DEFAULT '{}',
    rejection_codes TEXT[]                 NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ            NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ            NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX provider_document_files_current_key ON provider_document_files (document_id, slot) WHERE status = 'accepted' AND slot <> 'extra';
CREATE INDEX provider_document_files_document_idx ON provider_document_files (document_id, slot, version DESC);
CREATE INDEX provider_document_files_pending_idx ON provider_document_files (created_at) WHERE status = 'pending_upload';

INSERT INTO provider_document_files (id, document_id, provider_id, slot, version, status, object_key, content_type, size_bytes, created_at, updated_at)
SELECT gen_random_uuid(), d.id, d.provider_id,
       CASE d.document_type
           WHEN 'vehicle_photo_front' THEN 'front'
           WHEN 'vehicle_photo_rear' THEN 'rear'
           WHEN 'vehicle_photo_side' THEN 'side'
           WHEN 'vehicle_photo_interior' THEN 'interior'
           ELSE 'main'
       END::provider_document_slot,
       1, 'accepted', d.object_key, d.content_type, d.size_bytes, d.created_at, d.updated_at
FROM provider_documents d;

CREATE TEMPORARY TABLE vehicle_photo_keepers ON COMMIT DROP AS
SELECT DISTINCT ON (provider_id) provider_id, id AS keeper_id
FROM provider_documents
WHERE document_type LIKE 'vehicle_photo_%'
ORDER BY provider_id, created_at, id;

UPDATE provider_document_files f
SET document_id = k.keeper_id
FROM provider_documents d
JOIN vehicle_photo_keepers k ON k.provider_id = d.provider_id
WHERE f.document_id = d.id AND d.document_type LIKE 'vehicle_photo_%';

UPDATE provider_documents d
SET document_type = 'vehicle_photo',
    status = CASE
        WHEN bool_or_rejected THEN 'rejected'
        WHEN bool_and_approved THEN 'approved'
        ELSE 'uploaded'
    END,
    rejection_reason = CASE WHEN bool_or_rejected THEN first_reason ELSE '' END
FROM (
    SELECT provider_id,
           bool_or(status = 'rejected') AS bool_or_rejected,
           bool_and(status = 'approved') AS bool_and_approved,
           (array_agg(rejection_reason ORDER BY created_at) FILTER (WHERE status = 'rejected'))[1] AS first_reason
    FROM provider_documents
    WHERE document_type LIKE 'vehicle_photo_%'
    GROUP BY provider_id
) agg
JOIN vehicle_photo_keepers k ON k.provider_id = agg.provider_id
WHERE d.id = k.keeper_id;

DELETE FROM provider_document_rejection_reasons WHERE document_type LIKE 'vehicle_photo_%';
DELETE FROM provider_documents WHERE document_type LIKE 'vehicle_photo_%';

ALTER TABLE provider_documents DROP CONSTRAINT provider_documents_provider_id_document_type_key;

ALTER TABLE provider_documents
    ALTER COLUMN document_type TYPE provider_document_type USING document_type::provider_document_type,
    ADD COLUMN subject      provider_document_subject NOT NULL DEFAULT 'provider',
    ADD COLUMN number       TEXT                      NOT NULL DEFAULT '',
    ADD COLUMN issued_at    DATE,
    ADD COLUMN expires_at   DATE,
    ADD COLUMN details      JSONB                     NOT NULL DEFAULT '{}',
    ADD COLUMN review_flags TEXT[]                    NOT NULL DEFAULT '{}',
    DROP COLUMN object_key,
    DROP COLUMN content_type,
    DROP COLUMN size_bytes,
    ADD CONSTRAINT provider_documents_provider_type_subject_key UNIQUE (provider_id, document_type, subject);

CREATE INDEX provider_documents_expiry_idx ON provider_documents (expires_at) WHERE expires_at IS NOT NULL;

ALTER TABLE provider_document_rejection_reasons
    DROP CONSTRAINT provider_document_rejection_reasons_pkey,
    ALTER COLUMN document_type TYPE provider_document_type USING document_type::provider_document_type,
    ADD COLUMN subject provider_document_subject NOT NULL DEFAULT 'provider',
    ADD PRIMARY KEY (provider_id, document_type, subject, reason_id),
    ADD CONSTRAINT provider_document_rejection_reasons_document_fkey FOREIGN KEY (provider_id, document_type, subject) REFERENCES provider_documents (provider_id, document_type, subject) ON DELETE CASCADE;

DELETE FROM service_required_documents a
USING service_required_documents b
WHERE a.service_id = b.service_id AND a.document_type LIKE 'vehicle_photo_%' AND b.document_type LIKE 'vehicle_photo_%' AND a.document_type > b.document_type;

UPDATE service_required_documents SET document_type = 'vehicle_photo' WHERE document_type LIKE 'vehicle_photo_%';

ALTER TABLE service_required_documents
    ALTER COLUMN document_type TYPE provider_document_type USING document_type::provider_document_type;

DROP TABLE provider_document_types;
