-- name: InsertServiceRequest :exec
INSERT INTO service_requests (id, customer_id, category_id, category_slug, service_id, zone_id, policy_id, status, version, pickup_address_id, contact_phone,
    pickup_h3_r8, destination_address_id, destination_h3_r8, passenger_name, passenger_phone, details, scheduled_for, search_round, search_extended,
    search_deadline_at, next_action_at, reminder_at, fare_estimate_id, fare_cents, commission_cents, boost_bps, currency, idempotency_key, created_at, updated_at, payment_method, public_code, vehicle_type_id, completion_code, workshop_id, prepay, removal_day, removal_window_id, route_geometry)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40);

-- name: DeleteServiceRequestStops :exec
DELETE FROM service_request_stops WHERE request_id = $1;

-- name: InsertServiceRequestStop :exec
INSERT INTO service_request_stops (request_id, position, address_id) VALUES ($1, $2, $3);

-- name: ListRequestPlaces :many
SELECT r.id AS request_id, 'pickup'::text AS role, 0::int AS position, a.id, a.lat, a.lng, a.line, a.reference, NULL::timestamptz AS arrived_at
FROM service_requests r JOIN addresses a ON a.id = r.pickup_address_id
WHERE r.id = ANY(sqlc.arg(request_ids)::uuid[])
UNION ALL
SELECT r.id, 'destination'::text, 0::int, a.id, a.lat, a.lng, a.line, a.reference, NULL::timestamptz
FROM service_requests r JOIN addresses a ON a.id = r.destination_address_id
WHERE r.id = ANY(sqlc.arg(request_ids)::uuid[])
UNION ALL
SELECT st.request_id, 'stop'::text, st.position, a.id, a.lat, a.lng, a.line, a.reference, st.arrived_at
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
    next_action_at = sqlc.arg(next_action_at), reminder_at = sqlc.arg(reminder_at), provider_id = sqlc.arg(provider_id),
    organization_id = CASE WHEN provider_id IS NOT DISTINCT FROM sqlc.arg(provider_id) THEN organization_id ELSE (SELECT p.organization_id FROM providers p WHERE p.user_id = sqlc.arg(provider_id)) END, fare_cents = sqlc.arg(fare_cents),
    commission_cents = sqlc.arg(commission_cents), route_geometry = sqlc.arg(route_geometry), boost_bps = sqlc.arg(boost_bps), needs_operator = sqlc.arg(needs_operator), operator_reason = sqlc.arg(operator_reason),
    cancel_reason = sqlc.arg(cancel_reason), cancelled_by = sqlc.arg(cancelled_by), cancel_reason_id = sqlc.arg(cancel_reason_id), cancel_fee_cents = sqlc.arg(cancel_fee_cents), completion_attempts = sqlc.arg(completion_attempts), extra_due_cents = sqlc.arg(extra_due_cents), voucher_id = sqlc.arg(voucher_id), voucher_code = sqlc.arg(voucher_code), discount_cents = sqlc.arg(discount_cents), accepted_at = sqlc.arg(accepted_at), arrived_at = sqlc.arg(arrived_at),
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
WHERE created_at >= sqlc.arg(from_at)::timestamptz AND created_at < sqlc.arg(to_at)::timestamptz
  AND (sqlc.narg(zone_id)::uuid IS NULL OR zone_id = sqlc.narg(zone_id))
  AND (sqlc.arg(category)::text = '' OR category_slug = sqlc.arg(category))
GROUP BY pickup_h3_r8;

-- name: FlowsByCell :many
SELECT pickup_h3_r8, destination_h3_r8::bigint AS destination_h3_r8, count(*) AS requests
FROM service_requests
WHERE destination_h3_r8 IS NOT NULL
  AND created_at >= sqlc.arg(from_at)::timestamptz AND created_at < sqlc.arg(to_at)::timestamptz
  AND (sqlc.narg(zone_id)::uuid IS NULL OR zone_id = sqlc.narg(zone_id))
  AND (sqlc.arg(category)::text = '' OR category_slug = sqlc.arg(category))
GROUP BY pickup_h3_r8, destination_h3_r8;

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
SET status = $2, delivered_at = $3, delivered_via = $4, expires_at = $5, responded_at = $6, next_action_at = $7, fallback_step = $8, quote_cents = $9, quote_note = $10, reject_reason_id = $11, reject_note = $12
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

-- name: ListAdminRequests :many
SELECT r.id, r.status, r.version, r.category_slug, c.name AS category_name, r.service_id, s.name AS service_name, r.customer_id, cu.name AS customer_name,
       r.provider_id, pu.name AS provider_name, r.organization_id, o.name AS organization_name, r.zone_id, z.name AS zone_name, r.fare_cents, r.currency,
       r.needs_operator, r.scheduled_for, r.created_at, r.completed_at, pa.line AS pickup_line, pa.lat AS pickup_lat, pa.lng AS pickup_lng, da.line AS destination_line
