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
    values, with the actual tariff rules decided in C1.
- Categories and services are soft-deleted (`active = false`) so that
  historical requests keep their references.
- Catalogue reads are public and **cached in Redis** (`infra/cache`), with the
  cache invalidated on every backoffice change.
- Events: `catalogue.service-updated.v1`, consumed if other modules need
  denormalised copies.

## Consequences

- New categories or services do not need a deploy, only backoffice changes.
- Behaviour that differs per category (for example, a destination is
  required) is driven by service attributes, not `switch` statements on the
  category.
- Price calculation is kept out of the catalogue until the tariff rules are
  approved.

## Open questions

- Tariff model per category (fixed, distance-based, quote by provider)?
