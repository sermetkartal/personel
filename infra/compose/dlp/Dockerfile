# =============================================================================
# Personel DLP Service — Hardened Container
# Profile 1 (Hardened Container) per dlp-deployment-profiles.md
# Non-negotiable controls: distroless, non-root, read-only, cap-drop all,
# seccomp+AppArmor, no shell, no debugger surface.
# =============================================================================

# Build stage
FROM golang:1.22-alpine AS builder

ARG PERSONEL_VERSION=0.1.0
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

WORKDIR /build

# Only copy go module files first for layer caching
COPY ../../apps/dlp/go.mod ../../apps/dlp/go.sum ./
RUN go mod download

COPY ../../apps/dlp/ .

RUN go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${PERSONEL_VERSION} -extldflags=-static" \
    -o /dlp \
    ./cmd/dlp

# -----------------------------------------------------------------------
# Final stage — distroless nonroot (no shell, no package manager)
# -----------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

# Copy statically-linked binary
COPY --from=builder /dlp /dlp

# Non-root user uid 65532 (distroless nonroot default)
USER 65532:65532

# Read-only root filesystem enforced at container runtime via Compose
# Writable tmpfs mounts: /var/cache/dlp (pattern cache), /tmp
# These are mounted by docker-compose.yaml — NOT in the Dockerfile

# Working directory (tmpfs will overlay this path)
WORKDIR /

# Expose only the metrics port — no inbound RPC surface
EXPOSE 9092

# No shell entrypoint. No /bin/sh. No /bin/bash. No busybox.
ENTRYPOINT ["/dlp"]
CMD ["serve"]

# Health check binary is the same binary with a subcommand
# (distroless has no wget/curl)
# The healthcheck is defined in docker-compose.yaml using CMD form:
#   CMD ["/dlp", "healthcheck"]

# Build labels
LABEL org.opencontainers.image.title="personel-dlp"
LABEL org.opencontainers.image.description="Personel DLP Service — keystroke pattern matching (isolated)"
LABEL com.personel.security.profile="profile-1-hardened-container"
LABEL com.personel.security.distroless="true"
LABEL com.personel.security.nonroot="true"
