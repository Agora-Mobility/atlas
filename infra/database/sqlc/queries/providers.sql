-- name: CreateProvider :exec
INSERT INTO providers (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING;

-- name: GetProvider :one
SELECT * FROM providers WHERE user_id = $1;

-- name: UpsertProvider :exec
INSERT INTO providers (user_id, status, service_id, requested_service_id, birth_date, id_number, province, municipality, status_reason, submitted_at, reviewed_at, organization_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (user_id) DO UPDATE
SET status = EXCLUDED.status, service_id = EXCLUDED.service_id, requested_service_id = EXCLUDED.requested_service_id, birth_date = EXCLUDED.birth_date,
    id_number = EXCLUDED.id_number, province = EXCLUDED.province, municipality = EXCLUDED.municipality, status_reason = EXCLUDED.status_reason, submitted_at = EXCLUDED.submitted_at,
    reviewed_at = EXCLUDED.reviewed_at, organization_id = EXCLUDED.organization_id, updated_at = now();

-- name: GetVehicleOfProvider :one
SELECT * FROM vehicles WHERE provider_id = $1;

-- name: UpsertVehicle :exec
INSERT INTO vehicles (provider_id, kind, brand, model, year, color, plate, seats, capacity_kg)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (provider_id) DO UPDATE
SET kind = EXCLUDED.kind, brand = EXCLUDED.brand, model = EXCLUDED.model, year = EXCLUDED.year, color = EXCLUDED.color, plate = EXCLUDED.plate,
    seats = EXCLUDED.seats, capacity_kg = EXCLUDED.capacity_kg, updated_at = now();

-- name: ListProviderApplications :many
SELECT p.user_id, p.status, p.requested_service_id, p.service_id, p.organization_id, p.submitted_at, p.reviewed_at, v.plate AS vehicle_plate, u.name, u.phone,
       s.name AS requested_service_name, o.name AS organization_name
FROM providers p
JOIN users u ON u.id = p.user_id
LEFT JOIN vehicles v ON v.provider_id = p.user_id
LEFT JOIN services s ON s.id = p.requested_service_id
LEFT JOIN organizations o ON o.id = p.organization_id
LEFT JOIN services active ON active.id = COALESCE(p.service_id, p.requested_service_id)
LEFT JOIN service_categories c ON c.id = active.category_id
WHERE (sqlc.arg(status)::text = '' OR p.status = sqlc.arg(status)::text)
  AND (sqlc.narg(organization_id)::uuid IS NULL OR p.organization_id = sqlc.narg(organization_id))
  AND (sqlc.narg(service_id)::uuid IS NULL OR COALESCE(p.service_id, p.requested_service_id) = sqlc.narg(service_id))
  AND (sqlc.arg(category)::text = '' OR c.slug = sqlc.arg(category)::text)
  AND (sqlc.arg(search)::text = '' OR u.name ILIKE sqlc.arg(search)::text OR u.phone ILIKE sqlc.arg(search)::text OR v.plate ILIKE sqlc.arg(search)::text)
ORDER BY p.submitted_at NULLS LAST, p.created_at
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountProviderApplications :one
SELECT count(*)
FROM providers p
JOIN users u ON u.id = p.user_id
LEFT JOIN vehicles v ON v.provider_id = p.user_id
LEFT JOIN services active ON active.id = COALESCE(p.service_id, p.requested_service_id)
LEFT JOIN service_categories c ON c.id = active.category_id
WHERE (sqlc.arg(status)::text = '' OR p.status = sqlc.arg(status)::text)
  AND (sqlc.narg(organization_id)::uuid IS NULL OR p.organization_id = sqlc.narg(organization_id))
  AND (sqlc.narg(service_id)::uuid IS NULL OR COALESCE(p.service_id, p.requested_service_id) = sqlc.narg(service_id))
  AND (sqlc.arg(category)::text = '' OR c.slug = sqlc.arg(category)::text)
  AND (sqlc.arg(search)::text = '' OR u.name ILIKE sqlc.arg(search)::text OR u.phone ILIKE sqlc.arg(search)::text OR v.plate ILIKE sqlc.arg(search)::text);

-- name: UpdateProviderBase :execrows
UPDATE providers SET base_address_id = $2, service_radius_km = $3, updated_at = now() WHERE user_id = $1;

-- name: TouchProviderJobCompleted :exec
UPDATE providers SET last_job_completed_at = $2, completed_requests = completed_requests + 1, updated_at = now() WHERE user_id = $1;

-- name: GetProviderCategory :one
SELECT c.id, c.slug
FROM providers p
JOIN services s ON s.id = p.service_id
JOIN service_categories c ON c.id = s.category_id
WHERE p.user_id = $1;

-- name: FilterEligibleProviders :many
SELECT p.user_id, p.last_job_completed_at, p.created_at, b.lat AS base_lat, b.lng AS base_lng, p.service_radius_km, u.phone, COALESCE((SELECT v.capacity_kg FROM vehicles v WHERE v.provider_id = p.user_id AND v.is_active LIMIT 1), 0)::int AS capacity_kg
FROM providers p
LEFT JOIN addresses b ON b.id = p.base_address_id
JOIN users u ON u.id = p.user_id AND u.status = 'active' AND u.deleted_at IS NULL
LEFT JOIN service_provider_requirements r ON r.service_id = p.service_id
WHERE p.user_id = ANY(sqlc.arg(provider_ids)::uuid[]) AND p.status = 'approved' AND p.service_id = sqlc.arg(service_id)::uuid
  AND (NOT COALESCE(r.requires_vehicle, false) OR EXISTS (SELECT 1 FROM vehicles v WHERE v.provider_id = p.user_id AND v.is_active));

-- name: ProvidersWithActiveJob :many
SELECT DISTINCT provider_id::uuid AS provider_id FROM service_requests
WHERE provider_id = ANY(sqlc.arg(provider_ids)::uuid[]) AND status IN ('accepted', 'provider_en_route', 'in_progress')
  AND (scheduled_for IS NULL OR scheduled_for <= sqlc.arg(busy_until)::timestamptz);

-- name: DeleteProviderCoverage :exec
DELETE FROM provider_coverage WHERE provider_id = $1;

-- name: InsertProviderCoverage :exec
INSERT INTO provider_coverage (category_id, h3_r6, provider_id)
SELECT sqlc.arg(category_id), unnest(sqlc.arg(cells)::bigint[]), sqlc.arg(provider_id)
ON CONFLICT DO NOTHING;

-- name: ProvidersCoveringCell :many
SELECT provider_id FROM provider_coverage WHERE category_id = $1 AND h3_r6 = $2;

-- name: UpsertDeviceToken :exec
INSERT INTO device_tokens (token, user_id, platform) VALUES ($1, $2, $3)
ON CONFLICT (token) DO UPDATE SET user_id = EXCLUDED.user_id, platform = EXCLUDED.platform, updated_at = now();

-- name: DeleteDeviceToken :exec
DELETE FROM device_tokens WHERE token = $1;

-- name: DeleteUserDeviceToken :exec
DELETE FROM device_tokens WHERE token = $1 AND user_id = $2;

-- name: ListDeviceTokens :many
SELECT token FROM device_tokens WHERE user_id = $1 ORDER BY updated_at DESC LIMIT 5;

-- name: RecordNotification :exec
INSERT INTO notifications (user_id, channel, template, status, provider_message_id, error, delivery_key) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (delivery_key) WHERE status = 'sent' AND delivery_key IS NOT NULL DO NOTHING;

-- name: NotificationDelivered :one
SELECT EXISTS (SELECT 1 FROM notifications WHERE delivery_key = $1 AND status = 'sent');

-- name: GetUserContact :one
SELECT name, phone FROM users WHERE id = $1;
