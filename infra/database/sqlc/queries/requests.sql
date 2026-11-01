-- name: InsertServiceRequest :exec
INSERT INTO service_requests (id, customer_id, category_id, category_slug, service_id, zone_id, policy_id, status, version, pickup_address_id, contact_phone,
    pickup_h3_r8, destination_address_id, destination_h3_r8, passenger_name, passenger_phone, details, scheduled_for, search_round, search_extended,
    search_deadline_at, next_action_at, reminder_at, fare_estimate_id, fare_cents, commission_cents, boost_bps, currency, idempotency_key, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31);

-- name: DeleteServiceRequestStops :exec
DELETE FROM service_request_stops WHERE request_id = $1;

-- name: InsertServiceRequestStop :exec
INSERT INTO service_request_stops (request_id, position, address_id) VALUES ($1, $2, $3);

-- name: ListRequestPlaces :many
SELECT r.id AS request_id, 'pickup'::text AS role, 0::int AS position, a.id, a.lat, a.lng, a.line, a.reference
FROM service_requests r JOIN addresses a ON a.id = r.pickup_address_id
WHERE r.id = ANY(sqlc.arg(request_ids)::uuid[])
UNION ALL
SELECT r.id, 'destination'::text, 0::int, a.id, a.lat, a.lng, a.line, a.reference
FROM service_requests r JOIN addresses a ON a.id = r.destination_address_id
WHERE r.id = ANY(sqlc.arg(request_ids)::uuid[])
UNION ALL
SELECT st.request_id, 'stop'::text, st.position, a.id, a.lat, a.lng, a.line, a.reference
FROM service_request_stops st JOIN addresses a ON a.id = st.address_id
WHERE st.request_id = ANY(sqlc.arg(request_ids)::uuid[])
ORDER BY 2, 3;

-- name: GetServiceRequest :one
SELECT * FROM service_requests WHERE id = $1;

-- name: GetServiceRequestForUpdate :one
SELECT * FROM service_requests WHERE id = $1 FOR UPDATE;

-- name: TryLockServiceRequest :one
SELECT * FROM service_requests WHERE id = $1 FOR UPDATE SKIP LOCKED;

-- name: FindServiceRequestByIdempotencyKey :one
SELECT * FROM service_requests WHERE customer_id = $1 AND idempotency_key = $2;

-- name: UpdateServiceRequest :execrows
UPDATE service_requests
SET status = sqlc.arg(status), version = sqlc.arg(version), destination_address_id = sqlc.arg(destination_address_id), destination_h3_r8 = sqlc.arg(destination_h3_r8),
    search_round = sqlc.arg(search_round), search_extended = sqlc.arg(search_extended), search_deadline_at = sqlc.arg(search_deadline_at),
    next_action_at = sqlc.arg(next_action_at), reminder_at = sqlc.arg(reminder_at), provider_id = sqlc.arg(provider_id), fare_cents = sqlc.arg(fare_cents),
    commission_cents = sqlc.arg(commission_cents), boost_bps = sqlc.arg(boost_bps), needs_operator = sqlc.arg(needs_operator), operator_reason = sqlc.arg(operator_reason),
    cancel_reason = sqlc.arg(cancel_reason), cancelled_by = sqlc.arg(cancelled_by), accepted_at = sqlc.arg(accepted_at), arrived_at = sqlc.arg(arrived_at),
    started_at = sqlc.arg(started_at), completed_at = sqlc.arg(completed_at), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- name: DueServiceRequests :many
SELECT id FROM service_requests
WHERE next_action_at <= sqlc.arg(now)::timestamptz
ORDER BY next_action_at
LIMIT sqlc.arg(batch)
FOR UPDATE SKIP LOCKED;

-- name: DueServiceRequestReminders :many
SELECT id FROM service_requests
WHERE reminder_at <= sqlc.arg(now)::timestamptz
ORDER BY reminder_at
LIMIT sqlc.arg(batch)
FOR UPDATE SKIP LOCKED;

-- name: ActiveRequestOfCustomer :one
SELECT * FROM service_requests
WHERE customer_id = $1 AND status IN ('created', 'scheduled', 'searching', 'offered', 'awaiting_choice', 'accepted', 'provider_en_route', 'in_progress')
ORDER BY (status = 'scheduled'), created_at DESC
LIMIT 1;

-- name: ActiveRequestOfProvider :one
SELECT * FROM service_requests
WHERE provider_id = $1 AND status IN ('accepted', 'provider_en_route', 'in_progress')
ORDER BY (status = 'accepted'), created_at DESC
LIMIT 1;

