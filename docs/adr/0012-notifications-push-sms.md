# ADR 0012 – Asynchronous notifications (push and SMS)

- Status: proposed
- Date: 2026-10-26
- Requirements: RF14 (notifications); supports RF02 (OTP), RF05, RF12, RF16, RF17
- Cycle: C4 (SMS for OTP from C2)

## Context

Users must be informed of request changes while the app is in the
background, and OTPs must reach phones by SMS. External providers (FCM, SMS
operators) are slow and fail occasionally; the request flow must not wait on
them.

## Decision

- The **notifications module** is a consumer of domain events. It never
  gets called synchronously by other modules. Examples:
  - `requests.accepted.v1` → push to the customer;
  - `providers.approved.v1` → push and SMS to the provider;
  - `identity.otp-requested.v1` → SMS.
- Channels sit behind interfaces (`IPushSender`, `ISmsSender`):
  - **FCM** for Android and iOS push (APNs through FCM);
  - one adapter per **Angolan SMS operator or aggregator**, chosen in C1.
- Device tokens are stored per user and device and are removed when FCM
  reports them invalid.
- Message templates are versioned in code (Portuguese first), with
  variables filled from the event payload.
- Every send is stored in a `notifications` table (channel, template, status,
  provider message id, error) for support and auditing.
- Retries and DLQ come from the generic consumer (ADR 0004). OTP SMS has a
  shorter retry budget, because a late OTP is useless.

## Consequences

- Slow or failing external providers never block requests.
- Deliveries are at-least-once, so a user can, rarely, receive a duplicate
  push. The event-id deduplication keeps this to rare cases.
- SMS costs need monitoring (business metric per template and channel).

## Open questions

- Which SMS operator or aggregator, and its API and sender-id rules?
- Which events need SMS as well as push?
