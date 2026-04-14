# Multi-stage build for the Personel Ingest Gateway.
# Stage 1: build. Stage 2: minimal runtime image.

# ---- Build stage ----
FROM golang:1.22-bookworm AS builder

WORKDIR /build

# Copy go.mod and go.sum first for layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy generated proto stubs (committed to the repo after buf generate).
COPY pkg/ pkg/

# Copy source.
COPY cmd/ cmd/
COPY internal/ internal/

# Build both binaries.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /gateway ./cmd/gateway && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /enricher ./cmd/enricher

# ---- Runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot

# Metadata.
LABEL org.opencontainers.image.source="https://github.com/personel/personel" \
      org.opencontainers.image.description="Personel Ingest Gateway" \
      org.opencontainers.image.licenses="Proprietary"

# Copy binaries from builder.
COPY --from=builder /gateway /gateway
COPY --from=builder /enricher /enricher

# TLS certificates and config will be bind-mounted at runtime.
# Default config path: /configs/gateway.yaml
# The production compose stack mounts infra/compose/gateway/gateway.yaml
# into /configs — match that path here so this Dockerfile is interchangeable
# with infra/compose/gateway/Dockerfile. Override with:
#   docker run ... -v /host/config:/configs ...

EXPOSE 9443 9090

USER nonroot:nonroot

ENTRYPOINT ["/gateway"]
CMD ["--config", "/configs/gateway.yaml"]
