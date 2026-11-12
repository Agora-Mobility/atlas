-- name: CountRequestsByHeatCell :many
SELECT category_slug, pickup_h3_r9::bigint AS cell, count(*) AS requests
FROM service_requests
WHERE pickup_h3_r9 IS NOT NULL
  AND created_at >= sqlc.arg(from_at)::timestamptz AND created_at < sqlc.arg(to_at)::timestamptz
GROUP BY category_slug, pickup_h3_r9;

-- name: TryLockHeatmap :one
SELECT pg_try_advisory_xact_lock(hashtextextended('heatmap_cells', 0)) AS acquired;

-- name: UpsertHeatmapCells :exec
INSERT INTO heatmap_cells (window_start, resolution, category_slug, h3, count, value, updated_at)
SELECT sqlc.arg(window_start)::timestamptz, unnest(sqlc.arg(resolutions)::smallint[]), unnest(sqlc.arg(categories)::text[]),
    unnest(sqlc.arg(cells)::bigint[]), unnest(sqlc.arg(counts)::integer[]), unnest(sqlc.arg(heat_values)::double precision[]), sqlc.arg(updated_at)::timestamptz
ON CONFLICT ON CONSTRAINT heatmap_cells_key DO UPDATE
SET count = EXCLUDED.count, value = EXCLUDED.value, updated_at = EXCLUDED.updated_at;

-- name: DeleteStaleHeatmapCells :execrows
DELETE FROM heatmap_cells WHERE window_start = sqlc.arg(window_start)::timestamptz AND updated_at < sqlc.arg(updated_at)::timestamptz;

-- name: PurgeHeatmapCells :execrows
DELETE FROM heatmap_cells WHERE window_start < sqlc.arg(before)::timestamptz;

-- name: SumHeatmapCells :many
SELECT h3, sum(count)::bigint AS count, sum(value)::double precision AS value
FROM heatmap_cells
WHERE resolution = sqlc.arg(resolution)::smallint
  AND window_start >= sqlc.arg(from_at)::timestamptz AND window_start < sqlc.arg(to_at)::timestamptz
  AND h3 = ANY(sqlc.arg(cells)::bigint[])
  AND (sqlc.arg(category)::text = '' OR category_slug = sqlc.arg(category)::text)
GROUP BY h3;
