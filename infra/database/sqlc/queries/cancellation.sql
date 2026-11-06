-- name: GetCancellationRule :one
SELECT * FROM cancellation_rules WHERE service_id = $1;

-- name: ListCancellationRules :many
SELECT r.*, s.slug AS service_slug, s.name AS service_name
FROM cancellation_rules r JOIN services s ON s.id = r.service_id
ORDER BY s.display_order, s.slug;

-- name: UpsertCancellationRule :exec
INSERT INTO cancellation_rules (service_id, fee_cents, free_after_assign_seconds, scheduled_free_seconds, charge_without_provider, updated_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (service_id) DO UPDATE
SET fee_cents = EXCLUDED.fee_cents, free_after_assign_seconds = EXCLUDED.free_after_assign_seconds, scheduled_free_seconds = EXCLUDED.scheduled_free_seconds,
    charge_without_provider = EXCLUDED.charge_without_provider, updated_at = now();

-- name: ListReasonsForCategory :many
SELECT * FROM rejection_reasons
WHERE is_active AND scope = sqlc.arg(scope)::text AND (category_id IS NULL OR category_id = sqlc.arg(category_id)::uuid)
ORDER BY display_order, label;

-- name: GetActiveReason :one
SELECT * FROM rejection_reasons WHERE id = $1 AND scope = $2 AND is_active;

-- name: IncrementProviderCancellations :exec
UPDATE providers SET cancelled_requests = cancelled_requests + 1, updated_at = now() WHERE user_id = $1;

-- name: CancellationReport :many
SELECT sqlc.arg(scope)::text AS scope, reason.id AS reason_id, reason.code, reason.label, COALESCE(c.slug, '') AS category, count(*) AS total, COALESCE(sum(r.cancel_fee_cents), 0)::bigint AS fee_cents
FROM service_requests r
JOIN rejection_reasons reason ON reason.id = r.cancel_reason_id
LEFT JOIN service_categories c ON c.id = r.category_id
WHERE reason.scope = sqlc.arg(scope)::text
  AND (sqlc.arg(category)::text = '' OR c.slug = sqlc.arg(category)::text)
  AND (sqlc.narg(created_from)::timestamptz IS NULL OR r.created_at >= sqlc.narg(created_from))
  AND (sqlc.narg(created_to)::timestamptz IS NULL OR r.created_at < sqlc.narg(created_to))
GROUP BY reason.id, reason.code, reason.label, c.slug
ORDER BY total DESC, reason.label;

-- name: OfferRejectionReport :many
SELECT reason.id AS reason_id, reason.code, reason.label, COALESCE(c.slug, '') AS category, count(*) AS total
FROM request_offers o
JOIN rejection_reasons reason ON reason.id = o.reject_reason_id
JOIN service_requests r ON r.id = o.request_id
LEFT JOIN service_categories c ON c.id = r.category_id
WHERE (sqlc.arg(category)::text = '' OR c.slug = sqlc.arg(category)::text)
  AND (sqlc.narg(created_from)::timestamptz IS NULL OR r.created_at >= sqlc.narg(created_from))
  AND (sqlc.narg(created_to)::timestamptz IS NULL OR r.created_at < sqlc.narg(created_to))
GROUP BY reason.id, reason.code, reason.label, c.slug
ORDER BY total DESC, reason.label;
