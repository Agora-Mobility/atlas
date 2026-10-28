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
| Logs           | Zap (JSON, `LOG_LEVEL` / `LOG_OUTPUT`)             |
| Database       | PostgreSQL 18 + PostGIS 3.6, pgx/v5 + pgxpool      |
| Data access    | SQLC (no ORM)                                      |
| Migrations     | golang-migrate (embedded, `cmd/migrate` binary)    |
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
| `domain/` | Entities, repository and service interfaces, event contracts |
| `usecases/<module>/` | Application logic + DTOs |
| `infra/rest/` | Server (`server.go`), routes, controllers, routers, DI containers |
| `infra/repository/` | Repository implementations over SQLC |
| `infra/database/` | pgxpool, transactions, migrations runner, seeders, SQLC queries/code, Redis |
| `infra/queue/`, `infra/outbox/` | RabbitMQ manager/publisher/consumer, outbox relay |
| `infra/middleware/` | Client (`X-Client-Id`), authentication, correlation id, logging, recovery, metrics, rate limit, CORS, security headers |
| `infra/security/` | JWT, argon2id, opaque refresh tokens, OTP |
| `infra/sse/` | SSE hub (skeleton) |
| `shared/` | HTTP error responses, validation errors, enums, pagination |
| `migrations/` | `.up.sql` / `.down.sql` pairs |
| `cmd/` | Auxiliary binaries: `migrate`, `seed`, `healthcheck` |
| `docs/adr/` | Architecture decision records |

The `auth` module is the reference implementation across every layer. Copy
its shape for new modules.

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
make docker-up        # postgres, redis, rabbitmq, prometheus, grafana, api (migrates on start)
```

Or run the API on the host against the compose dependencies:

```bash
docker compose up -d postgres redis rabbitmq
make migrate-up seed run
```

| Service | URL |
|---|---|
| API | http://localhost:8080 (`/live`, `/ready`, `/metrics`) |
| API docs – Scalar (only `APP_ENV=development`) | http://localhost:8080/docs (spec: `/docs/openapi.json`) |
| RabbitMQ UI | http://localhost:15672 |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3000 |

Every `/api/v1` request needs the `X-Client-Id` header. The client decides
which roles can sign in and which role a self-registration creates:

| Client id | Type | Roles that sign in | Registration creates |
|---|---|---|---|
| `agora-customer-app` | `customer_app` | customer | customer |
| `agora-provider-app` | `provider_app` | provider | provider |
| `agora-partner-panel` | `partner_panel` | partner | not allowed |
| `agora-backoffice` | `backoffice` | operator, admin | not allowed |

Protected routes also need `Authorization: Bearer <access_token>` from
`POST /api/v1/auth/login`. `make seed` creates one user per role (password
`Agora@2026`):

```bash
curl -s localhost:8080/api/v1/auth/login \
  -H 'X-Client-Id: agora-backoffice' -H 'Content-Type: application/json' \
  -d '{"identifier":"admin@agora.ao","password":"Agora@2026"}'
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
