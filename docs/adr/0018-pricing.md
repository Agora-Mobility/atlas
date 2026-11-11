# ADR 0018 – Pricing: tariffs, quotes and commission

- Status: accepted
- Date: 2026-11-01, updated 2026-11-11
- Requirements: RF09
- Cycle: C3

## Decision

- `service_tariffs` holds versioned tariffs per service and optional zone. A trigger
  refuses edits: a change closes the current version and creates another, so the
  history explains every price.
- `price = max(minimum, base + km × per_km + minutes × per_minute) + zone fee + night fee`,
  always in integers.
- `POST /api/v1/fare-estimates` returns one fixed price per active service, valid for
  `FARE_ESTIMATE_TTL`. `POST /api/v1/requests` freezes it. An expired estimate answers
  409 with new quotes and creates nothing. A moved pin invalidates it.
- "Raise the offer" adds 10 or 20% over the original price, all of it for the provider,
  and starts a new search round. Providers who already refused see the higher offer.
- Every change to the quoted price is a row in `fare_adjustments` (`boost`, `waiting`,
  `route_change`).
- Commission is `commission_bps` of the tariff, on the price before any raise. It starts
  at 0 in the seeds.
- Demand pricing (proposal 0002): every minute one instance compares, in each H3 area of
  a zone (`DEMAND_AREA_RESOLUTION`), the distinct customers waiting or quoting with the
  free providers. The smoothed ratio `(customers + 1) / (free + 1)` maps to a factor
  through tiers configured per zone and category, with a cap and a mode: `off`,
  `observe` (measures and records without charging, to calibrate) or `on`. The quote applies the
  factor once and keeps it; a route change reuses it. The surge part is a `demand`
  adjustment. Commission on it is off unless `commission_on_surge` is set. Scheduled rides
  and removals stay at factor 1, and any failure reading demand falls back to 1.
- The price is never recomputed from the real distance. On completion the real distance
  (GPS samples) and duration are stored next to the quote in `request_trip_metrics`, and a
  deviation above `ROUTE_REVIEW_ABOVE_PERCENT` marks the ride for review. A shorter ride
  is only marked when the samples cover at least 60% of it, since GPS gaps shorten the
  measured distance.

## Not decided

- How to collect commission on cash trips, with the wallet parked.
- Who pays for long pickups.
- Whether a ride that ends well before the destination is repriced down.
- Tier values, caps and the share of the surge that goes to the provider (client
  decision; see proposal 0002, section 12).
