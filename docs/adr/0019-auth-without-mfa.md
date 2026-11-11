# ADR 0019 – Authentication without MFA

- Status: accepted
- Date: 2026-11-04
- Requirements: RF21
- Supersedes: the MFA sections of ADR 0015

## Decision

- Every account signs in with an identifier and the password alone (the phone
  number; for staff the e-mail since ADR 0021). There is no second factor, no
  authenticator app and no recovery codes.
- Migration `000036_remove_mfa` drops the storage (`user_mfa`,
  `mfa_recovery_codes`), the `members:mfa-reset` permission and the
  `roles.requires_mfa` column. Roles no longer require MFA.
- The endpoints `/auth/mfa/setup`, `/auth/mfa/verify`, `/auth/mfa/recover`,
  `/users/me/mfa/recovery-codes` and `/members/{id}/mfa/reset` are gone.
  `POST /auth/login` answers 200 with the session for every client.
- The remaining protections stay: argon2id passwords, immediate revocation of
  sessions, `perm_version`, rate limits, RLS and the generic answers that do
  not reveal which accounts exist.

## Consequences

- A leaked password is enough to enter any account, including Super Admin.
  Compensating controls move to monitoring and password policy.
- The `access.member-mfa-reset` event and its e-mail notice are gone.
- If MFA comes back, it returns as a new ADR with new storage; the old tables
  are not restored.
