# ADR 0008 – Service catalogue as data

- Status: proposed
- Date: 2026-10-26
- Requirements: RF06 (category management), RF07 (service registration), RF08 (service lookup), RF24 (service management)
- Cycle: C3

## Context

MVP 1 launches with four categories: Taxi, Towing (Reboque), Removals
(Mudanças) and Maintenance (Manutenção). Categories and services must be
managed from the backoffice, and future categories should not need a release.

## Decision

- Categories and services are **database rows**, not Go enums. The four MVP
  categories are created by a migration with stable `slug` values (`taxi`,
  `towing`, `removals`, `maintenance`) that code may reference.
- A **service** belongs to one category and defines:
  - name and description;
  - whether it needs a destination (Taxi, Towing, Removals) or only a location
    (Maintenance);
  - active/inactive flag;
  - pricing parameters (base fee, per-km, per-minute) stored as configurable
    values in cêntimos of kwanza (integers, never floating point), with the
    actual tariff rules decided in C1.
- Categories and services are soft-deleted (`active = false`) so that
  historical requests keep their references.
- Catalogue reads are public and **cached in Redis** (`infra/cache`), with the
  cache invalidated on every backoffice change.
- Events: `catalog.category-created.v1`, `catalog.category-updated.v1`,
  `catalog.service-created.v1` and `catalog.service-updated.v1`, consumed if
  other modules need denormalised copies. Code uses the US spelling
  (`catalog`), which the linter enforces.

## Consequences

- New categories or services do not need a deploy, only backoffice changes.
- Behaviour that differs per category (for example, a destination is
  required) is driven by service attributes, not `switch` statements on the
  category.
- Price calculation is kept out of the catalogue until the tariff rules are
  approved.

## Open questions

- Tariff model per category (fixed, distance-based, quote by provider)?

## Update 2026-11-01 (proposal 0001)

- What differs per category is now data in `dispatch_policies`, optionally per
  zone: candidate source, H3 resolution, radii, offer mode, deadlines, channels,
  ranking, tracking, what happens when no provider is found, and the pricing
  mode. No `switch` on the category exists in the code.
- Migration 000015 seeds one default policy per MVP category, from section 4 of
  the proposal. Those numbers are starting points to calibrate.
- Currency is no longer assumed: it lives in `service_zones` and
  `service_tariffs`.
- The open question on the tariff model is closed: `fixed` for Taxi and Towing,
  `call_out` for Maintenance and `quote` for Removals. See ADR 0018. The fee
  columns of `services` stay as a catalog hint; prices come from
  `service_tariffs`.
