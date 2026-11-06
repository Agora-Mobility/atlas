# ADR 0017 – Mapbox behind a routing interface

- Status: superseded by ADR 0020
- Date: 2026-11-01
- Requirements: RF10, RF13
- Cycle: C3

## Context

Mapbox is the map of the whole product. A provider outage must not stop dispatch,
and Mapbox prices and cache rules are not yet confirmed.

## Decision

- `services.IRoutingProvider` has `Route` and `ETAs`. `infra/geo/mapbox` implements
  it with Directions and Matrix, using the secret token from the environment. Apps
  use their own restricted public token.
- `infra/geo/routing.Resilient` wraps it with a short timeout and a circuit breaker.
  On failure, or without a token, it answers with distance × road factor and an
  average speed, marked `fallback`.
- **Responses are not cached**, because the terms were not validated.
- Matrix is off by default (`rank_by_eta` in the policy). Dispatch ranks by distance.
- `eta_observations` holds real durations per pair of resolution 8 cells and hour.
  It is Atlas data, fed by completed trips, and it improves fallback quotes.

## Consequences

- A quote made without Mapbox is marked, so its deviation can be measured.
- Taxi completes with Mapbox down (acceptance criterion 8).
- Before launch: confirm prices, cache terms and data quality in the first cities.