-- name: ListRequestsOfCustomer :many
SELECT * FROM service_requests
WHERE customer_id = sqlc.arg(customer_id)
  AND (sqlc.arg(statuses)::text[] = '{}' OR status = ANY(sqlc.arg(statuses)::text[]))
  AND (sqlc.arg(category_slug)::text = '' OR category_slug = sqlc.arg(category_slug))
ORDER BY created_at DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountRequestsOfCustomer :one
SELECT count(*) FROM service_requests
WHERE customer_id = sqlc.arg(customer_id)
  AND (sqlc.arg(statuses)::text[] = '{}' OR status = ANY(sqlc.arg(statuses)::text[]))
  AND (sqlc.arg(category_slug)::text = '' OR category_slug = sqlc.arg(category_slug));

-- name: ListOperatorQueue :many
SELECT * FROM service_requests
WHERE needs_operator AND status IN ('searching', 'offered', 'awaiting_choice')
ORDER BY created_at
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: InsertServiceRequestEvent :exec
INSERT INTO service_request_events (request_id, from_status, to_status, version, actor_type, actor_id, reason, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: InsertFareAdjustment :exec
INSERT INTO fare_adjustments (request_id, reason, amount_cents, actor_id, created_at) VALUES ($1, $2, $3, $4, $5);

-- name: ListFareAdjustments :many
SELECT reason, amount_cents, created_at FROM fare_adjustments WHERE request_id = $1 ORDER BY id;

-- name: DemandByCell :many
SELECT pickup_h3_r8, count(*) AS requests
FROM service_requests
WHERE created_at >= sqlc.arg(since)::timestamptz AND (sqlc.narg(zone_id)::uuid IS NULL OR zone_id = sqlc.narg(zone_id))
GROUP BY pickup_h3_r8;

-- name: UpsertRequestOffer :execrows
INSERT INTO request_offers (id, request_id, provider_id, status, sent_at, expires_at, next_action_at, fallback_step, distance_m, net_cents, boost_bps)
VALUES ($1, $2, $3, 'sent', $4, $5, $6, 0, $7, $8, $9)
ON CONFLICT (request_id, provider_id) DO UPDATE
SET id = EXCLUDED.id, status = 'sent', sent_at = EXCLUDED.sent_at, delivered_at = NULL, delivered_via = NULL, expires_at = EXCLUDED.expires_at,
    responded_at = NULL, next_action_at = EXCLUDED.next_action_at, fallback_step = 0, quote_cents = NULL, quote_note = '',
    distance_m = EXCLUDED.distance_m, net_cents = EXCLUDED.net_cents, boost_bps = EXCLUDED.boost_bps
WHERE request_offers.status IN ('rejected', 'expired', 'no_response', 'cancelled') AND request_offers.boost_bps < EXCLUDED.boost_bps;

-- name: GetRequestOffer :one
SELECT * FROM request_offers WHERE id = $1;

-- name: GetRequestOfferForUpdate :one
SELECT * FROM request_offers WHERE id = $1 FOR UPDATE;

-- name: UpdateRequestOffer :exec
UPDATE request_offers
SET status = $2, delivered_at = $3, delivered_via = $4, expires_at = $5, responded_at = $6, next_action_at = $7, fallback_step = $8, quote_cents = $9, quote_note = $10
WHERE id = $1;

-- name: ListRequestOffers :many
SELECT * FROM request_offers WHERE request_id = $1 ORDER BY sent_at;

-- name: ListRequestOffersForUpdate :many
SELECT * FROM request_offers WHERE request_id = $1 ORDER BY id FOR UPDATE;

-- name: DueRequestOffers :many
SELECT id FROM request_offers
WHERE next_action_at <= sqlc.arg(now)::timestamptz
ORDER BY next_action_at
LIMIT sqlc.arg(batch)
FOR UPDATE SKIP LOCKED;

-- name: ListOpenOffersOfProvider :many
SELECT * FROM request_offers
WHERE provider_id = $1 AND status IN ('sent', 'delivered') AND expires_at > sqlc.arg(now)::timestamptz
ORDER BY sent_at DESC;

-- name: InsertLocationSample :exec
INSERT INTO provider_location_samples (provider_id, request_id, lat, lng, recorded_at) VALUES ($1, $2, $3, $4, $5);

-- name: PurgeLocationSamples :execrows
DELETE FROM provider_location_samples WHERE recorded_at < $1;

-- name: ListRequestsWithProviderOnTheWay :many
SELECT * FROM service_requests
WHERE status IN ('accepted', 'provider_en_route') AND provider_id IS NOT NULL
  AND (scheduled_for IS NULL OR scheduled_for <= now())
ORDER BY updated_at
LIMIT $1;
