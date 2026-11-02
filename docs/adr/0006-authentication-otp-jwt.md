# ADR 0006 – Authentication with OTP and JWT

- Status: accepted
- Date: 2026-10-28
- Requirements: RF01 (customer sign-up), RF02 (authentication), RF03 (customer profile), RF04 (provider sign-up)
- Cycle: C2

## Context

Customers and providers sign up and log in from the mobile apps; operators
and admins use the backoffice. The plan requires OTP during registration and
password recovery. Phone numbers are the main identity in Angola, and the API
runs as several stateless instances behind a load balancer.

## Decision

- **Identity module** owns credentials; the **users module** owns profiles.
- Sign-up and password recovery verify the phone number with a **6-digit OTP**
  sent by SMS (through the notifications module, ADR 0012).
  - OTPs are stored hashed in Redis with a 5-minute TTL, at most 5 attempts,
    and a resend cooldown per phone number.
- Login uses phone + password for every role, on one endpoint; `X-Client-Id`
  decides which accounts are accepted. The request never names an
  organization: staff enter through their oldest active membership in the
  scope of the client, and the token carries it. Passwords are hashed with
  **argon2id**.
- Customers and providers can also sign in with an **SMS code**, on its own
  endpoints and with its own OTP purpose, so a sign-up or recovery code never
  signs anyone in here. Staff clients are closed: an SMS code alone must not
  replace the password and the MFA of a staff account.
- Sessions use a **short-lived JWT access token** (15 min, HS256, claims:
  `sub`, `role`, `sid` = session/token family, `cid` = client id) and a
  **rotating refresh token** (opaque, stored hashed in PostgreSQL, 30 days,
  bound to the client that created it). Reusing a rotated refresh token
  revokes the whole token family.
- **Ending a session takes effect at once.** Logout, logout-all, password
  change or recovery, account deletion, refresh-token reuse and a block by
  the backoffice put the session id (`sid`) in a Redis denylist
  (`<app>:revoked-session:<sid>`) for as long as an access token lives.
  `RequireAuth` rejects a denied `sid` with `SESSION_REVOKED` (401). When
  every session of a user ends, the sessions with a token issued within the
  access-token lifetime are denied. The denylist is written inside the same
  transaction as the change, so the change rolls back if Redis fails.
  `RequireAuth` **fails closed**: if Redis cannot be reached it answers
  `AUTH_UNAVAILABLE` (503) instead of accepting the token.
- Roles: `customer`, `provider`, `partner`, `operator`, `admin`
  (`shared/enums.Role`), enforced by `middleware.Authenticator`
  (`RequireAuth` / `RequireRole`).
- **Every `/api/v1` request identifies its client application** with the
  `X-Client-Id` header. Clients live in the `clients` table (`client_type`
  enum) and are seeded by migration 000003:

  | Client id | Type | Roles that sign in | Self-registration creates |
  |---|---|---|---|
  | `agora-customer-app` | `customer_app` | customer | customer |
  | `agora-provider-app` | `provider_app` | provider | provider |
  | `agora-partner-panel` | `partner_panel` | partner | not allowed |
  | `agora-backoffice` | `backoffice` | operator, admin | not allowed |

- **The role never comes from the request body.** Registration takes the role
  from the client type; login, refresh and phone verification only succeed
  when the client accepts the role stored on the user (`CLIENT_NOT_ALLOWED`).
  `partner`, `operator` and `admin` accounts are created by an admin.
- `RequireAuth` rejects an access token used with a different `X-Client-Id`
  than the one it was issued for (`TOKEN_CLIENT_MISMATCH`), and refresh rejects
  a refresh token presented by another client.
