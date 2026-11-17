# ADR 0022 – Heatmap of pre-aggregated H3 cells

- Status: accepted
- Date: 2026-11-12
- Extends: ADR 0016 (H3 with cgo)

## Context

The backoffice draws demand as hexagons with deck.gl `H3HexagonLayer`, so it
needs `{ h3, value }` per cell for the part of the map on screen, at a
resolution that fits the zoom. `GET /admin/dispatch/heatmap` reads
`service_requests` directly at resolution 8 and has no viewport, so it cannot
zoom and gets slower as the table grows.

## Decision

- Every request keeps `pickup_h3_r9` next to `pickup_h3_r8`. It is a `BIGINT`,
  like the other H3 columns. An H3 index is a 64-bit integer: 8 bytes against
  15 or more as text, an integer B-tree comparison, and `h3-go` reads it with no
  parsing. The API always writes it as the hexadecimal string, because
  JavaScript loses precision above 2^53.
- Resolution 9 is fixed by the column name. The roll-up resolutions (`8,7,6`)
  are configuration.
- A job runs every `HEATMAP_INTERVAL` (5 min) on every instance, and
  `pg_try_advisory_xact_lock` lets only one of them work. It recomputes the
  current window and the one before it (`HEATMAP_LOOKBACK_WINDOWS`, windows of
  `HEATMAP_WINDOW` = 15 min aligned to UTC) from `service_requests`. It rolls
  the counts up with `cellToParent`, smooths each resolution with
  `value = 0.6 × own + 0.4 × mean(gridDisk(c, 1) − c)`, and writes
  `heatmap_cells` in one transaction. Neighbors without requests count as 0,
  and they are stored too (count 0, value > 0), so the map fades out instead of
  ending at a hard edge.
- The write is an upsert on `(window_start, resolution, category_slug, h3)`.
  Rows of the window that this run did not touch are then deleted. Running the
  job twice gives the same rows: nothing is duplicated or added twice, and a
  cell that empties disappears.
- `category_slug` is part of the key, so the map can be filtered by category.
  Smoothing is linear, so the sum over categories is the smoothed sum.
- `GET /api/v1/admin/dispatch/heatmap/cells` is the new endpoint. It needs the
  same permission as the other dispatch maps (`requests.manage`), and the old
  `/heatmap` stays as it is.
  - `bbox` is rounded outward to 0.001°, which is both the cell set and the
    cache key.
  - `zoom` picks the resolution with `HEATMAP_ZOOM_RESOLUTIONS`.
  - Cells come from `polygonToCells`, by cell center.
  - The windows in `[from, to)` are summed.
  - The answer is cached in Redis for `HEATMAP_CACHE_TTL` (60 s).
- Above `HEATMAP_MAX_CELLS` (20 000) cells, the endpoint moves to the next
  coarser stored resolution, and `resolution` in the answer says which one it
  used. Only when even resolution 6 does not fit does it answer
  `400 AREA_TOO_LARGE`. The area is estimated before the fill, so a box the
  size of the country is refused without allocating millions of cells.
- The period defaults to the last complete window and spans at most
  `HEATMAP_MAX_PERIOD` (7 days). Rows older than `HEATMAP_RETENTION` (90 days)
  are purged by the job.
- A demand/supply ratio is left out: supply only exists live in Redis, with no
  history per window.

## Backfill

Requests created before migration 000046 (`pickup_h3_r9`) have `pickup_h3_r9` NULL and are
ignored by the job until they are backfilled:

```sh
make backfill-h3                      # sets the r9 cell in batches of 1000
make backfill-h3 batch=500 rebuild=168h  # also rebuilds the last 7 days of heatmap_cells
# in the container
docker compose exec api /backfill-h3 -batch 1000 -rebuild 168h
```

The command can be run again at any time: it only touches rows that are still
NULL, walks the table by id, and waits when the API job holds the lock. Once it
is done, a later migration can make `pickup_h3_r9` `NOT NULL`.

## Consequences

- One `DELETE` + upsert per window and resolution every 5 minutes, which is
  small for a city.
- Requests reach the map up to `HEATMAP_INTERVAL` late.
- Migration 000046 adds two indexes on `service_requests`, and the table is
  locked for writes while they are built. That is acceptable at the current
  size; with a large table, build them `CONCURRENTLY` by hand first.
