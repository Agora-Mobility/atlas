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
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/atlas .

FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=build /out/atlas /atlas

# 65532 is the "nonroot" user of distroless images.
USER 65532:65532
EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD ["/atlas", "healthcheck"]

ENTRYPOINT ["/atlas"]
CMD ["serve"]
