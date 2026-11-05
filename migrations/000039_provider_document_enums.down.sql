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

ALTER TABLE service_required_documents ALTER COLUMN document_type TYPE TEXT USING document_type::text;

INSERT INTO service_required_documents (service_id, document_type)
SELECT r.service_id, p.code
FROM service_required_documents r
CROSS JOIN (VALUES ('vehicle_photo_front'), ('vehicle_photo_rear'), ('vehicle_photo_side'), ('vehicle_photo_interior')) AS p (code)
WHERE r.document_type = 'vehicle_photo';

DELETE FROM service_required_documents WHERE document_type NOT IN (SELECT code FROM provider_document_types);

ALTER TABLE service_required_documents
    ADD CONSTRAINT service_required_documents_document_type_fkey FOREIGN KEY (document_type) REFERENCES provider_document_types (code);

ALTER TABLE provider_document_rejection_reasons
    DROP CONSTRAINT provider_document_rejection_reasons_document_fkey,
    DROP CONSTRAINT provider_document_rejection_reasons_pkey;

DELETE FROM provider_document_rejection_reasons
WHERE subject <> 'provider' OR document_type::text NOT IN (SELECT code FROM provider_document_types);

ALTER TABLE provider_document_rejection_reasons
    DROP COLUMN subject,
    ALTER COLUMN document_type TYPE TEXT USING document_type::text,
    ADD PRIMARY KEY (provider_id, document_type, reason_id);

ALTER TABLE provider_documents
    DROP CONSTRAINT provider_documents_provider_type_subject_key,
    ALTER COLUMN document_type TYPE TEXT USING document_type::text,
    ADD COLUMN object_key   TEXT,
    ADD COLUMN content_type TEXT,
    ADD COLUMN size_bytes   BIGINT;

DROP INDEX provider_documents_expiry_idx;

INSERT INTO provider_documents (id, provider_id, document_type, subject, object_key, content_type, size_bytes, status, rejection_reason, reviewed_by, reviewed_at, created_at, updated_at)
SELECT f.id, d.provider_id, 'vehicle_photo_' || f.slot::text, 'provider', f.object_key, f.content_type, f.size_bytes, d.status, d.rejection_reason, d.reviewed_by, d.reviewed_at, f.created_at, f.updated_at
FROM provider_documents d
JOIN provider_document_files f ON f.document_id = d.id AND f.status = 'accepted' AND f.slot IN ('front', 'rear', 'side', 'interior')
WHERE d.document_type = 'vehicle_photo' AND d.subject = 'provider';

UPDATE provider_documents d
SET object_key = f.object_key, content_type = f.content_type, size_bytes = f.size_bytes
FROM (
    SELECT DISTINCT ON (document_id) document_id, object_key, content_type, size_bytes
    FROM provider_document_files
    WHERE status IN ('accepted', 'processing')
    ORDER BY document_id, (status = 'accepted') DESC, version DESC
) f
WHERE f.document_id = d.id AND d.object_key IS NULL;

DELETE FROM provider_documents
WHERE object_key IS NULL OR subject <> 'provider' OR document_type NOT IN (SELECT code FROM provider_document_types);

DROP TABLE provider_document_files;

ALTER TABLE provider_documents
    DROP COLUMN subject,
    DROP COLUMN number,
    DROP COLUMN issued_at,
    DROP COLUMN expires_at,
    DROP COLUMN details,
    DROP COLUMN review_flags,
    ALTER COLUMN object_key SET NOT NULL,
    ALTER COLUMN content_type SET NOT NULL,
    ALTER COLUMN size_bytes SET NOT NULL,
    ADD CONSTRAINT provider_documents_provider_id_document_type_key UNIQUE (provider_id, document_type),
    ADD CONSTRAINT provider_documents_document_type_fkey FOREIGN KEY (document_type) REFERENCES provider_document_types (code);

DELETE FROM provider_document_rejection_reasons r
WHERE NOT EXISTS (SELECT 1 FROM provider_documents d WHERE d.provider_id = r.provider_id AND d.document_type = r.document_type);

ALTER TABLE provider_document_rejection_reasons
    ADD CONSTRAINT provider_document_rejection_reas_provider_id_document_type_fkey
        FOREIGN KEY (provider_id, document_type) REFERENCES provider_documents (provider_id, document_type) ON DELETE CASCADE;

DROP TABLE provider_verifications;

DROP TYPE provider_verification_kind;
DROP TYPE provider_document_subject;
DROP TYPE provider_document_slot;
DROP TYPE provider_document_type;
