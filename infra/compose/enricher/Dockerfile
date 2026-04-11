# =============================================================================
# Personel Event Enricher — ClickHouse writer, OpenSearch indexer, Audit appender
#
# Note: the enricher is a sibling binary inside the gateway module
# (apps/gateway/cmd/enricher), not a standalone module. Build context
# must be the repository root so we can COPY apps/gateway/...
# =============================================================================

FROM golang:1.25-alpine AS builder

ARG PERSONEL_VERSION=0.1.0
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

WORKDIR /build
# Same caveat as gateway/Dockerfile: local replace directive requires
# the full tree before go mod download.
COPY apps/gateway/ .
RUN go mod download
RUN go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${PERSONEL_VERSION}" \
    -o /personel-enricher \
    ./cmd/enricher

FROM gcr.io/distroless/base-debian12:nonroot

COPY --from=builder /personel-enricher /personel-enricher

USER 65532:65532
WORKDIR /

EXPOSE 9093

ENTRYPOINT ["/personel-enricher"]
CMD ["serve"]

LABEL org.opencontainers.image.title="personel-enricher"
LABEL com.personel.service="enricher"
