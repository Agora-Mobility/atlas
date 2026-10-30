# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.27.3-trixie AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/atlas . && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/bootstrap-admin ./cmd/bootstrap-admin

FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=build /out/atlas /atlas
COPY --from=build /out/migrate /migrate
COPY --from=build /out/healthcheck /healthcheck
COPY --from=build /out/bootstrap-admin /bootstrap-admin

ENV APP_ENV=production \
    HTTP_PORT=8700

# 65532 is the "nonroot" user of distroless images.
USER 65532:65532
EXPOSE 8700

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD ["/healthcheck"]

ENTRYPOINT ["/atlas"]
