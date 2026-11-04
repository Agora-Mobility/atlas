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
| `infra/database/` | pgxpool, transactions, migrations runner, seeders, SQLC queries/code, Redis. Every object lives in the `atlas` schema (ADR 0014) |
| `infra/queue/`, `infra/outbox/` | RabbitMQ manager/publisher/consumer, outbox relay |
| `infra/middleware/` | Client (`X-Client-Id`), authentication, correlation id, logging, recovery, metrics, rate limit, CORS, security headers |
| `infra/security/` | JWT, argon2id, opaque refresh tokens, OTP, session revocation |
| `infra/sse/` | SSE hub (skeleton) |
| `shared/` | HTTP error responses, validation errors, enums, pagination |
| `migrations/` | `.up.sql` / `.down.sql` pairs |
| `cmd/` | Auxiliary binaries: `migrate`, `seed`, `bootstrap-admin`, `healthcheck` |
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
make docker-up        # postgres, redis, rabbitmq, prometheus, grafana, api (Dockerfile.dev: entrypoint.sh migrates, then air hot reload)
```

Or run the API on the host against the compose dependencies:

```bash
docker compose up -d postgres redis rabbitmq
make migrate-up seed run
```

| Service | URL |
|---|---|
| API | http://localhost:8700 (`/live`, `/ready`, `/metrics`) |
| API docs – Scalar (only `APP_ENV=development`) | http://localhost:8700/docs (spec: `/docs/openapi.json`) |
| RabbitMQ UI | http://localhost:15672 |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3000 |

Every `/api/v1` request needs the `X-Client-Id` header. The client decides
which roles can sign in and which role a self-registration creates:

| Client id | Type | Roles that sign in | Registration creates |
|---|---|---|---|
| `agora-customer-app` | `customer_app` | customer | customer |
| `agora-provider-app` | `provider_app` | provider | provider |
| `agora-partner-panel` | `partner_panel` | staff with a partner membership | not allowed |
| `agora-backoffice` | `backoffice` | staff with an AGORA membership | not allowed |

Staff access is permission based (ADR 0015): each membership has roles, each
role a set of permissions, and every staff endpoint checks one permission.
Super Admin, Financeiro and Proprietário must set up an authenticator app: the
login answers `202` with an `mfa_token`, then `POST /api/v1/auth/mfa/setup` and
`POST /api/v1/auth/mfa/verify` start the session.

Protected routes also need `Authorization: Bearer <access_token>` from
`POST /api/v1/auth/login`, which takes the phone and the password. `make seed`
creates a customer, a provider, a Super Admin (`+244923000101`, asks for MFA),
an Operações member (`+244923000102`) and the owner of a test partner
(`+244923000105`), all with password `Agora@2026`:

```bash
curl -s localhost:8700/api/v1/auth/login \
  -H 'X-Client-Id: agora-backoffice' -H 'Content-Type: application/json' \
  -d '{"phone":"+244923000102","password":"Agora@2026"}'
