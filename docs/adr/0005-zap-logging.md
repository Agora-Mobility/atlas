# ADR 0005 – Zap for structured logging

- Status: accepted
- Date: 2026-10-26

## Context

Operations need searchable logs (Loki) correlated across HTTP requests and asynchronous event handling.

## Decision

Use Zap: JSON in staging/production, console in development. A correlation id is taken from `X-Correlation-ID` (or generated), stored in `context`, added to every log line, and carried in the event envelope to consumers.

## Consequences

Log calls take a `context.Context` (`logger.Info(ctx, ...)`). Never log secrets or personal data.