- **One set of auth endpoints for every role:**

  | Method | Path | Auth | Purpose |
  |---|---|---|---|
  | POST | `/api/v1/auth/register` | client | Sign up with the phone only, sends OTP (resends it while unverified) |
  | POST | `/api/v1/auth/login` | client | Returns a session |
  | POST | `/api/v1/auth/login/otp/request` | client | Send a sign-in OTP (always 202) |
  | POST | `/api/v1/auth/login/otp/verify` | client | Sign in with the OTP, returns a session |
  | POST | `/api/v1/auth/refresh` | client | Rotate refresh token |
  | POST | `/api/v1/auth/phone/verification-code` | client | Resend the OTP of an unverified account (always 202) |
  | POST | `/api/v1/auth/phone/verify` | client | Verify phone, returns a session |
  | POST | `/api/v1/auth/recovery/request` | client | Send reset OTP (always 202) |
  | POST | `/api/v1/auth/recovery/confirm` | client | Reset with OTP, revokes all sessions |
  | POST | `/api/v1/auth/logout` | client + bearer | Revoke the current session |
  | POST | `/api/v1/auth/logout-all` | client + bearer | Revoke every session |
  | GET | `/api/v1/users/me` | client + bearer | Authenticated user |
  | PATCH | `/api/v1/users/me` | client + bearer | Update name / e-mail (e-mail needs the current password) |
  | DELETE | `/api/v1/users/me` | client + bearer | Delete a customer or provider account, revokes all sessions |
  | POST | `/api/v1/users/me/phone/verification-code` | client + bearer | Send an OTP to a new phone |
  | PUT | `/api/v1/users/me/phone` | client + bearer | Confirm the new phone with the OTP |
  | POST | `/api/v1/users/password` | client + bearer | Set the first password of an account that has none, keeps the sessions |
  | PUT | `/api/v1/users/password` | client + bearer | Change password, revokes all sessions |

- **Changing the phone** sends the OTP to the new number with the subject
  `<user id>:<phone>`, so only the user who asked for it can use the code.
- **Deleting an account** soft-deletes the user; the phone and e-mail can be
  registered again. Staff accounts are not deleted this way.
- **Backoffice user management** (list, create staff, block, unblock, reset
  password) lives under `/api/v1/admin/users` and is described in ADR 0013.
- **The first admin** of an environment is created by `cmd/bootstrap-admin`
  (shipped in the image as `/bootstrap-admin`) from `BOOTSTRAP_ADMIN_*`
  variables. It does nothing when an admin already exists.

- Login of a user whose phone is not verified fails with
  `PHONE_NOT_VERIFIED` (403), so the app can send the user to the OTP screen.
- Unknown phone numbers still run one argon2id verification, so response time
  does not reveal which accounts exist. Argon2id runs are bounded by a
  semaphore sized to the CPU count.
- Hashes use the PHC format; outdated parameters are re-hashed on login.
- Register, login, phone OTP (verification and phone change) and recovery
  routes each have their own rate limit scope (`AUTH_RATE_LIMIT_*`),
  stricter than the global limiter.
- OTP and recovery requests answer 202 even when no SMS is sent (unknown
  phone, other client, resend cooldown, SMS unavailable), so they do not
  reveal which accounts exist.
- SMS goes through **CastBrick** (`github.com/IldySilva/castbrick-go`,
  `CASTBRICK_*` variables) behind `services.ISMSSender`. `CASTBRICK_API_KEY`
  is required in production; without it, other environments write the SMS
  to the log. Provider failures (invalid key, no credits, timeout) become
  `ErrSMSUnavailable` and are logged at error level without the message
  content.
- E-mail goes through **Resend** (`github.com/resend/resend-go/v3`,
  `RESEND_*` and `EMAIL_FROM`/`EMAIL_REPLY_TO` variables) behind
  `services.IEmailSender`. `RESEND_API_KEY` is required in production and
  needs `EMAIL_FROM` on a domain verified in Resend; without the key, other
  environments write the e-mail to the log. Provider failures become
  `ErrEmailUnavailable`, and the recipient is masked in the logs.
- Usecases return sentinel errors (`errors.Is`); controllers map each one to
  an HTTP status and a stable error code.
- Events: `identity.user-registered.v1`, `identity.phone-verified.v1`,
  `identity.phone-changed.v1`, `identity.user-blocked.v1`,
  `identity.user-unblocked.v1`, `identity.user-deleted.v1`.

## Consequences

- Access tokens are verified without a database hit; each authenticated
  request costs one Redis `EXISTS` for the session denylist. A logout,
  block or password change ends the session at once instead of after up to
  15 minutes. Redis becomes a hard dependency of authenticated routes.
- `X-Client-Id` is public (it ships inside the apps), so it is a channel
  policy, not a secret: it stops staff accounts from signing in through the
  public apps and tokens from being replayed across clients, but it does not
  authenticate the application itself.
- Backoffice and partner users without a phone number cannot use SMS
  recovery; an admin resets their password (`PUT /api/v1/admin/users/{id}/password`).
- SMS cost and delivery depend on the operator integration (ADR 0012).
- Refresh-token rotation locks the token row (`FOR UPDATE`). Two concurrent
  refreshes with the same token are treated as reuse and end that session,
  so clients must serialise refreshes.

## Open questions

- Does the backoffice need 2FA for admins in MVP 1?
- Can a single phone number be both a customer and a provider?
