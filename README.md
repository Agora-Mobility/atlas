# atlas

Core API of the AGORA platform: authentication, catalogue, service requests,
provider matching, real-time events, digital wallet, payments and settlement.

A **modular monolith** in Go with an **event-driven** core: domain modules
live in one binary and talk to each other through events (transactional
outbox → RabbitMQ), not network calls.

## Stack

| Concern        | Choice                                             |
|----------------|----------------------------------------------------|
| Language       | Go 1.27                                            |
| HTTP           | Gin                                                |
| Logs           | Zap (JSON in staging/production, console in dev)   |
| Database       | PostgreSQL 18 + PostGIS 3.6, pgx/v5 + pgxpool      |
| Data access    | SQLC (no ORM)                                      |
| Migrations     | golang-migrate (embedded in the binary)            |
| Cache          | Redis (go-redis v9)                                |
| Messaging      | RabbitMQ 4 (amqp091-go), quorum queues             |
| Observability  | Prometheus + Grafana                               |

## Architecture

```
infra/rest/<module>/controllers  →  usecases/<module>  →  domain/repository (interfaces)
                                                                ↑
                                         infra/repository (SQLC)  →  PostgreSQL
```

- Controllers never touch the database or SQLC.
- Usecases depend only on `domain` interfaces.
- `domain/` imports nothing from `infra/`.
- `infra/rest/containers` is the only place that receives the `*pgxpool.Pool`.

| Folder | Purpose |
|---|---|
| `config/` | Environment loading/validation, logger |
| `domain/` | Entities, value objects, validators, repository interfaces, event contracts |
| `usecases/<module>/` | Application logic + DTOs |
| `infra/rest/` | Controllers, routers, DI containers, server |
| `infra/repository/` | Repository implementations over SQLC |
| `infra/database/` | pgxpool, transactions, migrations runner, seeders, SQLC queries/code, Redis |
| `infra/queue/`, `infra/outbox/` | RabbitMQ manager/publisher/consumer, outbox relay |
| `infra/middleware/` | Correlation id, logging, recovery, metrics, rate limit, CORS, security headers, errors |
| `infra/security/auth-jwt/` | JWT issue/verify, auth and role middlewares |
| `infra/sse/` | SSE hub (skeleton) |
| `shared/` | Errors, enums, pagination |
| `migrations/` | `.up.sql` / `.down.sql` pairs |
| `docs/adr/` | Architecture decision records |

## Event flow

1. A usecase writes the state change **and** the event to the `outbox` table
   in the same transaction (`ITransactionManager.WithinTransaction`).
2. The outbox relay (goroutine in the same binary) locks pending rows with
   `FOR UPDATE SKIP LOCKED`, publishes them to the `agora.events` topic
   exchange with routing key `<module>.<event>.v<version>` using publisher
   confirms, and marks them published.
3. Each consumer owns a quorum queue bound to the routing keys it needs. On
   failure the message goes to `<queue>.retry` with a per-message TTL and comes
   back; after `RABBITMQ_MAX_RETRIES` it goes to `<queue>.dlq`.
4. Consumers skip events already processed (Redis, keyed by `event_id`).
   Delivery is at-least-once, so handlers must be idempotent.

Envelope: `event_id`, `event_type`, `version`, `occurred_at`,
`correlation_id`, `aggregate_type`, `aggregate_id`, `payload`.

## Running locally

```bash
cp .env.example .env
make tools            # pinned dev tools into ./bin
make docker-up        # postgres, redis, rabbitmq, prometheus, grafana, migrate, api
```

Or run the API on the host against the compose dependencies:

```bash
docker compose up -d postgres redis rabbitmq
make migrate-up seed run
```

| Service | URL |
|---|---|
| API | http://localhost:8080 (`/live`, `/ready`, `/metrics`) |
| Swagger (non-production) | http://localhost:8080/swagger/index.html |
| RabbitMQ UI | http://localhost:15672 |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3000 |

Protected routes need a token. In development:

```bash
go run . token admin    # or: docker compose exec api /atlas token admin
```

## Make targets

Run `make help` for the full list. Main ones:

| Target | What it does |
|---|---|
| `run`, `build` | Run / build the API |
| `test`, `test-race`, `test-integration`, `cover` | Tests (integration uses testcontainers) |
| `lint`, `fmt`, `tidy-check` | golangci-lint, formatting, go.mod check |
| `sqlc`, `sqlc-check` | Generate / verify SQLC code |
| `migrate-up`, `migrate-down`, `migrate-create name=...` | Migrations |
| `seed` | Development seed data |
| `swag` | Regenerate the OpenAPI spec |
| `docker-up`, `docker-down`, `docker-logs` | Compose stack |
| `security` | gosec, govulncheck, gitleaks |
| `tools`, `pre-commit-install` | Dev tools and git hooks |

## Conventions

- Commits follow Conventional Commits (enforced by the commit-msg hook).
- Branches: work on `develop`, release from `main`, tags `vX.Y.Z`.
- Configuration comes only from environment variables, validated at startup.
- Never commit `.env` or confidential documents (`*.pdf` is ignored).
