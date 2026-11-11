# ADR 0021 – Staff sign in with the e-mail

- Status: accepted
- Date: 2026-11-11
- Amends: ADR 0019 (identifier of the staff)

## Context

Every account signed in with the phone number and the password. The backoffice
and the partner panel are used at a desk, by people the platform invites by
e-mail, and their screens ask for the e-mail.

## Decision

- `POST /api/v1/auth/login` takes `email` and `password` from the staff clients
  (`agora-backoffice`, `agora-partner-panel`) and `phone` and `password` from
  the apps (`agora-customer-app`, `agora-provider-app`). The client decides; the
  other identifier is ignored, and a missing one answers `422 VALIDATION_ERROR`
  on that field.
- The e-mail is compared without case or surrounding spaces, as the unique index
  `users_email_key` on `lower(email)` already does.
- Every staff account has an e-mail: invitations and `bootstrap-admin` require
  it, and `PATCH /users/me` no longer lets a staff member clear it.
- A wrong pair still answers the generic `401 INVALID_CREDENTIALS` and spends the
  hash time, so the answer does not reveal which e-mails exist.
- Invitations still activate with the SMS code, and the phone stays on the
  staff account for notifications and recovery.

## Consequences

- The backoffice and partner panel send `email` instead of `phone`; sending the
  phone now fails with 422.
- Staff created before this change sign in with the e-mail they were invited
  with.
