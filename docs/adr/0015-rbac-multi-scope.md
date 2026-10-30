# ADR 0015 – Permission-based access control in two scopes (RF21)

- Status: accepted
- Date: 2026-10-29
- Requirements: RF21 (user management), RF26 (operational audit), RF02 (authentication)
- Supersedes: the `operator`/`admin`/`partner` roles and `RequireRole` of ADR 0013
- Spec: `specs/RF21-gestao-de-utilizadores/spec.md`

## Context

Staff were a fixed role in `users.role` (`operator`, `admin`, `partner`) and
routes were guarded by role names. Every operator had the same access, partners
had no organization, and the role of a new staff user came from the request
body. RF21 asks for one authorization engine for the AGORA team (backoffice
`agora-backoffice`) and for each partner company (`agora-partner-panel`),
isolated from each other.

## Decision

- A **user** is an identity only. `users.role` is the account kind:
  `customer`, `provider` or `staff`. Customers and providers keep access by
  ownership and are outside RBAC.
- A **membership** ties a staff user to an **organization** (`platform`, the
  single AGORA organization, or `partner`). It has a status (`invited`,
  `active`, `suspended`, `deactivated`), roles and a `perm_version`.
- An organization is a company. It has an optional, unique **NIF**
  (migration 000011): 10 digits for a company, or the BI number (9 digits,
  2 letters, 3 digits) for a sole trader. Spaces are dropped and letters
  upper-cased before it is stored.
- A **role** is a named set of **permissions** (`resource:action`) of one
  scope. System roles are seeded by migration and cannot be changed, not even
  directly in the database (trigger). Custom roles per organization are
  supported by the model; there are no endpoints to create them yet.
- **Endpoints check permissions, never role names**
  (`Authorizer.RequirePermission`). The organization and scope always come
  from the token, never from the request.
- The access token carries `mid` (membership), `oid` (organization), `scp`
  (scope) and `pv` (permission version) besides `sid` and `cid`. Permissions
  are not in the token: they are resolved per request from Redis
  (`PERMISSION_CACHE_TTL`) with a database fallback, and the cache entry is
  deleted on every change, so role changes, suspensions and deactivations
  apply on the next request.
- Rules enforced in the service: no privilege escalation (grant only roles
  whose permissions you hold, and do not manage members with permissions you
  lack); same organization and scope for roles (also a trigger); nothing on
  yourself; the last active Super Admin or Owner stays (the organization row
  is locked while checking); a deactivated membership is final; other tenants
  answer 404. Every change is audited in the same transaction, with the
  organization in `audit_log.organization_id`.

## Answers to the open questions of the spec

| Id | Decision |
|---|---|
| Q1 | Providers stay outside RBAC for now; partner providers are a later RF22 change. |
| Q2 | Invitations and activation codes go by **SMS** (the only channel the API has). The SMS holds a link to `INVITATION_URL_BACKOFFICE` or `INVITATION_URL_PARTNER_PANEL` with the token. E-mail can be added behind the same flow. |
| Q3 | Password policy and token lifetimes stay as in ADR 0006 for now. |
| Q4 | `/api/v1/admin/users` now manages **customer and provider accounts only** (permissions `users:read`, `users:block`, `users:reset-password`). Staff are invited and managed through `/api/v1/members`. Migration 000010 moves `admin` to a Super Admin membership and `operator` to Operações; `partner` users become `staff` without membership (there was no partner organization) and must be invited again. The table keeps the name `audit_log`. |
| Q5 | The prefix stays `/api/v1`. |
| Q6 | Client ids stay (`agora-backoffice`, `agora-partner-panel`, …). |
| Q7 | AGORA creates partner organizations at `POST /api/v1/partners` (`partners:manage`) and invites their first owner at `POST /api/v1/partners/{orgId}/members/invitations`. |
| Q8 | A deactivated member cannot be invited again to the same organization (`INVALID_MEMBER_STATE`). |
| Q9 | The client picks the scope; `organization_id` in the login picks one partner when the person belongs to several, otherwise the oldest active membership is used. Switching needs a new sign-in. |
| Q10 | Not answered by the plan; no extra targets are set here. |
| Q11 | **TOTP** (authenticator app, RFC 6238). The secret is sealed with AES-256-GCM (`MFA_ENCRYPTION_KEY`), each time step is accepted once. |
| Q12 | A staff identity is shared across organizations, but not with customer or provider accounts: inviting their e-mail or phone answers `IDENTITY_IN_USE`. |
| Q13 | `sid` and `cid` stay. `role` stays and is `staff` for every member; the membership claims are added. |
| Q14 | RF21 is brought forward from C7. |
| Q15 | RLS is enabled with a dedicated `atlas_app` role (see below); no second login is needed. |

