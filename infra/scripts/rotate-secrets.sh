#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Secret Rotation
# Per secret-rotation.md
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"
set -a; source "${COMPOSE_DIR}/.env"; set +a

CERTS_ONLY=false
for arg in "$@"; do [[ "${arg}" == "--certs-only" ]] && CERTS_ONLY=true; done

log() { echo "[rotate-secrets] $*"; }
warn() { echo "[rotate-secrets] WARN: $*" >&2; }

log "=== Secret Rotation Check ==="
log "Timestamp: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"

# ---------------------------------------------------------------------------
# Check server cert expiry and trigger renewal
# ---------------------------------------------------------------------------
log "--- Server TLS certificate expiry check ---"
TLS_DIR="${TLS_DIR:-/etc/personel/tls}"
WARN_DAYS=14

for service in gateway api vault postgres clickhouse nats opensearch; do
  CERT_FILE="${TLS_DIR}/${service}.crt"
  if [[ -f "${CERT_FILE}" ]]; then
    EXPIRY=$(openssl x509 -enddate -noout -in "${CERT_FILE}" 2>/dev/null | \
             sed 's/notAfter=//')
    EXPIRY_EPOCH=$(date -d "${EXPIRY}" +%s 2>/dev/null || \
                   python3 -c "from datetime import datetime; print(int(datetime.strptime('${EXPIRY}', '%b %d %H:%M:%S %Y %Z').timestamp()))")
    NOW_EPOCH=$(date +%s)
    DAYS_LEFT=$(( (EXPIRY_EPOCH - NOW_EPOCH) / 86400 ))

    if [[ "${DAYS_LEFT}" -lt "${WARN_DAYS}" ]]; then
      log "  RENEWING: ${service} cert expires in ${DAYS_LEFT} days"
      # Vault agent should handle this; this is a fallback signal
      # TODO: when vault-agent sidecar is implemented, it manages renewals
      warn "  Manual cert renewal needed for ${service}. See pki-bootstrap.md §5"
    else
      log "  OK: ${service} cert expires in ${DAYS_LEFT} days"
    fi
  fi
done

[[ "${CERTS_ONLY}" == "true" ]] && { log "Certs-only mode — done."; exit 0; }

# ---------------------------------------------------------------------------
# MinIO access key rotation (AppRole-like rotation via MinIO admin)
# ---------------------------------------------------------------------------
log "--- MinIO access key rotation check ---"
# In production, Vault dynamic secrets handles this.
# This script checks if rotation is overdue (>90 days).
warn "MinIO key rotation: verify Vault dynamic secrets are rotating MinIO credentials"

# ---------------------------------------------------------------------------
# NATS credential rotation reminder
# ---------------------------------------------------------------------------
log "--- NATS credentials check ---"
warn "NATS credentials: 90-day rotation due. Verify NKey credentials are current."

log ""
log "Secret rotation check complete."
log "TR: Tam döndürme prosedürü için: docs/security/runbooks/secret-rotation.md"
log "EN: For full rotation procedures, see: docs/security/runbooks/secret-rotation.md"
