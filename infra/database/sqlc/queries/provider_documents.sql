-- name: ListServicesOpenToProviders :many
SELECT s.id, s.slug, s.name, s.description, c.id AS category_id, c.slug AS category_slug, c.name AS category_name, o.requires_vehicle, o.min_vehicle_seats
FROM service_provider_requirements o
JOIN services s ON s.id = o.service_id
JOIN service_categories c ON c.id = s.category_id
WHERE s.is_active AND c.is_active AND o.self_signup
ORDER BY c.display_order, s.display_order;

-- name: GetProviderRequirements :one
SELECT o.service_id, o.requires_vehicle, o.min_vehicle_seats, o.vehicle_kinds, o.self_signup, s.name AS service_name
FROM service_provider_requirements o
JOIN services s ON s.id = o.service_id
JOIN service_categories c ON c.id = s.category_id
WHERE o.service_id = $1 AND s.is_active AND c.is_active;

-- name: ListRequiredDocuments :many
SELECT document_type FROM service_required_documents WHERE service_id = $1 ORDER BY document_type;

-- name: ListProviderDocuments :many
SELECT * FROM provider_documents WHERE provider_id = $1 ORDER BY document_type, subject;

-- name: GetProviderDocument :one
SELECT * FROM provider_documents WHERE provider_id = $1 AND document_type = $2 AND subject = $3;

-- name: GetProviderDocumentByID :one
SELECT * FROM provider_documents WHERE id = $1;

-- name: SaveProviderDocument :exec
INSERT INTO provider_documents (id, provider_id, document_type, subject, number, issued_at, expires_at, details, review_flags, status, rejection_reason, reviewed_by, reviewed_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT (id) DO UPDATE
SET number = EXCLUDED.number, issued_at = EXCLUDED.issued_at, expires_at = EXCLUDED.expires_at, details = EXCLUDED.details, review_flags = EXCLUDED.review_flags,
    status = EXCLUDED.status, rejection_reason = EXCLUDED.rejection_reason, reviewed_by = EXCLUDED.reviewed_by, reviewed_at = EXCLUDED.reviewed_at, updated_at = EXCLUDED.updated_at;

-- name: ListProviderDocumentFiles :many
SELECT * FROM provider_document_files WHERE provider_id = $1 AND status <> 'superseded' ORDER BY slot, version;

-- name: ListDocumentFiles :many
SELECT * FROM provider_document_files WHERE document_id = $1 AND status <> 'superseded' ORDER BY slot, version;

-- name: GetProviderDocumentFile :one
SELECT * FROM provider_document_files WHERE id = $1 AND provider_id = $2;

-- name: GetDocumentFile :one
SELECT * FROM provider_document_files WHERE id = $1;

-- name: NextDocumentFileVersion :one
SELECT (COALESCE(MAX(version), 0) + 1)::integer FROM provider_document_files WHERE document_id = $1 AND slot = $2;

-- name: SaveDocumentFile :exec
INSERT INTO provider_document_files (id, document_id, provider_id, slot, version, status, object_key, content_type, size_bytes, sha256, width, height, quality_checks, rejection_codes, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
ON CONFLICT (id) DO UPDATE
SET status = EXCLUDED.status, content_type = EXCLUDED.content_type, size_bytes = EXCLUDED.size_bytes, sha256 = EXCLUDED.sha256, width = EXCLUDED.width,
    height = EXCLUDED.height, quality_checks = EXCLUDED.quality_checks, rejection_codes = EXCLUDED.rejection_codes, updated_at = EXCLUDED.updated_at;

-- name: SupersedeDocumentFiles :exec
UPDATE provider_document_files SET status = 'superseded', updated_at = sqlc.arg(now)::timestamptz
WHERE document_id = sqlc.arg(document_id) AND slot = sqlc.arg(slot) AND id <> sqlc.arg(keep_id) AND status IN ('accepted', 'quality_failed', 'processing');

-- name: ListStalePendingFiles :many
SELECT * FROM provider_document_files WHERE status = 'pending_upload' AND created_at < sqlc.arg(before)::timestamptz ORDER BY created_at LIMIT sqlc.arg(max_rows);

-- name: DeleteDocumentFile :exec
DELETE FROM provider_document_files WHERE id = $1 AND status = 'pending_upload';

-- name: ListRejectionReasons :many
SELECT * FROM rejection_reasons
WHERE (sqlc.arg(scope)::text = '' OR scope = sqlc.arg(scope)::text) AND (sqlc.arg(include_inactive)::boolean OR is_active)
ORDER BY scope, display_order, label;

-- name: ListActiveRejectionReasonsByIDs :many
SELECT * FROM rejection_reasons
WHERE is_active AND scope = sqlc.arg(scope)::text AND id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY display_order, label;

-- name: GetRejectionReason :one
SELECT * FROM rejection_reasons WHERE id = $1;

-- name: CreateRejectionReason :exec
INSERT INTO rejection_reasons (id, scope, code, label, requires_note, is_active, display_order, category_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: UpdateRejectionReason :exec
UPDATE rejection_reasons SET label = $2, requires_note = $3, is_active = $4, display_order = $5, updated_at = now() WHERE id = $1;

-- name: ClearProviderRejectionReasons :exec
DELETE FROM provider_rejection_reasons WHERE provider_id = $1;

-- name: AddProviderRejectionReasons :exec
INSERT INTO provider_rejection_reasons (provider_id, reason_id) SELECT sqlc.arg(provider_id)::uuid, unnest(sqlc.arg(reason_ids)::uuid[]);

-- name: ListProviderRejectionReasons :many
SELECT r.* FROM provider_rejection_reasons pr JOIN rejection_reasons r ON r.id = pr.reason_id
WHERE pr.provider_id = $1 ORDER BY r.display_order, r.label;

-- name: ClearDocumentRejectionReasons :exec
DELETE FROM provider_document_rejection_reasons WHERE provider_id = $1 AND document_type = $2 AND subject = $3;

-- name: AddDocumentRejectionReasons :exec
INSERT INTO provider_document_rejection_reasons (provider_id, document_type, subject, reason_id)
SELECT sqlc.arg(provider_id)::uuid, sqlc.arg(document_type)::provider_document_type, sqlc.arg(subject)::provider_document_subject, unnest(sqlc.arg(reason_ids)::uuid[]);

-- name: ListDocumentRejectionReasons :many
SELECT d.id AS document_id, r.id, r.scope, r.code, r.label, r.requires_note, r.is_active, r.display_order, r.category_id
FROM provider_document_rejection_reasons dr
JOIN rejection_reasons r ON r.id = dr.reason_id
JOIN provider_documents d ON d.provider_id = dr.provider_id AND d.document_type = dr.document_type AND d.subject = dr.subject
WHERE dr.provider_id = $1 ORDER BY r.display_order, r.label;
