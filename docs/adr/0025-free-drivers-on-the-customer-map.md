# ADR 0025 – Free drivers on the map of the customer, one by one

- Status: accepted
- Date: 2026-11-19
- Extends: ADR 0010 (geospatial matching), proposal 0001

## Context

`GET /nearby` answers H3 cells with a count, without identity or exact
position, and only above `NEARBY_MIN_CELL_COUNT`. That was chosen to protect
the drivers, but the "Escolha a viatura" screen cannot draw the cars around the
pickup from it, nor say how far the nearest car of each option is.

## Decision

- New `GET /api/v1/nearby/drivers?lat&lng&category[&service_id]`, customers
  only. `/nearby` stays as it is.
- Live categories (taxi): the free drivers within the first radius of the
  policy, nearest first, at most `NEARBY_DRIVERS_MAX` (20). Free means online,
  approved, active, with the vehicle the service needs, no offer pending (Redis
  reservation) and no job now (accepted, en route, in progress).
- Each driver carries what the map needs and nothing else:
  - `id`: opaque, an HMAC of the driver and the UTC day with a key derived from
    `JWT_SECRET`. The same car keeps its id while the screen polls, on every
    instance; it changes every day, so nobody can follow a driver over time.
    It is not the user id and cannot be used anywhere else.
  - `lat`, `lng` rounded to 4 decimals (about 11 m), `heading` rounded to a
    degree, `age_s` of the position.
  - `service_id`, `service_slug`, `vehicle_kind`, and `distance_m`, `eta_s` to
    the pickup (straight line times `ROUTING_ROAD_FACTOR` at
    `ROUTING_FALLBACK_SPEED_KMH`, no Google call).
- `services` sums up per service: the number of drivers and the ETA of the
  nearest, for the options of the screen. `service_id` keeps one service.
- Coverage categories (no live position) list no drivers; `available` says
  whether someone covers the point, as in `/nearby`.

## Consequences

- The customer sees the cars move around the pickup by polling the endpoint
  (every few seconds while the screen is open) and matching cars by `id`.
- A customer can see where free drivers are, to about 11 m, but not who they
  are, nor follow one across days. Drivers on a job are never shown.
- One call reads the H3 cells of the radius in Redis and the free drivers in
  one Postgres query.
