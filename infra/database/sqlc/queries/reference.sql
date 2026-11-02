-- name: ListCatalogItems :many
SELECT i.*, COALESCE(c.slug, '') AS category_slug
FROM catalog_items i LEFT JOIN service_categories c ON c.id = i.category_id
WHERE i.kind = sqlc.arg(kind)::text
  AND (sqlc.arg(category)::text = '' OR c.slug = sqlc.arg(category)::text OR i.category_id IS NULL)
  AND (sqlc.arg(include_inactive)::boolean OR i.is_active)
ORDER BY i.display_order, i.label;

-- name: GetCatalogItem :one
SELECT i.*, COALESCE(c.slug, '') AS category_slug
FROM catalog_items i LEFT JOIN service_categories c ON c.id = i.category_id
WHERE i.id = $1;

-- name: GetCatalogItems :many
SELECT i.*, COALESCE(c.slug, '') AS category_slug
FROM catalog_items i LEFT JOIN service_categories c ON c.id = i.category_id
WHERE i.id = ANY(sqlc.arg(ids)::uuid[]);

-- name: InsertCatalogItem :exec
INSERT INTO catalog_items (id, kind, category_id, code, label, description, amount_cents, promo_amount_cents, promo_until, attrs, display_order, is_active)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);

-- name: UpdateCatalogItem :exec
UPDATE catalog_items
SET label = $2, description = $3, amount_cents = $4, promo_amount_cents = $5, promo_until = $6, attrs = $7, display_order = $8, is_active = $9, updated_at = now()
WHERE id = $1;

-- name: GetPlatformSetting :one
SELECT value FROM platform_settings WHERE key = $1;

-- name: ListPlatformSettings :many
SELECT key, value FROM platform_settings ORDER BY key;

-- name: UpsertPlatformSetting :exec
INSERT INTO platform_settings (key, value, updated_at) VALUES ($1, $2, now())
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();
