# ADR 0011 – Real-time updates with SSE fed by RabbitMQ

- Status: proposed
- Date: 2026-10-26
- Requirements: RF13 (real-time tracking), RF12 (offers to providers)
- Cycle: C4

## Context

Customers follow the request status and the provider's position; providers
receive offers. Updates flow almost entirely from server to client, through
Cloudflare, a load balancer and Nginx, to several API instances.

## Decision

- Use **Server-Sent Events** (`GET /events`, routed by Nginx), not
  WebSockets. Client → server actions (accept, start, location updates) stay
  on REST.
- Each API instance runs an **SSE hub** (`infra/sse`) holding its open
  connections by user id.
- Each instance consumes domain events through its own **exclusive,
  auto-delete queue** bound to `agora.events`, which gives fan-out to every
  instance. The hub pushes an event only to the users connected to that
  instance and concerned by it (customer and provider of the request).
- Every pushed event gets a sequential id per user and is also appended to a
  **Redis stream per user** (capped, e.g. last 200 events / 24 h). On
  reconnect the client sends `Last-Event-ID` and missed events are replayed.
  If the id is older than the stream, the server sends a `resync` event and
  the app reloads the state through REST.
- A **heartbeat** comment is sent every 20 s, below the ~100 s idle timeout
  of the Cloudflare proxy. Nginx uses `proxy_buffering off` and a long
  `proxy_read_timeout` on `/events`.
- Authentication: Bearer token on connection. When the token expires the
  server closes the stream and the client reconnects with a fresh token.

- SSE routes use `middleware.Streaming()`, which lifts the HTTP write
  deadline for that request only. Every other route keeps
  `HTTP_WRITE_TIMEOUT`.

## Consequences

- No sticky sessions are needed: any instance can serve any user.
- Every instance receives every event (fan-out), which is fine at MVP 1
  volumes. At larger scale, routing per user or a dedicated gateway can be
  introduced.
- Mobile apps must handle reconnection and `resync`. Push notifications
  (ADR 0012) cover the app in the background, since SSE only works while the
  app is open.

## Open questions

- How often to push the provider's position to the customer (throttling).
