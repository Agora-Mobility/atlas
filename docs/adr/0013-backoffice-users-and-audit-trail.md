# ADR 0013 – Backoffice users, roles and operational audit trail

- Status: proposed
- Date: 2026-10-26
- Requirements: RF21 (user management), RF26 (operational audit)
- Cycle: C7 (audit middleware usable from C2)

## Context

Operators and admins manage providers, requests and users from the
backoffice. Sensitive actions must be traceable: approving or suspending a
provider, cancelling a request, changing a user's role. RNF requirements
expect events, logs and auditing to be active in every module.

## Decision

- **Backoffice users** are created by admins only. There is no
  self-registration. Roles are `operator` (day-to-day operation) and `admin`
  (everything, including user management). Access is enforced with
  `RequireRole`.
- Admins can block or unblock customers and providers. Blocking revokes
  their refresh tokens (ADR 0006).
- An **append-only `audit_log` table** records every state-changing
  backoffice and system action with:
  - actor id and role;
  - action (e.g. `provider.approve`);
  - entity type and id;
  - `before` and `after` snapshots (JSONB, sensitive fields removed);
  - reason;
  - correlation id, IP, user agent and timestamp.
- Audit entries are written **in the same transaction** as the change
  (same pattern as the outbox), so no change exists without its audit
  record. The application database user has only `INSERT`/`SELECT` on
  `audit_log`, with no `UPDATE` or `DELETE`.
- The backoffice reads the audit log with filters (actor, entity, period)
  and pagination.
- Technical logs (Zap/Loki) and the audit log are separate. Logs are for
  debugging and expire; the audit log is a business record and is kept.

## Consequences

- Full traceability of operations, which helps support and any regulatory
  review.
- Each usecase that changes state must call the audit repository. This is
  enforced by review and tests, not automatically.
- The audit table grows forever and needs partitioning by month once
  volumes justify it.

## Open questions

- Retention period of the audit log (legal requirement in Angola?).
- Should customers and providers see their own activity log?
