# ADR 0006 – Authentication with OTP and JWT

- Status: proposed
- Date: 2026-10-26
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
  `sub`, `role`) and a **rotating refresh token** (opaque, stored hashed in
  PostgreSQL, 30 days). Reusing a revoked refresh token revokes the whole
  token family.
- Roles: `customer`, `provider`, `operator`, `admin` (`shared/enums.Role`),
  enforced by `authjwt.RequireAuth` / `RequireRole`.
- Login, OTP request and OTP verification have stricter rate limits than the
  global limiter.
- Events: `identity.user-registered.v1`, `identity.phone-verified.v1`.

## Consequences

- Access tokens are verified without a database hit, which keeps the API
  stateless. A logout or ban takes effect when the access token expires
  (at most 15 minutes) unless an explicit denylist is added later.
- SMS cost and delivery depend on the operator integration (ADR 0012).
- Refresh-token rotation needs careful handling of concurrent refreshes
  from the same device.

## Open questions

- Does the backoffice need 2FA for admins in MVP 1?
- Can a single phone number be both a customer and a provider?
