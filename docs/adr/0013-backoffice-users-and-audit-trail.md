# ADR 0013 – Backoffice users, roles and operational audit trail

- Status: proposed; the roles part is superseded by ADR 0015
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

## Implementation (C2)

User management and the audit table are in place:

| Method | Path | Roles | Purpose |
|---|---|---|---|
| GET | `/api/v1/admin/users` | `users:read` | Customer and provider accounts, with `role`, `status`, `search` filters and pagination |
| GET | `/api/v1/admin/users/{id}` | `users:read` | One customer or provider account |
| GET | `/api/v1/admin/block-reasons` | `users:block` | Active block reasons |
| POST | `/api/v1/admin/users/{id}/block` | `users:block` | Block with one or more reasons and an optional note; ends every session at once |
| POST | `/api/v1/admin/users/{id}/unblock` | `users:block` | Unblock |
| PUT | `/api/v1/admin/users/{id}/password` | `users:reset-password` | Reset the password; ends every session at once |

- These endpoints manage customer and provider accounts only. Staff are
  invited and managed as members (ADR 0015); nobody blocks or resets their
  own account.
- Block reasons are a catalogue (`block_reasons`, migration 000008). Each
  block is kept in `user_blocks` with its reasons in `user_block_reasons`;
  the audit entry and the `identity.user-blocked` event carry the reason
  codes and the note.
- `audit_log` (migration 000006) is append-only through a trigger that
  rejects `UPDATE`, `DELETE` and `TRUNCATE`, so it holds whatever database
  user the API runs with. Snapshots use the public user view, never the
  password hash. System actions (the admin bootstrap) have no actor.
- Reading the audit log from the backoffice is still planned for C7.

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
