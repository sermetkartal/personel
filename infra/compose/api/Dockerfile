# =============================================================================
# Personel Admin API — Multi-stage Dockerfile
# =============================================================================

FROM golang:1.22-alpine AS builder

ARG PERSONEL_VERSION=0.1.0
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

WORKDIR /build
COPY ../../apps/api/go.mod ../../apps/api/go.sum ./
RUN go mod download

COPY ../../apps/api/ .
RUN go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${PERSONEL_VERSION}" \
    -o /personel-api \
    ./cmd/api

FROM gcr.io/distroless/base-debian12:nonroot

COPY --from=builder /personel-api /personel-api

USER 65532:65532
WORKDIR /

EXPOSE 8000 9091

ENTRYPOINT ["/personel-api"]
CMD ["serve"]

LABEL org.opencontainers.image.title="personel-api"
LABEL com.personel.service="api"
