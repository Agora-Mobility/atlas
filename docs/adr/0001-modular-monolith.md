# ADR 0001 – Modular monolith

- Status: accepted
- Date: 2026-10-26

## Context

AGORA must ship MVP 1 in 18 weeks with a small team. The domains (identity, providers, catalogue, requests, matching, wallet, payments, notifications, admin) are well known but still evolving.

## Decision

Build one Go binary (`atlas`) organised as internal modules. Modules share no tables through code, expose behaviour through usecases and communicate asynchronously through domain events. No microservices.

## Consequences

One deploy, one CI pipeline and in-process transactions (critical for payments). Module boundaries must be kept by convention and review; a module can later be extracted because it already talks through events. Scaling is horizontal (more API instances), not per module.
