# =============================================================================
# Personel OCR Service — Multi-stage Dockerfile
# =============================================================================
# Stage 1: builder
#   - Installs build toolchain needed for pip wheel compilation
#   - Compiles all Python wheels into /opt/venv
#
# Stage 2: runtime
#   - python:3.12-slim base
#   - apt installs: tesseract-ocr, tesseract-ocr-tur, tesseract-ocr-eng
#   - Runs as non-root uid 65532 (distroless nonroot convention)
#   - Capabilities: drop ALL
#   - Read-only root FS except /tmp
#   - Healthcheck: GET /readyz
# =============================================================================

# ---- Stage 1: builder -------------------------------------------------------
FROM python:3.12-slim AS builder

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    git \
    && rm -rf /var/lib/apt/lists/*

ENV VIRTUAL_ENV=/opt/venv
RUN python3 -m venv "$VIRTUAL_ENV"
ENV PATH="$VIRTUAL_ENV/bin:$PATH"

RUN pip install --no-cache-dir --upgrade pip wheel setuptools

COPY pyproject.toml /src/pyproject.toml

# paddleocr is optional and large; install base deps without it first.
# Set PERSONEL_OCR_PADDLE_ENABLED=1 in the build arg to include it.
ARG INCLUDE_PADDLE=0

RUN if [ "$INCLUDE_PADDLE" = "1" ]; then \
        pip install --no-cache-dir /src/[paddle]; \
    else \
        pip install --no-cache-dir /src/ --no-deps && \
        pip install --no-cache-dir \
            "fastapi>=0.115,<0.116" \
            "uvicorn[standard]>=0.32,<0.34" \
            "pydantic>=2.9,<3.0" \
            "pydantic-settings>=2.6,<3.0" \
            "pillow>=11,<12" \
            "pytesseract>=0.3.10,<0.4.0" \
            "numpy>=2,<3" \
            "python-multipart>=0.0.9,<0.0.20" \
            "prometheus-client>=0.21,<0.22" \
            "structlog>=24.0,<25.0"; \
    fi

# ---- Stage 2: runtime -------------------------------------------------------
FROM python:3.12-slim AS runtime

LABEL org.opencontainers.image.title="personel-ocr-service"
LABEL org.opencontainers.image.description="OCR text extraction service — Personel Platform Phase 2.8"
LABEL org.opencontainers.image.vendor="Personel"
LABEL com.personel.component="ocr-service"
LABEL com.personel.kvkk.module="ocr"
LABEL com.personel.kvkk.default-state="disabled"

ENV DEBIAN_FRONTEND=noninteractive

# Tesseract 5.x + Turkish and English language packs
RUN apt-get update && apt-get install -y --no-install-recommends \
    tesseract-ocr \
    tesseract-ocr-tur \
    tesseract-ocr-eng \
    wget \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Non-root user (uid=65532, matches distroless nonroot convention)
RUN groupadd --gid 65532 nonroot && \
    useradd --uid 65532 --gid 65532 --no-create-home --shell /sbin/nologin nonroot

# Copy virtual environment from builder
ENV VIRTUAL_ENV=/opt/venv
COPY --from=builder "$VIRTUAL_ENV" "$VIRTUAL_ENV"
ENV PATH="$VIRTUAL_ENV/bin:$PATH"

WORKDIR /app
COPY src/ /app/src/
COPY docker-entrypoint.sh /app/docker-entrypoint.sh

RUN chmod +x /app/docker-entrypoint.sh && \
    chown -R 65532:65532 /app

USER 65532:65532

# Service listens on 8080
EXPOSE 8080

# Healthcheck polls /readyz; starts after 20s, checks every 15s
# Both 'ready' and 'degraded' are valid responses for a live container.
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -qO- http://localhost:8080/readyz | grep -q '"status"' || exit 1

# Default environment (overridden by docker-compose.yaml)
ENV PERSONEL_OCR_BIND_HOST="0.0.0.0"
ENV PERSONEL_OCR_BIND_PORT="8080"
ENV PERSONEL_OCR_DEFAULT_ENGINE="tesseract"
ENV PERSONEL_OCR_DEFAULT_LANGUAGES="tr,en"
ENV PERSONEL_OCR_LOG_LEVEL="info"
ENV PERSONEL_OCR_CONFIDENCE_THRESHOLD="0.30"
ENV PERSONEL_OCR_BATCH_MAX_ITEMS="16"

ENV PYTHONPATH="/app/src"

ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["python", "-m", "personel_ocr.main"]
