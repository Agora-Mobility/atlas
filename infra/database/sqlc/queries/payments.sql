-- name: ListCategoryPaymentMethods :many
SELECT c.id AS category_id, c.slug AS category_slug, c.name AS category_name, m.method, m.is_enabled, m.display_order
FROM category_payment_methods m
JOIN service_categories c ON c.id = m.category_id
WHERE (sqlc.arg(category)::text = '' OR c.slug = sqlc.arg(category)::text) AND (NOT sqlc.arg(only_enabled)::boolean OR m.is_enabled)
ORDER BY c.display_order, m.display_order;

-- name: IsPaymentMethodEnabled :one
SELECT EXISTS (SELECT 1 FROM category_payment_methods WHERE category_id = $1 AND method = $2 AND is_enabled);

-- name: SetCategoryPaymentMethod :execrows
UPDATE category_payment_methods SET is_enabled = $3, updated_at = now() WHERE category_id = $1 AND method = $2;

-- name: HasOutstandingPayment :one
SELECT EXISTS (
    SELECT 1 FROM service_requests
    WHERE customer_id = $1 AND payment_status IN ('unpaid', 'pending')
      AND ((status = 'completed' AND payment_method <> 'cash') OR (status = 'cancelled' AND cancel_fee_cents > 0))
);

-- name: SetRequestPayment :exec
UPDATE service_requests SET payment_method = $2, payment_status = $3, updated_at = now() WHERE id = $1;

-- name: InsertPayment :exec
INSERT INTO payments (id, request_id, customer_id, method, status, amount_cents, refunded_cents, currency, merchant_id, provider_charge_id, payer_phone_masked,
    reference_number, reference_entity, failure_message, expires_at, next_check_at, paid_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19);

-- name: UpdatePayment :exec
UPDATE payments
SET status = $2, refunded_cents = $3, provider_charge_id = $4, reference_number = $5, reference_entity = $6, failure_message = $7,
    expires_at = $8, next_check_at = $9, paid_at = $10, updated_at = $11
WHERE id = $1;

-- name: GetPayment :one
SELECT * FROM payments WHERE id = $1;

-- name: GetPaymentForUpdate :one
SELECT * FROM payments WHERE id = $1 FOR UPDATE;

-- name: GetPaymentByProviderCharge :one
SELECT * FROM payments WHERE provider_charge_id = $1;

-- name: GetPaymentByMerchantID :one
SELECT * FROM payments WHERE merchant_id = $1;

-- name: GetPendingPaymentOfRequest :one
SELECT * FROM payments WHERE request_id = $1 AND status = 'pending';

-- name: GetPaidPaymentOfRequest :one
SELECT * FROM payments WHERE request_id = $1 AND status = 'paid' ORDER BY created_at LIMIT 1;

-- name: ListPaymentsOfRequest :many
SELECT * FROM payments WHERE request_id = $1 ORDER BY created_at DESC;

-- name: DuePayments :many
SELECT id FROM payments WHERE status = 'pending' AND next_check_at <= $1 ORDER BY next_check_at LIMIT $2;

-- name: ListAdminPayments :many
SELECT p.*, r.category_slug, u.name AS customer_name
FROM payments p
JOIN service_requests r ON r.id = p.request_id
JOIN users u ON u.id = p.customer_id
WHERE (sqlc.arg(statuses)::text[] = '{}' OR p.status = ANY(sqlc.arg(statuses)::text[]))
  AND (sqlc.arg(method)::text = '' OR p.method = sqlc.arg(method)::text)
  AND (sqlc.arg(category)::text = '' OR r.category_slug = sqlc.arg(category)::text)
  AND (sqlc.narg(request_id)::uuid IS NULL OR p.request_id = sqlc.narg(request_id))
  AND (sqlc.narg(created_from)::timestamptz IS NULL OR p.created_at >= sqlc.narg(created_from))
  AND (sqlc.narg(created_to)::timestamptz IS NULL OR p.created_at < sqlc.narg(created_to))
ORDER BY p.created_at DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountAdminPayments :one
SELECT count(*)
FROM payments p
JOIN service_requests r ON r.id = p.request_id
WHERE (sqlc.arg(statuses)::text[] = '{}' OR p.status = ANY(sqlc.arg(statuses)::text[]))
  AND (sqlc.arg(method)::text = '' OR p.method = sqlc.arg(method)::text)
  AND (sqlc.arg(category)::text = '' OR r.category_slug = sqlc.arg(category)::text)
  AND (sqlc.narg(request_id)::uuid IS NULL OR p.request_id = sqlc.narg(request_id))
  AND (sqlc.narg(created_from)::timestamptz IS NULL OR p.created_at >= sqlc.narg(created_from))
  AND (sqlc.narg(created_to)::timestamptz IS NULL OR p.created_at < sqlc.narg(created_to));

-- name: InsertPaymentEvent :exec
INSERT INTO payment_events (payment_id, source, status, detail, occurred_at) VALUES ($1, $2, $3, $4, $5);

-- name: ListPaymentEvents :many
SELECT source, status, detail, occurred_at FROM payment_events WHERE payment_id = $1 ORDER BY id;

-- name: InsertPaymentRefund :exec
INSERT INTO payment_refunds (id, payment_id, amount_cents, reason, provider_refund_id, actor_id, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListPaymentRefunds :many
SELECT * FROM payment_refunds WHERE payment_id = $1 ORDER BY created_at;
