# syntax=docker/dockerfile:1.9
# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

ARG VERSION=dev
ARG BUILD_TIME

# Install git for go module fetching (some modules require it).
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# Cache module downloads separately from source — rebuilds reuse this layer
# when only source code changes (not go.mod/go.sum).
COPY go.mod go.sum ./
RUN go mod download

# Copy source.
COPY . .

# Build a fully static binary.
# CGO_ENABLED=0: all dependencies in this module are pure Go.
# -trimpath: strip host filesystem paths from stack traces.
# -ldflags -s -w: strip debug info and symbol table.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
      -trimpath \
      -ldflags "-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" \
      -o /out/personel-api \
      ./cmd/api

# ── Stage 2: Scan (optional — runs govulncheck in CI) ─────────────────────────
FROM golang:1.22-alpine AS vulncheck
RUN go install golang.org/x/vuln/cmd/govulncheck@latest
COPY --from=builder /src /src
WORKDIR /src
RUN govulncheck ./...

# ── Stage 3: Final image ──────────────────────────────────────────────────────
# scratch + copied ca-certificates + tzdata produces the smallest possible image
# with no shell, no package manager, and no attack surface beyond the binary.
FROM scratch AS final

# TLS roots (required for Vault HTTPS and Keycloak OIDC discovery).
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Timezone data (required for nightly cron scheduling in audit verifier).
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy the static binary.
COPY --from=builder /out/personel-api /personel-api

# Default config (overridden by volume mount or PERSONEL_CONFIG_PATH env var).
COPY configs/api.yaml /configs/api.yaml

# Non-root numeric UID (scratch has no /etc/passwd — use numeric directly).
USER 65534:65534

# Expose API and metrics ports.
EXPOSE 8080

# Health check against the liveness probe.
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/personel-api", "-healthcheck"] || exit 1

ENTRYPOINT ["/personel-api"]
