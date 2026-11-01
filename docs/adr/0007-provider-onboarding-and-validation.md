# ADR 0007 – Provider onboarding and validation

- Status: proposed
- Date: 2026-10-26
- Requirements: RF04 (provider sign-up), RF05 (provider validation), RF22 (provider management)
- Cycle: C2

## Context

Providers apply with documents. An operator must approve them before they
can receive requests, and must be able to reject or suspend them later. The
MVP 1 acceptance criteria require a provider approved in the backoffice by
the end of C2.

## Decision

- A provider has an explicit status, with every transition recorded in the
  audit trail (ADR 0013):

  ```
  pending_documents → under_review → approved
                                   ↘ rejected (reason required)
  approved ⇄ suspended (reason required)
  ```

- Only `approved` providers are eligible for matching (ADR 0010).
- **Documents** (ID, driving licence, vehicle papers, etc.) are uploaded to
  **Cloudflare R2** with **pre-signed URLs**, so files never pass through the
  API. The database stores only metadata: type, object key, checksum, status
  and review notes.
  - Buckets are private and read access is through short-lived pre-signed URLs.
- Each provider is linked to the service categories it can serve (ADR 0008).
  Approval can be partial per category, for example approved for towing but
  not for removals.
- Backoffice actions approve, reject and suspend through
  `RequireRole(operator, admin)`.
- Events:
  - `providers.application-submitted.v1`
  - `providers.approved.v1`
  - `providers.rejected.v1`
  - `providers.suspended.v1`

  Notifications (ADR 0012) and matching (ADR 0010) consume them.

## Consequences

- The API never streams large files, which keeps memory and bandwidth low.
- R2 credentials and the bucket policy become part of the infrastructure setup.
- The required document types per category must be agreed with AGORA
  (business rules, C1).

## Open questions

- Which documents are mandatory per category (Taxi, Towing, Removals,
  Maintenance)?
- Do documents expire, for example a driving licence, and force a re-review?

## Update 2026-11-01

Providers create their own accounts, like Yango and Uber. The backoffice reviews
them and never creates one.

- **Sign-up:** `POST /api/v1/providers/register`, only through the provider app. The
  account type comes from the client. The plain `POST /auth/register` no longer creates
  providers. The phone code is confirmed with `POST /auth/phone/verify`.
- **Statuses** replace the ones above: `draft → pending → approved | rejected`, with
  `approved ⇄ suspended`. A rejection and a suspension need a reason. After a rejection
  the provider corrects and submits again. The profile can only change in `draft` and
  `rejected`.
- **One service:** the provider chooses a requested service. The approval sets
  `service_id`, which must be sent explicitly: the backoffice proposes the requested one
  and can change it.
- **Requirements are data:** `service_onboarding` (minimum seats, for example 7 for XL)
  and `service_required_documents`. A service with no row there does not accept
  providers yet, so Towing, Maintenance and Removals stay closed for now. Partial
  approval per category no longer exists.
- **Documents:** the API validates type and size, returns a pre-signed PUT, and on
  registration checks that the object exists, belongs to that provider and really has an
  accepted content. Keys are `providers/{provider_id}/{document_type}/{uuid}.{ext}`, with
  no client file names. Each document is approved or rejected on its own, and a new file
  replaces the old one, which is deleted.
- **Approval** is refused unless every document required by the chosen service is
  approved and the vehicle meets the minimum.
- **The backend enforces it:** a provider who is not approved gets 403 when going online
  or answering an offer.
- Local development uses MinIO in `docker-compose.yml`. Production uses a private
  Cloudflare R2 bucket and refuses to start without it.

## Still open

- Documents do not expire yet, for example a driving licence or the insurance.
- The plate is not unique across providers.
