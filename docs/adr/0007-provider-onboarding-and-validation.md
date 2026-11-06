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
  Since 2026-11-05 the sign-up takes only the phone, with no name or password: the
  name comes later with the personal data, and the provider signs in with an SMS
  code (`/auth/login/otp/*`). An unverified number gets a new code on the same
  account; a registered number answers `PHONE_ALREADY_EXISTS`.
- **Profile in one step (2026-11-05):** `PUT /api/v1/providers/me/profile` takes the
  service and the personal data together: `service_id`, name, birth date, BI number,
  province, municipality and an optional e-mail. It replaces `PUT /me/service` and
  `PUT /me/personal`. The BI is checked for format only (9 digits, 2 letters, 3
  digits). The home address with coordinates is no longer asked; `home_address_id`
  stays in the table but is not written. Province and municipality are free text
  until a catalog exists. The e-mail is set without an extra code while the profile
  is editable, because the phone was just proved by SMS.
- **Statuses** replace the ones above: `draft → pending → approved | rejected`, with
  `approved ⇄ suspended`. A rejection and a suspension need a reason. After a rejection
  the provider corrects and submits again. The profile can only change in `draft` and
  `rejected`.
- **One service:** the provider chooses a requested service. The approval sets
  `service_id`, which must be sent explicitly: the backoffice proposes the requested one
  and can change it.
- **A provider is a user:** `providers.user_id` is both the primary key and the foreign
  key to `users`, with the data of the service provider. The car lives in `vehicles`,
  one per provider.
- **Requirements are data:** `service_provider_requirements` (minimum seats, for example 7 for XL)
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
- Documents live in Cloudflare R2, and production refuses to start without it. The
  end-to-end tests use an in-memory S3 server, so they never write to a real bucket;
  `make test-r2` checks the real one.

## Update 2026-11-06: verification in AGORA Pro

- **One documents table.** Document types, slots and subjects are enums (Go and
  Postgres): `provider_document_type`, `provider_document_slot` and
  `provider_document_subject` (provider, legal representative, vehicle owner).
  `provider_documents` holds one row per type and subject with the number, the dates,
  typed `details` (OCR, liveness, categories, owner, insurer, bank, company) and
  `review_flags`. The files live in `provider_document_files`, one per slot and version:
  a new accepted file supersedes the old one, which is kept for the audit.
  `provider_document_types` is gone; the vehicle photos became one `vehicle_photo`
  document with slots.
- **Plan.** A driver has eight steps in three groups, a workshop eight in four. The
  service says which plan applies (`service_provider_requirements.verification_kind`),
  which licence categories it accepts and the oldest vehicle it takes. Maintenance opens
  to partner workshops; approving one creates its organization, workshop, hours and
  services, so it shows up in the customer search.
- **Files.** Keys are `documents/{provider}/{verification}/{type}/{slot}/{uuid}.{ext}`.
  The upload is a pre-signed PUT, confirmed with HeadObject and the magic bytes (HEIC
  included), then processed in `providers.document-processing`: sha256, size, width and
  height, EXIF and GPS removed without re-encoding, and the quality check. Reading URLs
  last five minutes. Orphan uploads are dropped after a day.
- **Checks are ports.** `IImageQualityChecker`, `IDocumentOCRProvider` and
  `ILivenessProvider` have a `fake` adapter (dev and tests) and a `manual` one that leaves
  everything to the team; production refuses `fake`. No vendor is chosen yet. The selfie
  is liveness only, with no face match. The card is read in `providers.identity-reading`
  with at most three attempts before manual entry.
- **Review flags** mark what the team must look at: manual or edited card data, an
  expired card with a SIAC receipt, a liveness that failed three times, a vehicle of
  someone else, another insurer, a company name that differs, the repair activity of the
  licence and the representative in the certificate.
- **IBAN** is not encrypted: it is not a secret, and the risk is an account takeover that
  changes it, which the audit and the review cover. It is masked in every answer and
  never logged, like the BI number.
- **Expiry.** A periodic job warns 30 days before the insurance, the inspection, the
  driving licence and the business licence expire.

## Still open

- The bank codes and the insurers are seeded from public lists: confirm them with the
  official lists of BNA and ARSEG.
- Choose and integrate the OCR, quality and liveness vendors.
- A workshop owner has no partner panel access yet: the organization is created on
  approval, but no membership.
- The maximum vehicle age per service is not set (`max_vehicle_age_years` is empty).
- The plate is not unique across providers.
