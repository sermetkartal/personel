#!/bin/sh
# =============================================================================
# docker-entrypoint.sh — Personel OCR Service
# =============================================================================
# Responsibilities:
#   1. Verify Tesseract binary is present and language packs are installed.
#   2. Optionally install additional Tesseract language data packs at runtime
#      (useful for air-gapped deployments where packs are volume-mounted).
#   3. Exec the application process.
#
# Environment variables:
#   TESSERACT_EXTRA_LANGS   comma-separated extra lang codes to check at startup
#                           e.g. "deu,fra"  (installs via apt if missing)
#   PERSONEL_OCR_TESSDATA_PREFIX  override tessdata directory (default: system)
#
# Security:
#   - Runs as uid 65532 (nonroot); no sudo available.
#   - Language pack installation via apt is only possible during the builder
#     stage.  Runtime apt installs are intentionally disabled (nonroot + RO FS).
#   - This script only validates and logs; it never writes to sensitive paths.
# =============================================================================

set -e

log() {
    echo "[ocr-entrypoint] $*" >&2
}

# ---------------------------------------------------------------------------
# 1. Verify Tesseract binary
# ---------------------------------------------------------------------------

if ! command -v tesseract >/dev/null 2>&1; then
    log "WARNING: tesseract binary not found in PATH."
    log "Service will start in degraded mode — OCR extractions will return 503."
    exec "$@"
    exit 0
fi

TESS_VERSION=$(tesseract --version 2>&1 | head -1)
log "Tesseract found: $TESS_VERSION"

# ---------------------------------------------------------------------------
# 2. Verify required language packs (tur + eng)
# ---------------------------------------------------------------------------

check_lang() {
    lang="$1"
    # tesseract --list-langs exits 0 and prints available langs to stderr
    if tesseract --list-langs 2>&1 | grep -qw "$lang"; then
        log "Language pack OK: $lang"
    else
        log "WARNING: Tesseract language pack '$lang' not found."
        log "Install with: apt-get install tesseract-ocr-${lang}"
        log "OCR quality for this language will be degraded or unavailable."
    fi
}

check_lang "tur"
check_lang "eng"

# Check optional extra languages requested via env
if [ -n "${TESSERACT_EXTRA_LANGS:-}" ]; then
    for lang in $(echo "$TESSERACT_EXTRA_LANGS" | tr ',' ' '); do
        check_lang "$lang"
    done
fi

# ---------------------------------------------------------------------------
# 3. Set tessdata prefix if overridden
# ---------------------------------------------------------------------------

if [ -n "${PERSONEL_OCR_TESSDATA_PREFIX:-}" ]; then
    export TESSDATA_PREFIX="$PERSONEL_OCR_TESSDATA_PREFIX"
    log "TESSDATA_PREFIX set to: $TESSDATA_PREFIX"
fi

log "OCR service entrypoint complete — starting application."

exec "$@"
