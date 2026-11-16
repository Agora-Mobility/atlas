-- name: UpdateWorkshopSchedule :execrows
UPDATE workshops SET capacity = sqlc.arg(capacity), about = sqlc.arg(about), warranty = sqlc.arg(warranty), updated_at = now() WHERE id = sqlc.arg(id);

-- name: DeleteWorkshopHours :exec
DELETE FROM workshop_hours WHERE workshop_id = $1;

-- name: InsertWorkshopHours :exec
INSERT INTO workshop_hours (workshop_id, weekday, open_minute, close_minute) VALUES ($1, $2, $3, $4);

-- name: ListWorkshopHours :many
SELECT * FROM workshop_hours WHERE workshop_id = $1 ORDER BY weekday;

-- name: GetWorkshopProfile :one
SELECT capacity, about, warranty, rating_sum, rating_count FROM workshops WHERE id = $1;

-- name: LockWorkshop :one
SELECT id FROM workshops WHERE id = $1 FOR UPDATE;

-- name: UpsertWorkshopClosure :exec
INSERT INTO workshop_closures (workshop_id, closed_on, reason) VALUES ($1, $2, $3)
ON CONFLICT (workshop_id, closed_on) DO UPDATE SET reason = EXCLUDED.reason;

-- name: DeleteWorkshopClosure :exec
DELETE FROM workshop_closures WHERE workshop_id = $1 AND closed_on = $2;

-- name: ListWorkshopClosures :many
SELECT closed_on, reason FROM workshop_closures WHERE workshop_id = $1 AND closed_on BETWEEN sqlc.arg(from_day)::date AND sqlc.arg(to_day)::date ORDER BY closed_on;

-- name: InsertWorkshopMechanic :exec
INSERT INTO workshop_mechanics (id, workshop_id, name, is_active, created_at) VALUES ($1, $2, $3, true, $4);

-- name: DeactivateWorkshopMechanic :execrows
UPDATE workshop_mechanics SET is_active = false WHERE id = $1 AND workshop_id = $2;

-- name: ListWorkshopMechanics :many
SELECT * FROM workshop_mechanics WHERE workshop_id = $1 AND is_active ORDER BY name;

-- name: GetWorkshopMechanic :one
SELECT * FROM workshop_mechanics WHERE id = $1;

