# ADR 0009 – Service request lifecycle as an explicit state machine

- Status: proposed
- Date: 2026-10-26
- Requirements: RF09 (service request), RF12 (accept/reject), RF13 (tracking), RF16 (service start), RF17 (service completion), RF18 (history), RF23 (request management)
- Cycles: C3–C4

## Context

A request goes through several actors (customer, provider, operator) and is
mirrored in real time on the apps. Invalid transitions must be impossible,
such as completing a request that never started. Concurrent actions also have
to be safe, for example two providers accepting the same request at once.

## Decision

- The `ServiceRequest` entity owns an explicit state machine. Transitions are
  methods on the entity that validate the current state and the actor:

  ```
  created → searching → offered → accepted → provider_en_route → in_progress → completed
               │           │          │               │
               └───────────┴──────────┴───────────────┴──→ cancelled (by customer / provider / operator, reason required)
  searching → expired (no provider within timeout)
  offered   → searching (provider rejected or offer timed out)
  ```

- Every transition is persisted with an **optimistic lock** (`version`
  column, `UPDATE ... WHERE id = $1 AND version = $2`). Losing a race returns
  a `409 Conflict`, so at most one provider can accept a request.
- Every transition is written to a `service_request_events` history table
  (who, when, from, to, reason). This history serves RF18 and the audit trail.
- Every transition emits `requests.<transition>.v1` through the outbox, for
  example `requests.accepted.v1` or `requests.completed.v1`. Real-time updates
  (ADR 0011) and notifications (ADR 0012) consume these events.
- Timeouts (offer expiry, search expiry) are handled by a scheduler in the
  binary that scans due requests with `FOR UPDATE SKIP LOCKED`, the same
  pattern as the outbox relay.
- The `paid` states are left out until the wallet and payments are designed.
  The machine is extended then.

## Consequences

- State rules live in one place and are unit-testable without a database.
- The history table grows with every transition and needs a date index and,
  later, archiving.
- Adding payment states later requires a migration of the state enum and new
  transitions, not a redesign.

## Open questions

- Offer strategy: one provider at a time (sequential offers) or broadcast to
  the N nearest, first to accept wins?
- Offer and search timeouts per category.
- Cancellation rules and fees (depend on payments, parked).
