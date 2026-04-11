# =============================================================================
# livrec-service — multi-stage Docker build
# Per ADR 0019: service runs under a non-root user; distroless final image.
# =============================================================================

# Stage 1: Build
FROM golang:1.22-alpine AS builder

WORKDIR /src

# Copy go module files first for layer caching.
COPY go.mod ./
RUN go mod download

# Copy source.
COPY . .

# Build the binary.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o /bin/livrec ./cmd/livrec/...

# Stage 2: Minimal runtime image.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /bin/livrec /livrec

# Expose HTTP port.
EXPOSE 8080

# Healthcheck (requires wget or curl; use a TCP check instead for distroless).
# Healthcheck is defined in docker-compose.yaml via /readyz.

USER nonroot:nonroot

ENTRYPOINT ["/livrec"]
