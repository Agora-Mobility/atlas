-- name: ListServicesOpenToProviders :many
SELECT s.id, s.slug, s.name, s.description, c.id AS category_id, c.slug AS category_slug, c.name AS category_name, o.requires_vehicle, o.min_vehicle_seats
FROM service_provider_requirements o
JOIN services s ON s.id = o.service_id
JOIN service_categories c ON c.id = s.category_id
WHERE s.is_active AND c.is_active
ORDER BY c.display_order, s.display_order;

-- name: GetProviderRequirements :one
SELECT o.service_id, o.requires_vehicle, o.min_vehicle_seats, s.name AS service_name
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
