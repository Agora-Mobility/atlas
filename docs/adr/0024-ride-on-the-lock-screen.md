# ADR 0024 – The ride on the lock screen (Live Activity and Android notification)

- Status: accepted
- Date: 2026-11-19
- Extends: ADR 0012 (notifications), ADR 0023 (smooth provider movement)

## Context

While the phone is locked, the customer app has no SSE stream. The customer
still wants to see the ride: on iOS in a Live Activity (lock screen and
Dynamic Island), on Android in an ongoing notification. Both need the server to
push the state of the ride: the status, the ETA, the distance and the position
of the driver, on every change of state and every few seconds while the driver
moves.

A Live Activity is updated by APNs pushes of type `liveactivity`, addressed to
the push token of the activity (not the device token), with topic
`<bundle>.push-type.liveactivity`. FCM does not cover this, so these pushes go
straight to APNs.

## Decision

### Tokens

- `POST /api/v1/requests/{id}/push-token` with `{ "platform": "android" | "ios", "token": "…" }`.
  Only the customer of the request, only while it is active (`409 RIDE_ENDED`
  after). One token per platform: a new one replaces the old one.
- Android sends its FCM token. iOS sends the push token of the Live Activity
  (`Activity.pushTokenUpdates`), and sends it again whenever it changes.
- Tokens live in `ride_push_tokens` and are deleted when the ride ends, or one
  by one when FCM or APNs says the token is no longer valid.

### When the server pushes

- On every transition the lock screen shows (`accepted`, `provider_en_route`,
  `in_progress`), from the `notifications.ride-live` queue: high priority, and
  on iOS with an alert, so the screen lights up.
- When the driver arrives (`POST /requests/{id}/arrive`, which is not a
  transition): phase `arrived`, high priority.
- While the trip is tracked, on the positions of the driver, at most once per
  `RIDE_LIVE_INTERVAL` (5 s) per request (a Redis claim). On iOS at priority 5,
  so it does not spend the budget of the app; it expires after 60 s.
- On `completed`, `cancelled` and `expired`: event `end`, then the tokens are
  deleted. A completed ride stays on the lock screen for
  `RIDE_LIVE_DISMISS_AFTER` (5 min); a cancelled or expired one is dismissed at
  once. If a send can be retried, the tokens stay and the event is retried.
- Position updates do not wait: they run in the background, and are dropped
  when too many sends are in flight; the next update carries the same thing.

### iOS: APNs `liveactivity`

Headers `apns-push-type: liveactivity`, `apns-topic: <APNS_BUNDLE_ID>.push-type.liveactivity`,
`apns-priority: 10` (changes of state) or `5` (positions). Token-based
authentication with the `.p8` key (ES256 JWT, renewed every 50 min).

```json
{
  "aps": {
    "timestamp": 1795600000,
    "event": "update",
    "content-state": {
      "status": "provider_en_route",
      "phase": "en_route",
      "etaSeconds": 240,
      "distanceMeters": 1800,
      "driverLat": -8.8395,
      "driverLng": 13.2345,
      "heading": 92,
      "updatedAt": 1795600000
    },
    "stale-date": 1795600120,
    "alert": { "title": "Motorista a caminho", "body": "O teu motorista está a caminho do ponto de recolha." }
  }
}
```

The `ContentState` of the app's `ActivityAttributes` has to decode these keys.
`updatedAt` is seconds since 1970 (an `Int`, not a `Date`: ActivityKit's
decoder would read a `Date` from 2001). All fields but `status`, `phase` and
`updatedAt` are optional. `end` carries the final `content-state` and a
`dismissal-date`; `timestamp` makes iOS ignore an update older than the one
shown.

### Android: FCM data only

No `notification` block, high priority: the app draws and updates the ongoing
notification itself. Every value is a string.

| key | value |
|---|---|
| `type` | `ride.live` |
| `event` | `update` or `end` |
| `request_id`, `status`, `phase` | the request, its status and the phase below |
| `eta_s`, `distance_m` | to the pickup, or to the destination during the trip (absent without a position) |
| `lat`, `lng`, `heading` | the driver (absent without a position) |
| `updated_at` | seconds since 1970 |
| `title`, `body` | a suggested text in Portuguese |

### Phases

`searching`, `en_route`, `arrived`, `in_trip`, `completed`, `cancelled`,
`expired`. `arrived` is `provider_en_route` (or `accepted`) after the driver
pressed "arrived".

ETA and distance are the straight line times `ROUTING_ROAD_FACTOR` at
`ROUTING_FALLBACK_SPEED_KMH`, like the `position` event: no Google call every
few seconds.

### Configuration

`APNS_KEY_ID`, `APNS_TEAM_ID`, `APNS_PRIVATE_KEY_BASE64` (`base64 -w0 AuthKey.p8`),
`APNS_BUNDLE_ID`, `APNS_BASE_URL` (sandbox for development builds),
`APNS_TIMEOUT`, `RIDE_LIVE_INTERVAL`, `RIDE_LIVE_STALE_AFTER`,
`RIDE_LIVE_DISMISS_AFTER`. Without the APNs key, Live Activity pushes are
written to the log, like FCM.

## Consequences

- The customer sees the ride with the phone locked, on both platforms.
- Changes of state and the end are stored in `notifications` (channel `apns`
  or `fcm`, template `ride_live`); position updates are not, there are too many.
- One APNs base URL per environment: a development build (sandbox token)
  against production APNs gets `BadDeviceToken`, and its token is deleted.
- Position updates cost one query of the tokens per tracked ride every
  `RIDE_LIVE_INTERVAL`.
