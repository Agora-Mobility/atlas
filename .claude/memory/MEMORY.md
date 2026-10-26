# Atlas – project memory

Index of decisions and conventions. Details live in `docs/adr/` and `README.md`.

## Decisions
- Modular monolith, one Go binary, no microservices → `docs/adr/0001-modular-monolith.md`
- Gin for HTTP → `docs/adr/0002-gin.md`
- SQLC + pgx/v5, no ORM → `docs/adr/0003-sqlc-instead-of-orm.md`
- Event-driven: transactional outbox + RabbitMQ (`agora.events`, quorum queues, retry/DLQ, Redis dedup) → `docs/adr/0004-event-driven-outbox-rabbitmq.md`
- Zap logging with correlation_id in context → `docs/adr/0005-zap-logging.md`

## Feature ADRs (MVP 1, status: proposed)
- Auth: phone OTP + argon2id + JWT access / rotating refresh → `docs/adr/0006-authentication-otp-jwt.md`
- Provider onboarding: status machine, documents on R2 via pre-signed URLs → `docs/adr/0007-provider-onboarding-and-validation.md`
- Catalogue as data (4 seeded categories), cached in Redis → `docs/adr/0008-service-catalogue.md`
- Service request state machine, optimistic locking, history table → `docs/adr/0009-service-request-state-machine.md`
- Matching: Redis GEO for live positions, PostGIS for persistent geo, no ratings → `docs/adr/0010-geospatial-matching-postgis.md`
- Real time: SSE + per-instance fan-out queue + Redis replay → `docs/adr/0011-real-time-sse.md`
- Notifications: event consumers, FCM + SMS adapters → `docs/adr/0012-notifications-push-sms.md`
- Backoffice users/roles + append-only audit log in same transaction → `docs/adr/0013-backoffice-users-and-audit-trail.md`
- Wallet/payments/settlement/refunds/reconciliation (RF27–RF40 except RF21/RF26): no ADR yet, parked.

## Conventions
- Layers: controller → usecase → `domain/repository` interface → `infra/repository` (SQLC). `domain/` never imports `infra/`.
- Only `infra/rest/containers` receives `*pgxpool.Pool`.
- Usecase methods: `(ctx, input) (*OutputDTO, error)`; DTOs in `usecases/<module>/dtos`.
- Entities: struct + `New*()` + `isValid()` + `Update()`.
- Errors: return `shared/customerrors` AppErrors; controllers call `c.Error(err)`.
- Events: routing key `<module>.<event>.v<version>`; save via outbox inside `WithinTransaction`; handlers idempotent.
- Queues: `<module>.<purpose>`, with `.retry` and `.dlq`.
- Migrations: `make migrate-create name=...`, always with a `.down.sql`.
- Commits: Conventional Commits; branch `develop`, releases from `main`.
- Dev tools pinned in `Makefile`, installed into `./bin`.
- Never commit `.env` or `*.pdf`.

## Open items
- Wallet (Carteira) is parked: do not design it until requested.
- `example` module is a reference; remove when real modules exist.
- SSE hub is a skeleton (no stream handler, heartbeat or replay yet).
- CodeQL / SARIF / dependency review need GHAS on the private repo (`GHAS_ENABLED` variable).
