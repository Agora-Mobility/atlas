# ADR 0014 – Dedicated `atlas` database schema

- Status: accepted
- Date: 2026-10-29
- Requirements: RNF (security)
- Cycle: C2

## Context

Every table, enum type and function was created in the `public` schema.
`public` is shared by default: it is in every role's `search_path` and,
before PostgreSQL 15, every role could create objects in it. An object
planted there can shadow ours through the `search_path` (CVE-2018-1058), and
mixing our objects with extension objects (PostGIS adds `spatial_ref_sys`,
types and functions) makes grants and backups harder to scope.

## Decision

- All application objects live in the **`atlas` schema**
  (`postgres.Schema`), including the `schema_migrations` table.
- Extensions stay in `public` (`CREATE EXTENSION postgis WITH SCHEMA public`);
  PostGIS cannot be relocated.
- Every connection uses `search_path = atlas, public`: our objects resolve
  first, extension functions and types next. The pool sets it itself, so a
  `search_path` in `DATABASE_URL` is ignored. Queries and migrations stay
  unqualified.
- The migration runner (`postgres.Migrate`) creates `atlas` when it is
  missing, moves an existing `public.schema_migrations` into it so the
  version history is kept, and runs the migrations with the same
  `search_path`. New databases create everything in `atlas` from the first
  migration.
- Migration 000007 moves the objects of databases created before this
  change from `public` to `atlas` (tables carry their indexes, constraints
  and triggers; data is kept) and revokes `CREATE` on `public` from `PUBLIC`
  when the role owns `public`.

## Consequences

- Grants, dumps (`pg_dump -n atlas`) and future read-only roles can target
  one schema.
- New migrations must not qualify names with `public.`; unqualified names
  land in `atlas`.
- sqlc still reads the schema from the migrations; 000007 uses a `DO` block,
  so sqlc keeps resolving the unqualified names.
- Rolling back 000007 moves the objects back to `public`; the `atlas`
  schema stays because it holds the migrations table.
- Where the database role is not the owner of the database (managed
  PostgreSQL), the DBA creates the `atlas` schema owned by that role and
  revokes `CREATE` on `public`.

## Update 2026-11-01

`atlas` is now the only schema of the database. Before production, migrations can
still be edited in place, so the legacy path was removed.

- PostGIS is installed inside `atlas`. Migration 000001 drops the extensions and
  schemas that the PostGIS image creates (`tiger`, `topology`) and then `public`.
- `search_path` is `atlas`, also as the default of the database.
- The role that runs the migrations must be able to create the extension and drop
  `public`. A managed PostgreSQL may need the DBA for this.
