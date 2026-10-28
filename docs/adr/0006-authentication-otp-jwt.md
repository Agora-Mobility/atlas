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
- Login uses phone (or e-mail for backoffice users) + password. Passwords are
  hashed with **argon2id**.
- Sessions use a **short-lived JWT access token** (15 min, HS256, claims:
  `sub`, `role`, `sid` = session/token family, `cid` = client id) and a
  **rotating refresh token** (opaque, stored hashed in PostgreSQL, 30 days,
  bound to the client that created it). Reusing a rotated refresh token
  revokes the whole token family.
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
  | POST | `/api/v1/auth/register` | client | Sign up, sends OTP |
  | POST | `/api/v1/auth/login` | client | Returns a session |
  | POST | `/api/v1/auth/refresh` | client | Rotate refresh token |
  | POST | `/api/v1/auth/phone/verification-code` | client | Resend phone OTP (always 202) |
  | POST | `/api/v1/auth/phone/verify` | client | Verify phone, returns a session |
  | POST | `/api/v1/auth/recovery/request` | client | Send reset OTP (always 202) |
  | POST | `/api/v1/auth/recovery/confirm` | client | Reset with OTP, revokes all sessions |
  | POST | `/api/v1/auth/logout` | client + bearer | Revoke the current session |
  | POST | `/api/v1/auth/logout-all` | client + bearer | Revoke every session |
  | GET | `/api/v1/users/me` | client + bearer | Authenticated user |
  | PUT | `/api/v1/users/password` | client + bearer | Change password, revokes all sessions |

- Login of a user whose phone is not verified fails with
  `PHONE_NOT_VERIFIED` (403), so the app can send the user to the OTP screen.
- Unknown identifiers still run one argon2id verification, so response time
  does not reveal which accounts exist. Argon2id runs are bounded by a
  semaphore sized to the CPU count.
- Hashes use the PHC format; outdated parameters are re-hashed on login.
- Register, login and OTP/recovery routes each have their own rate limit
  scope (`AUTH_RATE_LIMIT_*`), stricter than the global limiter.
- OTP and recovery requests answer 202 even when no SMS is sent (unknown
  phone, other client, resend cooldown, SMS unavailable), so they do not
  reveal which accounts exist.
- SMS goes through **CastBrick** (`github.com/IldySilva/castbrick-go`,
  `CASTBRICK_*` variables) behind `services.ISMSSender`. `CASTBRICK_API_KEY`
  is required in production; without it, other environments write the SMS
  to the log. Provider failures (invalid key, no credits, timeout) become
  `ErrSMSUnavailable` and are logged at error level without the message
  content.
- Usecases return sentinel errors (`errors.Is`); controllers map each one to
  an HTTP status and a stable error code.
- Events: `identity.user-registered.v1`, `identity.phone-verified.v1`.

## Consequences

- Access tokens are verified without a database hit, which keeps the API
  stateless. A logout or ban takes effect when the access token expires
  (at most 15 minutes) unless an explicit denylist is added later.
- `X-Client-Id` is public (it ships inside the apps), so it is a channel
  policy, not a secret: it stops staff accounts from signing in through the
  public apps and tokens from being replayed across clients, but it does not
  authenticate the application itself.
- Backoffice and partner users without a phone number cannot use SMS
  recovery; an admin resets their password.
- SMS cost and delivery depend on the operator integration (ADR 0012).
- Refresh-token rotation locks the token row (`FOR UPDATE`). Two concurrent
  refreshes with the same token are treated as reuse and end that session,
  so clients must serialise refreshes.

## Open questions

- Does the backoffice need 2FA for admins in MVP 1?
- Can a single phone number be both a customer and a provider?
