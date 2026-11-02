-- name: GetVoucher :one
SELECT * FROM vouchers WHERE id = $1;

-- name: GetVoucherByCodeForUpdate :one
SELECT * FROM vouchers WHERE code = upper(sqlc.arg(code)::text) FOR UPDATE;

-- name: GetVoucherByCode :one
SELECT * FROM vouchers WHERE code = upper(sqlc.arg(code)::text);

-- name: ListVouchers :many
SELECT * FROM vouchers ORDER BY created_at DESC LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountVouchers :one
SELECT count(*) FROM vouchers;

-- name: ListPublicVouchers :many
SELECT v.* FROM vouchers v
LEFT JOIN service_categories c ON c.id = v.category_id
WHERE v.is_active AND v.is_public AND v.code IS NOT NULL AND v.valid_from <= sqlc.arg(now)::timestamptz AND v.valid_until > sqlc.arg(now)::timestamptz
  AND (sqlc.arg(category)::text = '' OR c.slug = sqlc.arg(category)::text OR v.category_id IS NULL)
ORDER BY v.valid_until;

-- name: InsertVoucher :exec
INSERT INTO vouchers (id, code, description, category_id, service_id, discount_bps, discount_cents, max_discount_cents, min_fare_cents, valid_from, valid_until, max_redemptions, per_customer_limit, is_public, is_active, workshop_id, workshop_service_id, auto_apply)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18);

-- name: UpdateVoucher :exec
UPDATE vouchers
SET description = $2, discount_bps = $3, discount_cents = $4, max_discount_cents = $5, min_fare_cents = $6, valid_from = $7, valid_until = $8, max_redemptions = $9,
    per_customer_limit = $10, is_public = $11, is_active = $12, updated_at = now()
WHERE id = $1;

-- name: CountVoucherRedemptions :one
SELECT count(*) FROM voucher_redemptions WHERE voucher_id = $1 AND released_at IS NULL;

-- name: CountCustomerVoucherRedemptions :one
SELECT count(*) FROM voucher_redemptions WHERE voucher_id = $1 AND customer_id = $2 AND released_at IS NULL;

-- name: InsertVoucherRedemption :exec
INSERT INTO voucher_redemptions (voucher_id, customer_id, request_id, discount_cents) VALUES ($1, $2, $3, $4);

-- name: ReleaseVoucherRedemption :exec
UPDATE voucher_redemptions SET released_at = now() WHERE request_id = $1 AND released_at IS NULL;