-- name: AddWorkshopFavorite :exec
INSERT INTO workshop_favorites (customer_id, workshop_id, created_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING;

-- name: RemoveWorkshopFavorite :exec
DELETE FROM workshop_favorites WHERE customer_id = $1 AND workshop_id = $2;

-- name: SearchWorkshops :many
SELECT w.id, w.name, w.offers_home_service, w.home_service_fee_cents, w.currency, w.rating_sum, w.rating_count, a.lat, a.lng, a.line,
       ST_Distance(a.geom, ST_SetSRID(ST_MakePoint(sqlc.arg(lng)::float8, sqlc.arg(lat)::float8), 4326)::geography)::float8 AS distance_m,
       EXISTS (SELECT 1 FROM workshop_favorites f WHERE f.workshop_id = w.id AND f.customer_id = sqlc.arg(customer_id)) AS is_favorite,
       COALESCE((SELECT m.object_key FROM workshop_photos p JOIN media_uploads m ON m.id = p.upload_id
                 WHERE p.workshop_id = w.id ORDER BY p.position LIMIT 1), '')::text AS cover_key
FROM workshops w JOIN addresses a ON a.id = w.address_id
WHERE w.is_active
  AND (sqlc.arg(search)::text = '' OR w.name ILIKE '%' || sqlc.arg(search)::text || '%')
  AND (sqlc.arg(category)::text = '' OR EXISTS (
        SELECT 1 FROM workshop_services s JOIN catalog_items c ON c.id = s.category_id
        WHERE s.workshop_id = w.id AND s.is_active AND c.code = sqlc.arg(category)::text))
  AND (NOT sqlc.arg(only_favorites)::boolean OR EXISTS (SELECT 1 FROM workshop_favorites f WHERE f.workshop_id = w.id AND f.customer_id = sqlc.arg(customer_id)))
  AND (NOT sqlc.arg(only_home)::boolean OR w.offers_home_service)
  AND (sqlc.arg(min_rating_bps)::bigint = 0 OR (w.rating_count > 0 AND w.rating_sum * 100 >= sqlc.arg(min_rating_bps)::bigint * w.rating_count))
  AND (sqlc.arg(max_distance_m)::float8 = 0 OR ST_DWithin(a.geom, ST_SetSRID(ST_MakePoint(sqlc.arg(lng)::float8, sqlc.arg(lat)::float8), 4326)::geography, sqlc.arg(max_distance_m)::float8))
ORDER BY CASE WHEN sqlc.arg(by_rating)::boolean THEN (w.rating_sum::float8 / GREATEST(w.rating_count, 1)) END DESC NULLS LAST, distance_m, w.id
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: ActiveWorkshopVouchers :many
SELECT * FROM vouchers
WHERE workshop_id = $1 AND auto_apply AND is_active AND valid_from <= sqlc.arg(now)::timestamptz AND valid_until > sqlc.arg(now)::timestamptz
ORDER BY valid_until;

-- name: LockVoucher :one
SELECT * FROM vouchers WHERE id = $1 FOR UPDATE;

-- name: InsertBooking :exec
INSERT INTO bookings (id, request_id, workshop_id, customer_id, vehicle_id, status, at_home, home_fee_cents, services_cents, starts_at, ends_at, description, mileage_km, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15);

-- name: InsertBookingItem :exec
INSERT INTO booking_items (id, booking_id, service_id, name, price_cents, duration_minutes) VALUES ($1, $2, $3, $4, $5, $6);

-- name: UpdateBooking :exec
UPDATE bookings SET status = sqlc.arg(status), mechanic_id = sqlc.arg(mechanic_id), expected_ready_at = sqlc.arg(expected_ready_at), updated_at = sqlc.arg(updated_at) WHERE id = sqlc.arg(id);

-- name: GetBookingDetail :one
SELECT b.*, r.public_code, w.name AS workshop_name, w.organization_id, COALESCE(m.name, '')::text AS mechanic_name
FROM bookings b JOIN service_requests r ON r.id = b.request_id JOIN workshops w ON w.id = b.workshop_id LEFT JOIN workshop_mechanics m ON m.id = b.mechanic_id
WHERE b.id = $1;

-- name: GetBookingIDOfRequest :one
SELECT id FROM bookings WHERE request_id = $1;

-- name: LockBooking :one
SELECT id FROM bookings WHERE id = $1 FOR UPDATE;

-- name: ListBookingItems :many
SELECT * FROM booking_items WHERE booking_id = $1 ORDER BY name;

-- name: ListBookingExtras :many
SELECT * FROM booking_extras WHERE booking_id = $1 ORDER BY created_at;

-- name: InsertBookingExtra :exec
INSERT INTO booking_extras (id, booking_id, description, amount_cents, status, created_at) VALUES ($1, $2, $3, $4, $5, $6);

-- name: UpdateBookingExtra :exec
UPDATE booking_extras SET status = $2, decided_at = $3 WHERE id = $1;

-- name: InsertBookingEvent :exec
INSERT INTO booking_events (booking_id, from_status, to_status, actor_type, actor_id, occurred_at) VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListBookedSlots :many
SELECT starts_at, ends_at FROM bookings
WHERE workshop_id = $1 AND status <> 'cancelled' AND status <> 'completed' AND starts_at < sqlc.arg(until)::timestamptz AND ends_at > sqlc.arg(since)::timestamptz;

-- name: ListCustomerBookings :many
SELECT id FROM bookings WHERE customer_id = $1 ORDER BY starts_at DESC LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountCustomerBookings :one
SELECT count(*) FROM bookings WHERE customer_id = $1;

-- name: ListWorkshopBookings :many
SELECT id FROM bookings WHERE workshop_id = $1 AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
ORDER BY starts_at DESC LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountWorkshopBookings :one
SELECT count(*) FROM bookings WHERE workshop_id = $1 AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text);

-- name: ListWorkshopPhotos :many
SELECT m.* FROM workshop_photos p JOIN media_uploads m ON m.id = p.upload_id
WHERE p.workshop_id = $1
ORDER BY p.position;

-- name: DeleteWorkshopPhotos :exec
DELETE FROM workshop_photos WHERE workshop_id = $1;

-- name: InsertWorkshopPhoto :exec
INSERT INTO workshop_photos (upload_id, workshop_id, position, created_at) VALUES ($1, $2, $3, $4);
