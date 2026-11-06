# ADR 0018 – Pricing: tariffs, quotes and commission

- Status: accepted
- Date: 2026-11-01
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

## Not decided

- How to collect commission on cash trips, with the wallet parked.
- Who pays for long pickups, and when a deviation from the route changes the price.
- Dynamic pricing stays out. The same route at the same hour costs everyone the same.