FROM service_requests r
JOIN service_categories c ON c.id = r.category_id
JOIN services s ON s.id = r.service_id
JOIN users cu ON cu.id = r.customer_id
JOIN service_zones z ON z.id = r.zone_id
JOIN addresses pa ON pa.id = r.pickup_address_id
LEFT JOIN addresses da ON da.id = r.destination_address_id
LEFT JOIN users pu ON pu.id = r.provider_id
LEFT JOIN organizations o ON o.id = r.organization_id
WHERE (sqlc.arg(statuses)::text[] = '{}' OR r.status = ANY(sqlc.arg(statuses)::text[]))
  AND (sqlc.arg(category)::text = '' OR r.category_slug = sqlc.arg(category)::text)
  AND (sqlc.narg(service_id)::uuid IS NULL OR r.service_id = sqlc.narg(service_id))
  AND (sqlc.narg(zone_id)::uuid IS NULL OR r.zone_id = sqlc.narg(zone_id))
  AND (sqlc.narg(organization_id)::uuid IS NULL OR r.organization_id = sqlc.narg(organization_id))
  AND (sqlc.narg(provider_id)::uuid IS NULL OR r.provider_id = sqlc.narg(provider_id))
  AND (sqlc.narg(created_from)::timestamptz IS NULL OR r.created_at >= sqlc.narg(created_from))
  AND (sqlc.narg(created_to)::timestamptz IS NULL OR r.created_at < sqlc.narg(created_to))
ORDER BY r.created_at DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountAdminRequests :one
SELECT count(*) FROM service_requests r
WHERE (sqlc.arg(statuses)::text[] = '{}' OR r.status = ANY(sqlc.arg(statuses)::text[]))
  AND (sqlc.arg(category)::text = '' OR r.category_slug = sqlc.arg(category)::text)
  AND (sqlc.narg(service_id)::uuid IS NULL OR r.service_id = sqlc.narg(service_id))
  AND (sqlc.narg(zone_id)::uuid IS NULL OR r.zone_id = sqlc.narg(zone_id))
  AND (sqlc.narg(organization_id)::uuid IS NULL OR r.organization_id = sqlc.narg(organization_id))
  AND (sqlc.narg(provider_id)::uuid IS NULL OR r.provider_id = sqlc.narg(provider_id))
  AND (sqlc.narg(created_from)::timestamptz IS NULL OR r.created_at >= sqlc.narg(created_from))
  AND (sqlc.narg(created_to)::timestamptz IS NULL OR r.created_at < sqlc.narg(created_to));

-- name: SummarizeRequests :many
SELECT r.category_slug, c.name AS category_name, r.service_id, s.name AS service_name, r.status, r.zone_id, z.name AS zone_name, r.organization_id, o.name AS organization_name,
       r.currency, count(*) AS total, COALESCE(sum(r.fare_cents) FILTER (WHERE r.status = 'completed'), 0)::bigint AS fare_cents
FROM service_requests r
JOIN service_categories c ON c.id = r.category_id
JOIN services s ON s.id = r.service_id
JOIN service_zones z ON z.id = r.zone_id
LEFT JOIN organizations o ON o.id = r.organization_id
WHERE (sqlc.arg(category)::text = '' OR r.category_slug = sqlc.arg(category)::text)
  AND (sqlc.narg(zone_id)::uuid IS NULL OR r.zone_id = sqlc.narg(zone_id))
  AND (sqlc.narg(organization_id)::uuid IS NULL OR r.organization_id = sqlc.narg(organization_id))
  AND (sqlc.narg(created_from)::timestamptz IS NULL OR r.created_at >= sqlc.narg(created_from))
  AND (sqlc.narg(created_to)::timestamptz IS NULL OR r.created_at < sqlc.narg(created_to))
GROUP BY r.category_slug, c.name, r.service_id, s.name, r.status, r.zone_id, z.name, r.organization_id, o.name, r.currency;

-- name: ListServiceRequestEvents :many
SELECT from_status, to_status, version, actor_type, actor_id, reason, occurred_at FROM service_request_events WHERE request_id = $1 ORDER BY id;

-- name: GetRequestOrganization :one
SELECT organization_id FROM service_requests WHERE id = $1;

-- name: LockCustomerRequests :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(customer_id)::uuid::text, 0));

-- name: ListActiveRequestsOfCustomerService :many
SELECT * FROM service_requests
WHERE customer_id = $1 AND service_id = $2 AND status NOT IN ('completed', 'cancelled', 'expired');

-- name: GetPassengerNotice :one
SELECT c.passenger_sms, COALESCE(u.name, '') AS provider_name, COALESCE(v.plate, '') AS vehicle_plate
FROM service_requests r
JOIN service_categories c ON c.id = r.category_id
LEFT JOIN users u ON u.id = r.provider_id
LEFT JOIN vehicles v ON v.provider_id = r.provider_id
WHERE r.id = $1;

-- name: NextRequestCode :one
SELECT (c.code_prefix || '-' || nextval('service_request_code_seq'))::text FROM service_categories c WHERE c.id = $1;

-- name: MarkStopArrived :execrows
UPDATE service_request_stops SET arrived_at = COALESCE(arrived_at, $3) WHERE request_id = $1 AND position = $2;

-- name: ListRecentDestinations :many
SELECT recent.lat, recent.lng, recent.line, recent.reference, recent.created_at
FROM (
    SELECT DISTINCT ON (round(a.lat::numeric, 4), round(a.lng::numeric, 4)) a.lat, a.lng, a.line, a.reference, r.created_at
    FROM service_requests r
    JOIN addresses a ON a.id = r.destination_address_id
    WHERE r.customer_id = $1
    ORDER BY round(a.lat::numeric, 4), round(a.lng::numeric, 4), r.created_at DESC
) AS recent
ORDER BY recent.created_at DESC
LIMIT sqlc.arg(page_limit);
