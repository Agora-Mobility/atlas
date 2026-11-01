-- name: SaveAddress :exec
INSERT INTO addresses (id, lat, lng, line, reference, neighborhood, city, province, country_code)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (id) DO UPDATE
SET lat = EXCLUDED.lat, lng = EXCLUDED.lng, line = EXCLUDED.line, reference = EXCLUDED.reference, neighborhood = EXCLUDED.neighborhood,
    city = EXCLUDED.city, province = EXCLUDED.province, country_code = EXCLUDED.country_code, updated_at = now();

-- name: GetAddress :one
SELECT id, lat, lng, line, reference, neighborhood, city, province, country_code FROM addresses WHERE id = $1;

-- name: DeleteAddress :exec
DELETE FROM addresses WHERE id = $1;

-- name: SaveUserAddress :exec
INSERT INTO user_addresses (address_id, user_id, kind, label) VALUES ($1, $2, $3, $4)
ON CONFLICT (address_id) DO UPDATE SET kind = EXCLUDED.kind, label = EXCLUDED.label;

-- name: GetUserAddress :one
SELECT a.id, a.lat, a.lng, a.line, a.reference, a.neighborhood, a.city, a.province, a.country_code, ua.kind, ua.label
FROM user_addresses ua JOIN addresses a ON a.id = ua.address_id
WHERE ua.user_id = $1 AND ua.address_id = $2;

-- name: ListUserAddresses :many
SELECT a.id, a.lat, a.lng, a.line, a.reference, a.neighborhood, a.city, a.province, a.country_code, ua.kind, ua.label
FROM user_addresses ua JOIN addresses a ON a.id = ua.address_id
WHERE ua.user_id = $1
ORDER BY ua.created_at;
