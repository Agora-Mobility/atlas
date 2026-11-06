-- name: GetProviderVerification :one
SELECT * FROM provider_verifications WHERE provider_id = $1;

-- name: SaveProviderVerification :exec
INSERT INTO provider_verifications (id, provider_id, kind, workshop, declaration_accepted_at, submitted_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO UPDATE
SET kind = EXCLUDED.kind, workshop = EXCLUDED.workshop, declaration_accepted_at = EXCLUDED.declaration_accepted_at,
    submitted_at = EXCLUDED.submitted_at, updated_at = EXCLUDED.updated_at;

-- name: ListBanks :many
SELECT * FROM banks
WHERE is_active
  AND (sqlc.arg(search)::text = '' OR name ILIKE '%' || sqlc.arg(search)::text || '%' OR short_name ILIKE '%' || sqlc.arg(search)::text || '%' OR code = sqlc.arg(search)::text)
ORDER BY display_order, short_name;

-- name: GetBank :one
SELECT * FROM banks WHERE id = $1 AND is_active;

-- name: GetBankByCode :one
SELECT * FROM banks WHERE code = $1 AND is_active;

-- name: ListInsurers :many
SELECT * FROM insurers
WHERE is_active AND (sqlc.arg(search)::text = '' OR name ILIKE '%' || sqlc.arg(search)::text || '%')
ORDER BY display_order, name;

-- name: GetInsurer :one
SELECT * FROM insurers WHERE id = $1 AND is_active;

-- name: ListWorkshopCategories :many
SELECT id, code, label, description FROM catalog_items WHERE kind = 'workshop_category' AND is_active ORDER BY display_order, label;

-- name: ScheduleVerificationReminder :exec
INSERT INTO verification_reminders (id, provider_id, kind, document_id, expires_on, due_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT DO NOTHING;

-- name: ListDueVerificationReminders :many
SELECT r.id, r.provider_id, r.kind, r.document_id, r.expires_on, r.due_at, d.document_type
FROM verification_reminders r
LEFT JOIN provider_documents d ON d.id = r.document_id
WHERE r.sent_at IS NULL AND r.due_at <= sqlc.arg(now)::timestamptz
ORDER BY r.due_at
LIMIT sqlc.arg(max_rows)
FOR UPDATE OF r SKIP LOCKED;

-- name: MarkVerificationReminderSent :exec
UPDATE verification_reminders SET sent_at = $2 WHERE id = $1;

-- name: ListExpiringDocuments :many
SELECT d.id, d.provider_id, d.document_type, d.expires_at::date AS expires_on
FROM provider_documents d
JOIN providers p ON p.user_id = d.provider_id
WHERE d.expires_at IS NOT NULL
  AND d.expires_at BETWEEN sqlc.arg(from_day)::date AND sqlc.arg(until_day)::date
  AND d.document_type::text = ANY(sqlc.arg(document_types)::text[])
  AND d.subject = 'provider'
  AND p.status IN ('pending', 'approved')
  AND NOT EXISTS (SELECT 1 FROM verification_reminders r WHERE r.document_id = d.id AND r.expires_on = d.expires_at)
ORDER BY d.expires_at
LIMIT sqlc.arg(max_rows);

-- name: CountOrganizationsWithNIF :one
SELECT count(*) FROM organizations WHERE nif = $1;