```

The API runs every query as the `atlas_app` role, which the migrations create
and which is subject to row-level security (ADR 0015). In production the
migration user needs `CREATEROLE`, or the DBA creates the role first.

The seed does not run in production. To create the first Super Admin there,
run `/bootstrap-admin` from the api image once, with `BOOTSTRAP_ADMIN_EMAIL`,
`BOOTSTRAP_ADMIN_PHONE` and `BOOTSTRAP_ADMIN_PASSWORD` (12+ characters) set. It does nothing when an
active Super Admin already exists. Further staff are invited by SMS and
e-mail (`POST /api/v1/members/invitations`) and accept at
`POST /api/v1/invitations/accept`.

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
| `bootstrap-admin` | Create the first Super Admin when none exists |
| `swag` | Regenerate the OpenAPI spec |
| `docker-up`, `docker-down`, `docker-logs` | Compose stack |
| `security` | gosec, govulncheck, gitleaks |
| `tools`, `pre-commit-install` | Dev tools and git hooks |

## Releases and images

Images live in the public Docker Hub repository `docker.io/agoramobility/atlas`
and are built only by `.github/workflows/cd.yml`, once per commit on `main`.
The workflows log in with the repository variable `DOCKERHUB_USERNAME` and the
repository secret `DOCKERHUB_TOKEN` (Settings → Secrets and variables → Actions). The same
image (same digest) is promoted from staging to production; nothing is rebuilt.

| Tag | Meaning | Mutable |
|---|---|---|
| `sha-1a2b3c4` | Build of that commit on `main` | no |
| `1.4.2`, `1.4` | Release `v1.4.2` (`1.4` follows the latest patch) | `1.4.2` no |
| `staging` | What staging runs | yes |
| `production`, `latest` | What production runs | yes |

Flow:

1. Merging `develop` into `main` builds `sha-…`, signs it (cosign keyless),
   attests the SBOM and deploys it to the `staging` environment.
2. release-please keeps a release PR open with the next version and the
   CHANGELOG, computed from the Conventional Commits (`feat` → minor,
   `fix` → patch, `!` → major; minor while on `0.x`).
3. Merging the release PR tags `vX.Y.Z`, publishes the GitHub release with the
   image digest and SBOM, and deploys that image to `production`. Merging the
   release PR is the production approval.
4. Merge `main` back into `develop` after a release so the CHANGELOG and the
   manifest stay in sync.

Rollback: Actions → **Rollback** → choose the environment and a previous tag
(`1.4.1` for production, `1.4.1` or `sha-…` for staging). The workflow checks
that the image was signed by `cd.yml` on `main`, moves the environment tag to it
and redeploys. Nothing is rebuilt, so the rollback is the exact bytes that ran
before.

## Conventions

- Commits follow Conventional Commits (enforced by the commit-msg hook).
- Branches: work on `develop`, release from `main`, tags `vX.Y.Z` (created by release-please, never by hand).
- Configuration comes only from environment variables, validated at startup.
- Never commit `.env` or confidential documents (`*.pdf` is ignored).

## Dispatch and real time

Proposal: `docs/proposals/0001-despacho-geoespacial-e-tempo-real.md`. Decisions:
ADR 0008 to 0012 and 0016 to 0018.

- `infra/geo/h3index`: the only package that imports H3. The API needs a C compiler
  (`CGO_ENABLED=1`). After pulling this change, rebuild the dev image: `make docker-up`.
- `usecases/dispatch`: policies, candidates, offers, quotes, watcher and fares.
- `usecases/realtime` and `infra/sse`: projector, mailbox, signal and stream.
- Policies, zones and tariffs are data managed under `/api/v1/admin`. `make seed`
  creates a rough Luanda zone and illustrative tariffs, for development only.

### Tests

```bash
make test              # unit
make test-integration  # repositories, Redis and backoffice flows (Docker)
make test-e2e          # full stack and acceptance criteria of the proposal (Docker)
make test-load         # k6 against a disposable stack (Docker and k6)
```

## Provider sign-up

Providers apply in the app and the backoffice reviews them (ADR 0007).

1. `POST /api/v1/providers/register`, then `POST /api/v1/auth/phone/verify`.
2. `GET /api/v1/providers/services`, then `PUT /api/v1/providers/me/service`.
3. `PUT /api/v1/providers/me/personal` and `PUT /api/v1/providers/me/vehicle`.
4. For each document: `POST /api/v1/providers/me/documents/upload-url`, PUT the file to
   the returned URL with the returned headers, then `POST /api/v1/providers/me/documents`.
5. `POST /api/v1/providers/me/submit`. `GET /api/v1/providers/me/profile` shows what is missing.

Documents go to Cloudflare R2 (`R2_*` in `.env`). `make test-r2` checks the credentials
against the real bucket. The other test suites never touch R2.
