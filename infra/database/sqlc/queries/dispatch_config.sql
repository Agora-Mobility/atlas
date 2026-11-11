-- name: ListServiceZones :many
SELECT id, country_code, name, timezone, currency, is_active, created_at, updated_at,
       ST_AsGeoJSON(geom::geometry)::text AS geojson
FROM service_zones
WHERE NOT sqlc.arg(only_active)::boolean OR is_active
ORDER BY name;

-- name: GetServiceZone :one
SELECT id, country_code, name, timezone, currency, is_active, created_at, updated_at,
       ST_AsGeoJSON(geom::geometry)::text AS geojson
FROM service_zones
WHERE id = $1;

-- name: CreateServiceZone :exec
INSERT INTO service_zones (id, country_code, name, geom, timezone, currency, is_active)
VALUES (sqlc.arg(id), sqlc.arg(country_code), sqlc.arg(name),
        ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON(sqlc.arg(geojson)::text), 4326))::geography,
        sqlc.arg(timezone), sqlc.arg(currency), sqlc.arg(is_active));

-- name: UpdateServiceZone :execrows
UPDATE service_zones
SET name = sqlc.arg(name), timezone = sqlc.arg(timezone), currency = sqlc.arg(currency), is_active = sqlc.arg(is_active),
    geom = ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON(sqlc.arg(geojson)::text), 4326))::geography, updated_at = now()
WHERE id = sqlc.arg(id);

-- name: FindZoneCoveringPoint :one
SELECT id FROM service_zones
WHERE is_active AND ST_Covers(geom, ST_SetSRID(ST_MakePoint(sqlc.arg(lng)::float8, sqlc.arg(lat)::float8), 4326)::geography)
ORDER BY ST_Area(geom)
LIMIT 1;

-- name: IsValidZoneGeometry :one
SELECT ST_IsValid(ST_GeomFromGeoJSON(sqlc.arg(geojson)::text))::boolean;

-- name: ResolveDispatchPolicy :one
SELECT * FROM dispatch_policies
WHERE category_id = $1 AND (zone_id = $2 OR zone_id IS NULL)
ORDER BY zone_id NULLS LAST
LIMIT 1;

-- name: GetDispatchPolicy :one
SELECT * FROM dispatch_policies WHERE id = $1;

-- name: ListDispatchPolicies :many
SELECT * FROM dispatch_policies ORDER BY category_id, zone_id NULLS FIRST;

-- name: ListLiveIndexes :many
SELECT DISTINCT c.id AS category_id, c.slug, p.h3_resolution
FROM dispatch_policies p
JOIN service_categories c ON c.id = p.category_id
WHERE p.candidate_source = 'live';

-- name: CreateDispatchPolicy :exec
INSERT INTO dispatch_policies (id, category_id, zone_id, candidate_source, h3_resolution, radii_km, offer_mode, batch_size,
    offer_ttl_seconds, search_ttl_seconds, ack_timeout_seconds, ack_giveup_seconds, initial_channels, fallback_channels, ranking,
    tracking_statuses, on_exhausted, reserve_provider, schedule_max_days, search_lead_seconds, pricing_mode, allow_boost, rank_by_eta, sms_on_accept, max_stops, schedule_slot_minutes)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26);

-- name: UpdateDispatchPolicy :execrows
UPDATE dispatch_policies
SET candidate_source = $2, h3_resolution = $3, radii_km = $4, offer_mode = $5, batch_size = $6, offer_ttl_seconds = $7,
    search_ttl_seconds = $8, ack_timeout_seconds = $9, ack_giveup_seconds = $10, initial_channels = $11, fallback_channels = $12,
    ranking = $13, tracking_statuses = $14, on_exhausted = $15, reserve_provider = $16, schedule_max_days = $17,
    search_lead_seconds = $18, pricing_mode = $19, allow_boost = $20, rank_by_eta = $21, sms_on_accept = $22, max_stops = $23, schedule_slot_minutes = $24, updated_at = now()
WHERE id = $1;

-- name: CurrentServiceTariff :one
SELECT * FROM service_tariffs
WHERE service_id = $1 AND (zone_id = $2 OR zone_id IS NULL) AND valid_until IS NULL
ORDER BY zone_id NULLS LAST
LIMIT 1;

-- name: GetServiceTariff :one
SELECT * FROM service_tariffs WHERE id = $1;

-- name: ListCurrentServiceTariffs :many
SELECT * FROM service_tariffs WHERE valid_until IS NULL ORDER BY service_id, zone_id NULLS FIRST;

