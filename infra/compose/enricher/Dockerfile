# =============================================================================
# Personel Event Enricher — ClickHouse writer, OpenSearch indexer, Audit appender
# =============================================================================

FROM golang:1.22-alpine AS builder

ARG PERSONEL_VERSION=0.1.0
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

WORKDIR /build
COPY ../../apps/enricher/go.mod ../../apps/enricher/go.sum ./
RUN go mod download

COPY ../../apps/enricher/ .
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
