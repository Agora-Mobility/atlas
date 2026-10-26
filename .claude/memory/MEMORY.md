# Atlas – project memory

Index of decisions and conventions. Details live in `docs/adr/` and `README.md`.

## Decisions
- Modular monolith, one Go binary, no microservices → `docs/adr/0001-modular-monolith.md`
- Gin for HTTP → `docs/adr/0002-gin.md`
- SQLC + pgx/v5, no ORM → `docs/adr/0003-sqlc-instead-of-orm.md`
- Event-driven: transactional outbox + RabbitMQ (`agora.events`, quorum queues, retry/DLQ, Redis dedup) → `docs/adr/0004-event-driven-outbox-rabbitmq.md`
- Zap logging with correlation_id in context → `docs/adr/0005-zap-logging.md`

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
