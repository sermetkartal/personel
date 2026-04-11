#!/bin/sh
# =============================================================================
# docker-entrypoint.sh — Personel ML Classifier
# =============================================================================
# On first container start, downloads the GGUF model file if the /models
# volume is empty.  On subsequent starts the file is already present.
#
# Model source: Hugging Face Hub (TheBloke/Llama-3.2-3B-Instruct-GGUF or
#               bartowski/Llama-3.2-3B-Instruct-GGUF, ADR 0017).
#
# Security:
#   - SHA-256 checksum is verified before the file is moved into place.
#   - Download happens ONLY when the model file is absent (first-run UX).
#   - If PERSONEL_ML_SKIP_MODEL_DOWNLOAD=true, the entrypoint skips the
#     download entirely (used when model is pre-staged via install.sh or
#     a signed tar.zst bundle dropped into the volume at install time).
#   - If download fails, the service still starts in fallback mode.
#
# TODO (devops-engineer): wire in a customer-specific download URL and
# expected SHA-256 from Vault KV once model signing is implemented.
# =============================================================================

set -e

MODEL_PATH="${PERSONEL_ML_MODEL_PATH:-/models/llama-3.2-3b.Q4_K_M.gguf}"
MODEL_DIR="$(dirname "$MODEL_PATH")"
MODEL_FILENAME="$(basename "$MODEL_PATH")"

# Default download URL — GGUF q4_k_m quantization.
# Override with PERSONEL_ML_MODEL_URL for air-gapped deployments pointing to
# an internal artifact repository.
MODEL_URL="${PERSONEL_ML_MODEL_URL:-https://huggingface.co/bartowski/Llama-3.2-3B-Instruct-GGUF/resolve/main/Llama-3.2-3B-Instruct-Q4_K_M.gguf}"

# Expected SHA-256 for the model file.
# TODO (devops-engineer): pin this after Phase 2 model certification.
# Leave empty to skip checksum verification during development.
MODEL_SHA256="${PERSONEL_ML_MODEL_SHA256:-}"

SKIP_DOWNLOAD="${PERSONEL_ML_SKIP_MODEL_DOWNLOAD:-false}"

log() {
    echo "[entrypoint] $*" >&2
}

# ---------------------------------------------------------------------------
# Check if the model file already exists
# ---------------------------------------------------------------------------

if [ -f "$MODEL_PATH" ]; then
    log "Model file already present at $MODEL_PATH — skipping download."
    exec "$@"
    exit 0
fi

# ---------------------------------------------------------------------------
# Skip download if explicitly disabled
# ---------------------------------------------------------------------------

if [ "$SKIP_DOWNLOAD" = "true" ]; then
    log "PERSONEL_ML_SKIP_MODEL_DOWNLOAD=true — skipping download."
    log "WARNING: No model file found at $MODEL_PATH."
    log "Service will start in fallback (rule-based) mode."
    exec "$@"
    exit 0
fi

# ---------------------------------------------------------------------------
# Download the model
# ---------------------------------------------------------------------------

log "No model file at $MODEL_PATH."
log "Downloading Llama-3.2-3B-Instruct GGUF q4_k_m from: $MODEL_URL"
log "This is a ~2 GB download. Set PERSONEL_ML_SKIP_MODEL_DOWNLOAD=true to skip."

mkdir -p "$MODEL_DIR"
TEMP_FILE="$MODEL_DIR/.downloading_${MODEL_FILENAME}"

if command -v wget >/dev/null 2>&1; then
    if ! wget --no-verbose --show-progress -O "$TEMP_FILE" "$MODEL_URL"; then
        log "ERROR: wget download failed. Removing partial file."
        rm -f "$TEMP_FILE"
        log "Starting service in fallback mode."
        exec "$@"
        exit 0
    fi
elif command -v curl >/dev/null 2>&1; then
    if ! curl --location --progress-bar --fail -o "$TEMP_FILE" "$MODEL_URL"; then
        log "ERROR: curl download failed. Removing partial file."
        rm -f "$TEMP_FILE"
        log "Starting service in fallback mode."
        exec "$@"
        exit 0
    fi
else
    log "ERROR: Neither wget nor curl found. Cannot download model."
    log "Pre-stage the model file at $MODEL_PATH and restart the container."
    exec "$@"
    exit 0
fi

# ---------------------------------------------------------------------------
# SHA-256 verification (if checksum is provided)
# ---------------------------------------------------------------------------

if [ -n "$MODEL_SHA256" ]; then
    log "Verifying SHA-256 checksum..."
    ACTUAL_SHA=$(sha256sum "$TEMP_FILE" | awk '{print $1}')
    if [ "$ACTUAL_SHA" != "$MODEL_SHA256" ]; then
        log "ERROR: SHA-256 mismatch!"
        log "  Expected: $MODEL_SHA256"
        log "  Actual:   $ACTUAL_SHA"
        log "Removing corrupted download. Starting in fallback mode."
        rm -f "$TEMP_FILE"
        exec "$@"
        exit 0
    fi
    log "SHA-256 verified OK."
else
    log "WARNING: PERSONEL_ML_MODEL_SHA256 not set — skipping checksum verification."
    log "TODO: set this before Phase 2 production deployment."
fi

# ---------------------------------------------------------------------------
# Atomically move the downloaded file into place
# ---------------------------------------------------------------------------

mv "$TEMP_FILE" "$MODEL_PATH"
log "Model downloaded and staged at $MODEL_PATH."

exec "$@"
