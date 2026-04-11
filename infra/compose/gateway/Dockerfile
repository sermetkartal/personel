# =============================================================================
# Personel Ingest Gateway — Multi-stage Dockerfile
# Copies binary from monorepo build output.
# =============================================================================

# Build stage
FROM golang:1.22-alpine AS builder

ARG PERSONEL_VERSION=0.1.0
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

WORKDIR /build
COPY ../../apps/gateway/go.mod ../../apps/gateway/go.sum ./
RUN go mod download

COPY ../../apps/gateway/ .
RUN go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${PERSONEL_VERSION}" \
    -o /personel-gateway \
    ./cmd/gateway

# Final stage
FROM gcr.io/distroless/base-debian12:nonroot

COPY --from=builder /personel-gateway /personel-gateway

USER 65532:65532
WORKDIR /

# gRPC mTLS port
EXPOSE 9443
# Prometheus metrics
EXPOSE 9090

ENTRYPOINT ["/personel-gateway"]
CMD ["serve"]

LABEL org.opencontainers.image.title="personel-gateway"
LABEL com.personel.service="gateway"