-- name: CloseServiceTariff :exec
UPDATE service_tariffs SET valid_until = $3
WHERE service_id = $1 AND zone_id IS NOT DISTINCT FROM $2 AND valid_until IS NULL;

-- name: CreateServiceTariff :exec
INSERT INTO service_tariffs (id, service_id, zone_id, currency, base_fee_cents, per_km_fee_cents, per_minute_fee_cents, minimum_fee_cents,
    zone_fee_cents, night_fee_cents, night_start_hour, night_end_hour, free_wait_seconds, commission_bps, valid_from, stop_fee_cents)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16);

-- name: CreateFareEstimate :exec
INSERT INTO fare_estimates (id, customer_id, tariff_id, service_id, origin_lat, origin_lng, destination_lat, destination_lng, stops,
    distance_m, duration_s, route_source, total_cents, commission_cents, currency, expires_at, created_at, scheduled_for, vehicle_type_id, details, route_geometry,
    demand_factor_bps, demand_cents, demand_commissioned)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24);

-- name: GetFareEstimate :one
SELECT * FROM fare_estimates WHERE id = $1;

-- name: UseFareEstimate :execrows
UPDATE fare_estimates SET used_at = $2 WHERE id = $1 AND used_at IS NULL;

-- name: RecordETAObservation :exec
INSERT INTO eta_observations (origin_h3_r8, destination_h3_r8, hour_band, samples, total_seconds)
VALUES ($1, $2, $3, 1, $4)
ON CONFLICT (origin_h3_r8, destination_h3_r8, hour_band)
DO UPDATE SET samples = eta_observations.samples + 1, total_seconds = eta_observations.total_seconds + EXCLUDED.total_seconds, updated_at = now();

-- name: GetETAObservation :one
SELECT samples, total_seconds FROM eta_observations
WHERE origin_h3_r8 = $1 AND destination_h3_r8 = $2 AND hour_band = $3;

-- name: GetServiceForDispatch :one
SELECT s.id, s.name, s.slug, s.category_id, s.requires_destination, s.is_active AS service_active, c.slug AS category_slug, c.is_active AS category_active
FROM services s JOIN service_categories c ON c.id = s.category_id
WHERE s.id = $1;

-- name: ListActiveServicesOfCategory :many
SELECT s.id, s.name, s.slug, s.category_id, s.requires_destination, c.slug AS category_slug
FROM services s JOIN service_categories c ON c.id = s.category_id
WHERE c.slug = $1 AND s.is_active AND c.is_active
ORDER BY s.display_order;

-- name: GetCategoryBySlug :one
SELECT * FROM service_categories WHERE slug = $1;

-- name: TrackedDistanceMeters :one
SELECT COALESCE(sum(ST_Distance(ST_SetSRID(ST_MakePoint(s.lng, s.lat), 4326)::geography, ST_SetSRID(ST_MakePoint(s.previous_lng, s.previous_lat), 4326)::geography)
           ) FILTER (WHERE s.previous_lat IS NOT NULL), 0)::bigint AS meters,
       count(*)::integer AS samples
FROM (
    SELECT lat, lng, lag(lat) OVER (ORDER BY recorded_at) AS previous_lat, lag(lng) OVER (ORDER BY recorded_at) AS previous_lng
    FROM provider_location_samples
    WHERE request_id = sqlc.arg(request_id) AND recorded_at >= sqlc.arg(since)::timestamptz
) s;

-- name: UpsertTripMetrics :exec
INSERT INTO request_trip_metrics (request_id, estimated_distance_m, estimated_duration_s, actual_distance_m, actual_duration_s,
    distance_deviation_bps, duration_deviation_bps, samples, route_changed, needs_review, recorded_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (request_id) DO UPDATE SET
    estimated_distance_m = EXCLUDED.estimated_distance_m, estimated_duration_s = EXCLUDED.estimated_duration_s,
    actual_distance_m = EXCLUDED.actual_distance_m, actual_duration_s = EXCLUDED.actual_duration_s,
    distance_deviation_bps = EXCLUDED.distance_deviation_bps, duration_deviation_bps = EXCLUDED.duration_deviation_bps, samples = EXCLUDED.samples,
    route_changed = EXCLUDED.route_changed, needs_review = EXCLUDED.needs_review, recorded_at = EXCLUDED.recorded_at;

-- name: GetTripMetrics :one
SELECT * FROM request_trip_metrics WHERE request_id = $1;
