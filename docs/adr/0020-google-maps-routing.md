# ADR 0020 – Google Maps behind the routing interface

- Status: accepted
- Date: 2026-11-05
- Requirements: RF10, RF13
- Supersedes: ADR 0017

## Context

The product moves from Mapbox to Google Maps. Dispatch, H3 indexing, the circuit
breaker and the fallback estimate do not depend on the provider, so they stay.

## Decision

- `infra/geo/googlemaps` implements `services.IRoutingProvider` with the Routes API:
  `computeRoutes` for `Route` and `computeRouteMatrix` for `ETAs`. The server key
  travels in a header and never in the URL. Apps use their own restricted keys.
- Routes use traffic (`TRAFFIC_AWARE`). The matrix does not (`TRAFFIC_UNAWARE`), as
  before with the Mapbox `driving` profile.
- `infra/geo/routing.Resilient` is unchanged: timeout, circuit breaker, and distance ×
  road factor marked `fallback` on failure or without a key.
- **Responses are not cached**, as in ADR 0017.
- Matrix stays off by default (`rank_by_eta` in the policy).
- `route_source` is now `google`. `mapbox` stays valid in `fare_estimates` for older
  quotes, which really came from Mapbox.

## Consequences

- `route.geometry` is now a Google encoded polyline, precision 5. With Mapbox it was
  precision 6, so apps must decode it with 5.
- Clients see `google` instead of `mapbox` in `route_source` and `source`.
- The metric is now `atlas_business_routing_calls_total`. The old Mapbox series ends.
- Without `GOOGLE_MAPS_API_KEY` the API still starts and every quote is `fallback`.
- Before launch: confirm prices, data quality in the first cities, and whether storing
  `distance_m` and `duration_s` in `fare_estimates` fits the Google Maps Platform terms.
