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
