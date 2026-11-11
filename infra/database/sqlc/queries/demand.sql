-- name: ListDemandPricing :many
SELECT * FROM demand_pricing ORDER BY zone_id, category_id;

-- name: GetDemandPricing :one
SELECT * FROM demand_pricing WHERE zone_id = $1 AND category_id = $2;

-- name: UpsertDemandPricing :exec
INSERT INTO demand_pricing (id, zone_id, category_id, mode, tiers, max_factor_bps, commission_on_surge, updated_by, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (zone_id, category_id) DO UPDATE SET
    mode = EXCLUDED.mode, tiers = EXCLUDED.tiers, max_factor_bps = EXCLUDED.max_factor_bps,
    commission_on_surge = EXCLUDED.commission_on_surge, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at;

-- name: PendingCustomersByCell :many
SELECT DISTINCT pickup_h3_r8, customer_id
FROM service_requests
WHERE zone_id = $1 AND category_id = $2 AND status IN ('created', 'searching', 'offered', 'awaiting_choice');

-- name: InsertDemandSnapshot :exec
INSERT INTO demand_snapshots (zone_id, category_id, area_h3, pending, free, ratio_bps, factor_bps, recorded_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListDemandSnapshots :many
SELECT * FROM demand_snapshots
WHERE zone_id = $1 AND category_id = $2 AND recorded_at >= sqlc.arg(since)::timestamptz
ORDER BY recorded_at DESC, area_h3
LIMIT sqlc.arg(row_limit);

-- name: PurgeDemandSnapshots :execrows
DELETE FROM demand_snapshots WHERE recorded_at < $1;
