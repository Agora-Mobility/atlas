-- name: GetProviderProfile :one
SELECT * FROM provider_profiles WHERE user_id = $1;

-- name: UpsertProviderProfile :exec
INSERT INTO provider_profiles (user_id, status, vehicle_plate, vehicle_description, service_id)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id) DO UPDATE
SET status = EXCLUDED.status, vehicle_plate = EXCLUDED.vehicle_plate, vehicle_description = EXCLUDED.vehicle_description, service_id = EXCLUDED.service_id, updated_at = now();

-- name: UpdateProviderBase :execrows
UPDATE provider_profiles SET base_lat = $2, base_lng = $3, service_radius_km = $4, updated_at = now() WHERE user_id = $1;

-- name: TouchProviderJobCompleted :exec
UPDATE provider_profiles SET last_job_completed_at = $2, updated_at = now() WHERE user_id = $1;

-- name: GetProviderCategory :one
SELECT c.id, c.slug
FROM provider_profiles p
JOIN services s ON s.id = p.service_id
JOIN service_categories c ON c.id = s.category_id
WHERE p.user_id = $1;

-- name: FilterEligibleProviders :many
SELECT p.user_id, p.last_job_completed_at, p.created_at, p.base_lat, p.base_lng, p.service_radius_km, u.phone
FROM provider_profiles p
JOIN users u ON u.id = p.user_id AND u.status = 'active' AND u.deleted_at IS NULL
WHERE p.user_id = ANY(sqlc.arg(provider_ids)::uuid[]) AND p.status = 'approved' AND p.service_id = sqlc.arg(service_id)::uuid;

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
INSERT INTO notifications (user_id, channel, template, status, provider_message_id, error) VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetUserContact :one
SELECT name, phone FROM users WHERE id = $1;
