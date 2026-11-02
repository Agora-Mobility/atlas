-- name: ListFleetDrivers :many
SELECT p.user_id, p.status, p.requested_service_id, p.service_id, p.organization_id, u.name, u.phone, u.is_verified, v.id AS vehicle_id, v.plate AS vehicle_plate, s.name AS service_name
FROM providers p
JOIN users u ON u.id = p.user_id
LEFT JOIN vehicles v ON v.provider_id = p.user_id
LEFT JOIN services s ON s.id = COALESCE(p.service_id, p.requested_service_id)
WHERE p.organization_id = sqlc.arg(organization_id) AND (sqlc.arg(status)::text = '' OR p.status = sqlc.arg(status)::text)
ORDER BY u.name
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountFleetDrivers :one
SELECT count(*) FROM providers p WHERE p.organization_id = sqlc.arg(organization_id) AND (sqlc.arg(status)::text = '' OR p.status = sqlc.arg(status)::text);

-- name: CreateFleetVehicle :exec
INSERT INTO vehicles (id, organization_id, kind, brand, model, year, color, plate, seats, capacity_kg) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: UpdateFleetVehicle :exec
UPDATE vehicles SET kind = $2, brand = $3, model = $4, year = $5, color = $6, plate = $7, seats = $8, capacity_kg = $9, is_active = $10, updated_at = now() WHERE id = $1;

-- name: GetVehicle :one
SELECT v.*, u.name AS provider_name FROM vehicles v LEFT JOIN users u ON u.id = v.provider_id WHERE v.id = $1;

-- name: ListVehicles :many
SELECT v.*, u.name AS provider_name FROM vehicles v LEFT JOIN users u ON u.id = v.provider_id
WHERE (sqlc.narg(organization_id)::uuid IS NULL OR v.organization_id = sqlc.narg(organization_id))
  AND (sqlc.arg(kind)::text = '' OR v.kind = sqlc.arg(kind)::text)
  AND (sqlc.arg(search)::text = '' OR v.plate ILIKE sqlc.arg(search)::text)
ORDER BY v.plate
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountVehicles :one
SELECT count(*) FROM vehicles v
WHERE (sqlc.narg(organization_id)::uuid IS NULL OR v.organization_id = sqlc.narg(organization_id))
  AND (sqlc.arg(kind)::text = '' OR v.kind = sqlc.arg(kind)::text)
  AND (sqlc.arg(search)::text = '' OR v.plate ILIKE sqlc.arg(search)::text);

-- name: ReleaseVehicleOfProvider :exec
UPDATE vehicles SET provider_id = NULL, updated_at = now() WHERE provider_id = $1;

-- name: AssignVehicleToProvider :execrows
UPDATE vehicles SET provider_id = $2, updated_at = now() WHERE id = $1 AND provider_id IS NULL AND is_active;

-- name: SaveWorkshop :exec
INSERT INTO workshops (id, organization_id, address_id, name, phone, opening_hours, offers_home_service, home_service_fee_cents, currency, is_active)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (id) DO UPDATE
SET address_id = EXCLUDED.address_id, name = EXCLUDED.name, phone = EXCLUDED.phone, opening_hours = EXCLUDED.opening_hours, offers_home_service = EXCLUDED.offers_home_service,
    home_service_fee_cents = EXCLUDED.home_service_fee_cents, currency = EXCLUDED.currency, is_active = EXCLUDED.is_active, updated_at = now();

-- name: GetWorkshop :one
SELECT * FROM workshops WHERE id = $1;

-- name: ListWorkshops :many
SELECT * FROM workshops
WHERE (sqlc.narg(organization_id)::uuid IS NULL OR organization_id = sqlc.narg(organization_id))
ORDER BY name
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountWorkshops :one
SELECT count(*) FROM workshops WHERE (sqlc.narg(organization_id)::uuid IS NULL OR organization_id = sqlc.narg(organization_id));

-- name: SaveWorkshopService :exec
INSERT INTO workshop_services (id, workshop_id, name, description, price_cents, duration_minutes, is_active)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name, description = EXCLUDED.description, price_cents = EXCLUDED.price_cents, duration_minutes = EXCLUDED.duration_minutes, is_active = EXCLUDED.is_active, updated_at = now();

-- name: GetWorkshopService :one
SELECT * FROM workshop_services WHERE id = $1;

-- name: ListWorkshopServices :many
SELECT * FROM workshop_services WHERE workshop_id = $1 ORDER BY name;
