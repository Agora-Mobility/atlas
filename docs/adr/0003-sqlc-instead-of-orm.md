# ADR 0003 – SQLC instead of an ORM

- Status: accepted
- Date: 2026-10-26

## Context

Financial and geospatial queries (ledger, PostGIS) need explicit, reviewable SQL and predictable performance.

## Decision

Write SQL in `infra/database/sqlc/queries` and generate type-safe Go with SQLC on pgx/v5. Repositories in `infra/repository` map rows to domain entities. Schema comes from the golang-migrate files.

## Consequences

No hidden queries or N+1 surprises; SQL is versioned and reviewed. Generated code must be regenerated (`make sqlc`) and is checked in CI. PostGIS values are read via ST_X/ST_Y/ST_AsText in queries.