## Row-level security (RN16, Q15)

- Migration 000012 creates **`atlas_app`**, a `NOLOGIN NOBYPASSRLS` role that
  owns nothing, and grants it to the migration user. The API keeps one
  login (`DATABASE_URL`) and runs `SET ROLE atlas_app` on every pooled
  connection (`postgres.Connection`). Migrations and the seed keep the
  owner (`postgres.OwnerConnection`). This matters because the docker user is a
  superuser, and superusers bypass RLS even with `FORCE`.
- `atlas_app` has DML on every table (default privileges cover future
  ones) except `schema_migrations`, and only `SELECT`/`INSERT` on
  `audit_log`.
- `memberships`, `membership_roles`, `invitations`, `roles` and
  `role_permissions` have `ENABLE` and `FORCE ROW LEVEL SECURITY`. A
  membership is visible when it belongs to `atlas.organization_id` or to
  `atlas.user_id` (sign-in, refresh and MFA know the person before the
  organization) and is written only in `atlas.organization_id`; the other
  tables follow the visible memberships and roles. Without a tenant nothing
  is visible.
- The tenant travels in the context (`shared/tenant`): the authorizer sets
  it from the token, and the use cases set it when acting on a partner. The
  RLS repositories always run in a transaction (`postgres.Scoped`) that
  sets both values with `set_config(..., true)`, so a pooled connection never
  keeps a previous tenant.
- Two narrow `SECURITY DEFINER` functions cross the boundary on purpose:
  `invitation_scope(token_hash)` (an invitation is opened by its token
  alone) and `user_has_membership_outside(user, org)` (MFA reset rule).
- In production the migration user needs `CREATEROLE`, or the DBA runs
  `CREATE ROLE atlas_app NOLOGIN NOBYPASSRLS` and `GRANT atlas_app TO <user>`
  first. A data migration that must touch these tables as a non-superuser
  owner sets the tenant or disables `FORCE` for the change.

## MFA recovery

- Confirming the authenticator returns **10 recovery codes** once
  (`mfa_recovery_codes` keeps only SHA-256 hashes). `POST /auth/mfa/recover`
  finishes a sign-in with one of them (single use, counts against the
  attempts of the MFA step, audited). `POST /users/me/mfa/recovery-codes`
  replaces them after a fresh authenticator code.
- `POST /members/{id}/mfa/reset` (`members:mfa-reset`, given to Super Admin,
  Administrador and Proprietário) removes the authenticator and the codes
  and ends every session; the next sign-in asks for a new setup. The usual
  rules apply (not yourself, not someone with more permissions). Because the
  authenticator belongs to the person, a partner cannot reset someone who also
  belongs to another organization; AGORA can.

## Custom roles

- `POST/PATCH/DELETE /roles` (`roles:manage`, AGORA only, as in the spec) and
  `GET /roles/{id}`. A custom role belongs to the organization of the
  session, holds only permissions of its scope that the actor holds, cannot
  reuse a system code, and cannot be deleted while assigned. Changing its
  permissions or MFA requirement bumps `perm_version` of every holder and
  invalidates their cached access.

## Events

Every change is published through the outbox in the same transaction:
`access.member-invited`, `-activated`, `-roles-changed`, `-suspended`,
`-reactivated`, `-deactivated`, `-mfa-reset`, `access.role-created`,
`-updated`, `-deleted` and `access.partner-created` (all `v1`).

## Consequences

- Staff sign-in may answer `202` with an `mfa_token` instead of a session
  (`/auth/mfa/setup`, `/auth/mfa/verify`). Super Admin, Financeiro (platform)
  and Proprietário (partner) always require MFA.
- Revoking access is immediate: suspensions end every session of the person
  (ADR 0006 revocation list) and permission changes invalidate the cache.
- `MFA_ENCRYPTION_KEY` is a new required setting.
