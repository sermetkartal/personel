# =============================================================================
# Personel UBA Detector — Production Dockerfile
# Python 3.12 slim, non-root, read-only filesystem
# Network: net_ml + data (reads ClickHouse)
# =============================================================================

FROM python:3.12-slim AS builder

WORKDIR /build

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    g++ \
    && rm -rf /var/lib/apt/lists/*

COPY pyproject.toml ./
COPY src/ ./src/

RUN pip install --no-cache-dir --upgrade pip && \
    pip install --no-cache-dir \
        "fastapi>=0.115.0" \
        "uvicorn[standard]>=0.32.0" \
        "pydantic>=2.9.0" \
        "pydantic-settings>=2.6.0" \
        "scikit-learn>=1.5.0" \
        "joblib>=1.4.0" \
        "pandas>=2.2.0" \
        "numpy>=2.1.0" \
        "clickhouse-connect>=0.8.0" \
        "structlog>=24.4.0" \
        "prometheus-client>=0.21.0" \
        "httpx>=0.27.0" \
        "anyio>=4.6.0" \
        "hatchling"

# Install the package itself
RUN pip install --no-cache-dir --no-deps -e .

# =============================================================================
FROM python:3.12-slim AS runtime

# Security: run as non-root user (uid 1001)
RUN groupadd --gid 1001 uba && \
    useradd --uid 1001 --gid uba --shell /bin/false --no-create-home uba

WORKDIR /app

# Copy installed packages from builder
COPY --from=builder /usr/local/lib/python3.12/site-packages /usr/local/lib/python3.12/site-packages
COPY --from=builder /usr/local/bin/uvicorn /usr/local/bin/uvicorn

# Copy application source
COPY --chown=uba:uba src/ ./src/
COPY --chown=uba:uba pyproject.toml ./

# Re-install only the package (no deps, already copied)
RUN pip install --no-cache-dir --no-deps -e .

# Create writable tmp for model serialisation; rest of fs is read-only
RUN mkdir -p /tmp/uba-models && chown uba:uba /tmp/uba-models

USER uba

EXPOSE 8080
EXPOSE 9090

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    UBA_PORT=8080 \
    UBA_METRICS_PORT=9090 \
    UBA_MODEL_DIR=/tmp/uba-models

HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:8080/healthz')" || exit 1

CMD ["uvicorn", "personel_uba.main:app", "--host", "0.0.0.0", "--port", "8080", "--log-config", "/dev/null"]
