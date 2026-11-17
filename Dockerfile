# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.27.4-trixie AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

# H3 is a C library (infra/geo/h3index), so the API links it statically.
# hadolint ignore=DL3008
RUN if [ "$(dpkg --print-architecture)" != "$TARGETARCH" ]; then \
        apt-get update && \
        case "$TARGETARCH" in \
            arm64) apt-get install -y --no-install-recommends gcc-aarch64-linux-gnu libc6-dev-arm64-cross ;; \
            amd64) apt-get install -y --no-install-recommends gcc-x86-64-linux-gnu libc6-dev-amd64-cross ;; \
        esac; \
    fi

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    case "$TARGETARCH" in \
        arm64) export CC=aarch64-linux-gnu-gcc ;; \
        amd64) export CC=x86_64-linux-gnu-gcc ;; \
    esac && \
    CGO_ENABLED=1 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -tags netgo,osusergo -ldflags="-s -w -X main.version=${VERSION} -linkmode external -extldflags -static" -o /out/atlas . && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/bootstrap-admin ./cmd/bootstrap-admin && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed && \
    CGO_ENABLED=1 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -tags netgo,osusergo -ldflags="-s -w -linkmode external -extldflags -static" -o /out/backfill-h3 ./cmd/backfill-h3

# Alpine em vez de distroless: o arranque é um script de shell (entrypoint.sh) que espera pelo
# Postgres com o pg_isready. O tzdata é preciso para os fusos das zonas (Africa/Luanda).
FROM alpine:3.23

RUN apk add --no-cache ca-certificates=20261020-r0 tzdata=2026f-r0 postgresql18-client=18.6-r0

COPY --from=build /out/atlas /atlas
COPY --from=build /out/migrate /migrate
COPY --from=build /out/healthcheck /healthcheck
COPY --from=build /out/bootstrap-admin /bootstrap-admin
COPY --from=build /out/seed /seed
COPY --from=build /out/backfill-h3 /backfill-h3
COPY --chmod=755 entrypoint.sh /entrypoint.sh

# As migrations correm no entrypoint, antes da API; a API não as repete.
ENV APP_ENV=production \
    HTTP_PORT=8700 \
    DB_MIGRATE_ON_START=false

# Utilizador sem privilégios, o mesmo uid do "nonroot" do distroless que usávamos.
USER 65532:65532
EXPOSE 8700

HEALTHCHECK --interval=15s --timeout=3s --start-period=30s --retries=3 \
    CMD ["/healthcheck"]

ENTRYPOINT ["/entrypoint.sh"]
CMD ["/atlas"]
