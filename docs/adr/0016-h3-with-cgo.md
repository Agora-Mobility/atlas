# ADR 0016 – H3 as a C library, linked statically

- Status: accepted
- Date: 2026-11-01
- Requirements: RF10, RF11
- Cycle: C3

## Context

Proposal 0001 uses H3 for the live index and for coverage. `uber/h3-go` v4.5.0
does not compile with `CGO_ENABLED=0`, the only pure Go library found declares
itself experimental and lacks `GridDisk`, and a PostgreSQL extension would send
every position to the database. The API image is `distroless/static`.

## Decision

- The API is built with `CGO_ENABLED=1`, static linking and the `netgo` and
  `osusergo` tags, so the image stays `distroless/static`.
- `migrate`, `healthcheck` and `bootstrap-admin` stay pure Go.
- `Dockerfile` installs a cross compiler when the target differs from the build
  platform. Both `linux/amd64` and `linux/arm64` are built by the CD.
- Only `infra/geo/h3index` imports the library, behind `services.IGeoIndex`, so it
  can be replaced if a mature pure Go option appears.

## Consequences

- Local builds and CI need a C compiler. `Dockerfile.dev` installs `gcc` and
  `musl-dev`.
- Cross compiling arm64 adds about a minute to the image build. Whether to keep
  arm64 at all depends on having arm64 servers (decision 3 of the proposal).
