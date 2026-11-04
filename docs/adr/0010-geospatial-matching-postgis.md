# ADR 0010 – Provider matching with PostGIS, without ratings

- Status: proposed
- Date: 2026-10-26
- Requirements: RF10 (location), RF11 (available providers)
- Cycle: C3

## Context

A request must list the eligible providers near the customer. MVP 1
explicitly excludes ratings from matching (RF19/RF20 are out of scope).
Providers send their position frequently while online, so the write volume
of location updates is far higher than the read volume of searches.

## Decision

- **Current provider position** lives in **Redis GEO** (`GEOADD`), keyed per
  category, together with an online/availability flag that expires (TTL)
  when the app stops sending heartbeats. This absorbs the high-frequency
  writes without touching PostgreSQL.
- **PostGIS** (`geography(Point, 4326)` with a GIST index) stores
  addresses, request pickup/destination points and service areas, and
  answers persistent queries (history, reports, area checks).
- A provider is **eligible** when:
  1. its status is `approved` (ADR 0007);
  2. it is approved for the request's category;
  3. it is online and not busy with another request;
  4. it is within the search radius of the pickup point.
- Results are ordered by **straight-line distance** (Redis
  `GEOSEARCH ... ASC`), and the radius expands in steps (e.g. 3 → 5 → 10 km)
  when no one is found. ETA based on road routing is out of MVP 1.
- Matching is a domain service behind an interface
  (`domain/repository.IProviderLocator`), so the ranking can later add
  ratings, acceptance rate or routing ETA without touching usecases.
- Location points are validated as WGS84 coordinates (`valueobjects.GeoPoint`).

## Consequences

- Fast, cheap matching suitable for MVP 1 scale. Redis becomes part of the
  critical path for matching, so its availability matters.
- Straight-line distance can rank badly where roads are indirect; acceptable
  for MVP 1 and replaceable later.
- Positions in Redis are ephemeral. If tracking history is needed, it must
  be sampled into PostgreSQL explicitly.

## Open questions

- Default and maximum radius per category (towing likely needs more than taxi).
- Location update frequency from the provider app (battery vs. accuracy).

## Update 2026-11-01 (proposal 0001)

The Redis GEO index is replaced. H3 is the index and never the source of truth:
exact distances are computed in Go and official polygons stay in PostGIS.

- **Live index (Taxi, Towing):** `live:pos:{provider}` hash with the expiring
  presence, and `live:idx:{category}:r{res}:{cell}` sets. A position only touches
  the sets when the cell changes. A provider with no valid position is removed
  from a set on the next read.
- **Coverage index (Maintenance, Removals):** `provider_coverage` in PostgreSQL,
  computed once when the provider saves a base and a radius. No H3 extension is
  needed: cells fit a positive `BIGINT`.
- **Rings:** `k = ceil(radius / (1.5 × min_edge)) + 1`, with the minimum edge
  measured in African cities. `infra/geo/h3index` tests this against 17 cities,
  because a `k` that is too small hides providers without any error.
- **Zones:** interior H3 cells of each zone are cached in memory; a cell on a
  border or with no match falls back to `ST_Covers`.
- `IProviderLocator` is the interface foreseen by this ADR
  (`domain/repository/dispatch.go`).
- The open questions are closed by the policy (radii) and by the cadence decided
  by the server (`entities.CadenceFor`).
- **One provider, one service (2026-11-01):** like Yango and Uber, a provider is tied
  to one vehicle and one service, set by the backoffice (`provider_profiles.service_id`).
  Economy and Comfort drivers never receive each other's requests. The live index stays
  per category, so eligibility is checked before the candidate cap, and again on accept.
- **Addresses (2026-11-01):** `addresses` holds every place with mandatory `lat` and
  `lng` and a generated PostGIS point. `service_requests` (pickup, destination),
  `service_request_stops`, `providers` (home, base) and `user_addresses` point to it.
  A request gets its own address rows, so changing a saved address never rewrites
  the history. Fare estimates keep plain coordinates: they are a quote, not a place.
