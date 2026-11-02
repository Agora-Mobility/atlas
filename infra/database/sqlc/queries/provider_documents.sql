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
SELECT t.code, t.name, t.scope, t.allowed_content_types, t.max_size_bytes
FROM service_required_documents r
JOIN provider_document_types t ON t.code = r.document_type
WHERE r.service_id = $1
ORDER BY t.display_order;

-- name: GetDocumentType :one
SELECT code, name, scope, allowed_content_types, max_size_bytes FROM provider_document_types WHERE code = $1;

-- name: ListProviderDocuments :many
SELECT d.id, d.provider_id, d.document_type, d.object_key, d.content_type, d.size_bytes, d.status, d.rejection_reason, d.reviewed_by, d.reviewed_at, d.created_at, d.updated_at, t.name AS type_name
FROM provider_documents d
JOIN provider_document_types t ON t.code = d.document_type
WHERE d.provider_id = $1
ORDER BY t.display_order;

-- name: GetProviderDocument :one
SELECT d.id, d.provider_id, d.document_type, d.object_key, d.content_type, d.size_bytes, d.status, d.rejection_reason, d.reviewed_by, d.reviewed_at, d.created_at, d.updated_at, t.name AS type_name
FROM provider_documents d
JOIN provider_document_types t ON t.code = d.document_type
WHERE d.provider_id = $1 AND d.document_type = $2;

-- name: SaveProviderDocument :exec
INSERT INTO provider_documents (id, provider_id, document_type, object_key, content_type, size_bytes, status, rejection_reason, reviewed_by, reviewed_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (provider_id, document_type) DO UPDATE
SET object_key = EXCLUDED.object_key, content_type = EXCLUDED.content_type, size_bytes = EXCLUDED.size_bytes, status = EXCLUDED.status,
    rejection_reason = EXCLUDED.rejection_reason, reviewed_by = EXCLUDED.reviewed_by, reviewed_at = EXCLUDED.reviewed_at, updated_at = EXCLUDED.updated_at;

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
INSERT INTO rejection_reasons (id, scope, code, label, requires_note, is_active, display_order) VALUES ($1, $2, $3, $4, $5, $6, $7);

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
DELETE FROM provider_document_rejection_reasons WHERE provider_id = $1 AND document_type = $2;

-- name: AddDocumentRejectionReasons :exec
INSERT INTO provider_document_rejection_reasons (provider_id, document_type, reason_id)
SELECT sqlc.arg(provider_id)::uuid, sqlc.arg(document_type)::text, unnest(sqlc.arg(reason_ids)::uuid[]);

-- name: ListDocumentRejectionReasons :many
SELECT dr.document_type, r.id, r.scope, r.code, r.label, r.requires_note, r.is_active, r.display_order
FROM provider_document_rejection_reasons dr JOIN rejection_reasons r ON r.id = dr.reason_id
WHERE dr.provider_id = $1 ORDER BY r.display_order, r.label;
