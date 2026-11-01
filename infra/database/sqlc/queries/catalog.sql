-- name: ListServiceCategories :many
SELECT * FROM service_categories
WHERE (NOT @only_active::boolean OR is_active)
ORDER BY display_order, name;

-- name: GetServiceCategory :one
SELECT * FROM service_categories
WHERE id = @id;

-- name: CreateServiceCategory :exec
INSERT INTO service_categories (id, slug, name, description, icon, display_order, is_active, created_at, updated_at)
VALUES (@id, @slug, @name, @description, @icon, @display_order, @is_active, @created_at, @updated_at);

-- name: UpdateServiceCategory :exec
UPDATE service_categories
SET name          = @name,
    description   = @description,
    icon          = @icon,
    display_order = @display_order,
    is_active     = @is_active,
    updated_at    = @updated_at
WHERE id = @id;

-- name: ListServices :many
SELECT * FROM services
WHERE (sqlc.narg(category_id)::uuid IS NULL OR category_id = sqlc.narg(category_id)::uuid)
  AND (sqlc.narg(is_active)::boolean IS NULL OR is_active = sqlc.narg(is_active)::boolean)
  AND (sqlc.narg(search)::text IS NULL OR name ILIKE sqlc.narg(search)::text OR slug ILIKE sqlc.narg(search)::text)
ORDER BY display_order, name
LIMIT @page_limit OFFSET @page_offset;

-- name: CountServices :one
SELECT count(*) FROM services
WHERE (sqlc.narg(category_id)::uuid IS NULL OR category_id = sqlc.narg(category_id)::uuid)
  AND (sqlc.narg(is_active)::boolean IS NULL OR is_active = sqlc.narg(is_active)::boolean)
  AND (sqlc.narg(search)::text IS NULL OR name ILIKE sqlc.narg(search)::text OR slug ILIKE sqlc.narg(search)::text);

-- name: ListActiveServicesOfActiveCategories :many
SELECT s.* FROM services s
JOIN service_categories c ON c.id = s.category_id
WHERE s.is_active AND c.is_active
ORDER BY s.display_order, s.name;

-- name: GetService :one
SELECT * FROM services
WHERE id = @id;

-- name: CreateService :exec
INSERT INTO services (
    id, category_id, slug, name, description, icon, requires_destination,
    base_fee_cents, per_km_fee_cents, per_minute_fee_cents, display_order, is_active, created_at, updated_at
) VALUES (
    @id, @category_id, @slug, @name, @description, @icon, @requires_destination,
    @base_fee_cents, @per_km_fee_cents, @per_minute_fee_cents, @display_order, @is_active, @created_at, @updated_at
);

-- name: UpdateService :exec
UPDATE services
SET category_id          = @category_id,
    name                 = @name,
    description          = @description,
    icon                 = @icon,
    requires_destination = @requires_destination,
    base_fee_cents       = @base_fee_cents,
    per_km_fee_cents     = @per_km_fee_cents,
    per_minute_fee_cents = @per_minute_fee_cents,
    display_order        = @display_order,
    is_active            = @is_active,
    updated_at           = @updated_at
WHERE id = @id;
