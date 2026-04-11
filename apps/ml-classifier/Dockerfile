# =============================================================================
# Personel ML Classifier — Multi-stage Dockerfile
# =============================================================================
# Stage 1: builder
#   - Installs build toolchain (gcc, cmake) needed to compile llama-cpp-python
#   - Compiles all wheels into /install
#
# Stage 2: runtime
#   - python:3.12-slim, no build tools
#   - Copies only installed packages from builder
#   - Runs as non-root uid 65532 (nobody equivalent)
#   - Capabilities: drop ALL
#   - No write access except /tmp and /models (model volume mount)
# =============================================================================

# ---- Stage 1: builder -------------------------------------------------------
FROM python:3.12-slim AS builder

# Suppress interactive prompts during apt-get
ENV DEBIAN_FRONTEND=noninteractive

# Build toolchain for llama-cpp-python (requires gcc + cmake + make)
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    cmake \
    git \
    && rm -rf /var/lib/apt/lists/*

# Create a virtual environment for clean wheel isolation
ENV VIRTUAL_ENV=/opt/venv
RUN python3 -m venv "$VIRTUAL_ENV"
ENV PATH="$VIRTUAL_ENV/bin:$PATH"

# Upgrade pip/wheel/setuptools before installing deps
RUN pip install --no-cache-dir --upgrade pip wheel setuptools

# Copy project metadata only first — layer-cache friendly
COPY pyproject.toml /src/pyproject.toml

# Install runtime dependencies
# CMAKE_ARGS controls llama-cpp-python build flags:
#   -DLLAMA_NATIVE=OFF  — portable binary, no host-specific AVX tuning
#   -DLLAMA_BLAS=OFF    — CPU-only; set to ON + DLLAMA_BLAS_VENDOR=OpenBLAS for perf
# FORCE_CMAKE=1 ensures llama-cpp-python uses its bundled cmake build
ENV CMAKE_ARGS="-DLLAMA_NATIVE=OFF -DLLAMA_BLAS=OFF"
ENV FORCE_CMAKE=1

# Install all runtime deps from pyproject.toml
RUN pip install --no-cache-dir /src/

# ---- Stage 2: runtime -------------------------------------------------------
FROM python:3.12-slim AS runtime

LABEL org.opencontainers.image.title="personel-ml-classifier"
LABEL org.opencontainers.image.description="Local LLM category classifier — Personel Platform Phase 2.3"
LABEL org.opencontainers.image.vendor="Personel"
LABEL com.personel.adr="0017"
LABEL com.personel.component="ml-classifier"

ENV DEBIAN_FRONTEND=noninteractive

# Minimal runtime packages: wget for healthcheck, ca-certificates for TLS
RUN apt-get update && apt-get install -y --no-install-recommends \
    wget \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Create a non-root user/group (uid=65532, matches distroless nonroot convention)
RUN groupadd --gid 65532 nonroot && \
    useradd --uid 65532 --gid 65532 --no-create-home --shell /sbin/nologin nonroot

# Copy installed virtual environment from builder
ENV VIRTUAL_ENV=/opt/venv
COPY --from=builder "$VIRTUAL_ENV" "$VIRTUAL_ENV"
ENV PATH="$VIRTUAL_ENV/bin:$PATH"

# Copy application source
WORKDIR /app
COPY src/ /app/src/
COPY prompts/ /app/prompts/
COPY docker-entrypoint.sh /app/docker-entrypoint.sh

# Model volume mount point — populated by docker-entrypoint.sh or pre-staged
# by install.sh from the signed tar.zst bundle (ADR 0017)
RUN mkdir -p /models && chown 65532:65532 /models

# Allow writes only to /tmp (read-only root except /tmp and /models)
RUN chmod +x /app/docker-entrypoint.sh && \
    chown -R 65532:65532 /app

USER 65532:65532

# Service listens on 8080 (configured via PERSONEL_ML_BIND_PORT)
EXPOSE 8080

# Healthcheck polls /readyz; starts after 30s, checks every 15s
# Returns 200 for both 'ready' and 'degraded' — the service is always up
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 \
    CMD wget -qO- http://localhost:8080/readyz | grep -q '"status"' || exit 1

# Default environment (overridden by docker-compose.yaml)
ENV PERSONEL_ML_BIND_HOST="0.0.0.0"
ENV PERSONEL_ML_BIND_PORT="8080"
ENV PERSONEL_ML_MODEL_PATH="/models/llama-3.2-3b.Q4_K_M.gguf"
ENV PERSONEL_ML_N_THREADS="0"
ENV PERSONEL_ML_N_CTX="2048"
ENV PERSONEL_ML_LOG_LEVEL="info"
ENV PERSONEL_ML_CONFIDENCE_THRESHOLD="0.70"

# Python path so the src layout is importable without editable install
ENV PYTHONPATH="/app/src"

ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["python", "-m", "personel_ml.main"]
