# ADR 0023 – Smooth provider movement on the customer map

- Status: accepted
- Date: 2026-11-18
- Extends: ADR 0010 (geospatial matching, cadence), ADR 0011 (real-time SSE)

## Context

While the provider drives to the pickup, the customer app moves the car only
when a `position` event arrives. The provider app reports on the cadence the
server returns (`entities.CadenceFor`): every 7 s or 30 m on the way to the
pickup, every 10 s or 50 m during the trip. The app animates each jump in about
0.9 s, so the car slides for a moment and then stands still for 6 to 9 s.

`POST /providers/me/locations` already takes a batch of up to 100 points, each
with `age_s`. `ReportPositions` validates the whole batch but
`shareWithCustomer` sends only the last accepted point (`positionPayload`:
`lat`, `lng`, `heading`, `eta_s`). The app has no way to know when the next
point is due or how fast the car is going, and the intermediate points of the
batch are lost to it.

During the trip the customer is inside the car, so the customer app can move
the car with the phone's own GPS. That phase needs nothing from the server.

## Decision

The `position` event gains three optional fields. Apps that do not read them
keep working; nothing else in the payload changes.

```json
{
  "class": "ephemeral",
  "request_id": "…",
  "lat": -8.8147, "lng": 13.2302, "heading": 92, "eta_s": 240,
  "next_in_s": 7,
  "speed_mps": 9.4,
  "points": [
    { "lat": -8.8139, "lng": 13.2291, "heading": 90, "dt_ms": -6000 },
    { "lat": -8.8143, "lng": 13.2297, "heading": 91, "dt_ms": -3000 }
  ]
}
```

- `next_in_s`: the interval of the cadence the server just returned to the
  provider, so the app can animate the car over the whole interval instead of
  a fixed 0.9 s.
- `speed_mps`: distance over time between the last two accepted points
  (this batch, or the previous stored position), only when the time between
  them is positive. The app can use it to keep the car moving if the next
  event is late.
- `points`: the other accepted points of the batch, oldest first, without the
  last one (which stays in `lat`/`lng`). `dt_ms` is the time before the last
  point, from `age_s`, so it does not depend on any clock. At most 20
  points (`sharedPointsMax`); the oldest are dropped. Absent when
  the batch had a single point.

The cadence moves from constants to configuration, with today's values as
defaults, so it can be tuned in production without a release:

| Variable | Default |
| --- | --- |
| `CADENCE_IDLE_INTERVAL`, `CADENCE_IDLE_DISTANCE_M` | 30s, 150 |
| `CADENCE_APPROACH_INTERVAL`, `CADENCE_APPROACH_DISTANCE_M` | 7s, 30 |
| `CADENCE_TRIP_INTERVAL`, `CADENCE_TRIP_DISTANCE_M` | 10s, 50 |
| `CADENCE_SLOWDOWN_FACTOR` (battery low, weak network) | 2 |

Config validation refuses non-positive values, and an approach or trip
interval below 2 s.

## Requirements on the apps

- **Provider app:** read the GPS often (about every second, or by distance)
  and send the points of each interval in one batch, as the endpoint already
  allows. If it sends a single point per interval, `points` stays empty and
  only `next_in_s` and `speed_mps` help.
- **Customer app:** on the way to the pickup, play `points` in order and then
  animate to `lat`/`lng` over `next_in_s`, along the route it already draws
  (`route_geometry` of the ETA), turning the car with `heading`. During the
  trip, move the car with the phone's GPS and hide the default blue dot.

## Consequences

- No extra requests: the same batch now reaches the customer in full. The
  event grows by up to 20 points (about 1.5 KB) every 7 to 10 s per active
  trip.
- The car on the customer map is shown about one interval behind reality on
  the way to the pickup, in exchange for continuous movement.
- If the motion is still not smooth enough, `CADENCE_APPROACH_INTERVAL` can be
  lowered (for example to 5 s): each active trip goes from about 9 to 12
  location requests a minute, and the provider phone spends more battery and
  data.
- The backoffice is not affected: it reads the last position from the request
  detail.
