#!/usr/bin/env bash
# =============================================================================
# Personel Platform — KVKK Incident Forensic Bundle Export
# Per incident-response-playbook.md §8
# TR: KVKK ihlal bildirimi için adli inceleme paketi hazırlar.
# EN: Prepares forensic bundle for KVKK data breach notification.
#
# Usage:
#   ./export-forensic-bundle.sh --incident-id PER-INC-20260410-1
#   ./export-forensic-bundle.sh --incident-id ID --since 2026-04-01 --until 2026-04-10
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"
set -a; source "${COMPOSE_DIR}/.env"; set +a

INCIDENT_ID=""
SINCE="$(date -d '7 days ago' +%Y-%m-%d 2>/dev/null || date -v-7d +%Y-%m-%d)"
UNTIL="$(date +%Y-%m-%d)"

for arg in "$@"; do
  case "${arg}" in
    --incident-id=*) INCIDENT_ID="${arg#--incident-id=}" ;;
    --since=*)       SINCE="${arg#--since=}" ;;
    --until=*)       UNTIL="${arg#--until=}" ;;
  esac
done

[[ -n "${INCIDENT_ID}" ]] || { echo "Usage: $0 --incident-id ID [--since DATE] [--until DATE]"; exit 1; }

BUNDLE_DIR="/var/lib/personel/forensic-bundles/${INCIDENT_ID}-$(date +%Y%m%dT%H%M%S)"
mkdir -p "${BUNDLE_DIR}"

log() { echo "[forensic] $*" | tee -a "${BUNDLE_DIR}/export.log"; }

log "=== KVKK Forensic Bundle Export ==="
log "Incident: ${INCIDENT_ID}"
log "Period:   ${SINCE} to ${UNTIL}"
log ""

# Export audit log entries for the incident period
log "Exporting audit log entries..."
docker exec personel-postgres psql -U postgres -d personel \
  --csv -t -c "
SELECT
  id, tenant_id, event_type, actor_type, actor_id,
  entity_type, entity_id, payload, signed_at, seq, row_hash, prev_hash
FROM audit.audit_events
WHERE signed_at >= '${SINCE}'::timestamptz
  AND signed_at <  '${UNTIL}'::timestamptz + INTERVAL '1 day'
ORDER BY seq;" > "${BUNDLE_DIR}/audit-events.csv" 2>/dev/null

log "Exporting live view sessions..."
docker exec personel-postgres psql -U postgres -d personel \
  --csv -t -c "
SELECT id, endpoint_id, requested_by, approved_by, state, reason, started_at, ended_at
FROM core.live_view_requests
WHERE created_at >= '${SINCE}'::timestamptz
  AND created_at <  '${UNTIL}'::timestamptz + INTERVAL '1 day'
ORDER BY created_at;" > "${BUNDLE_DIR}/live-view-sessions.csv" 2>/dev/null

log "Exporting DSR requests..."
docker exec personel-postgres psql -U postgres -d personel \
  --csv -t -c "
SELECT id, employee_email, request_type, state, created_at, sla_deadline, updated_at
FROM core.dsr_requests
WHERE created_at >= '${SINCE}'::timestamptz
ORDER BY created_at;" > "${BUNDLE_DIR}/dsr-requests.csv" 2>/dev/null

# Collect Vault audit
log "Exporting Vault audit log excerpt..."
docker exec personel-vault sh -c \
  "grep '${SINCE}' /vault/data/audit.log 2>/dev/null || true" \
  > "${BUNDLE_DIR}/vault-audit.json" 2>/dev/null || true

# System info
log "Recording system state..."
{
  echo "=== System Info at $(date -u) ==="
  docker compose ps 2>/dev/null
  echo ""
  echo "=== Docker Images ==="
  docker images "personel/*" 2>/dev/null
} > "${BUNDLE_DIR}/system-state.txt"

# Create manifest
python3 -c "
import json, os, hashlib
bundle_dir = '${BUNDLE_DIR}'
files = {}
for fname in os.listdir(bundle_dir):
    fpath = os.path.join(bundle_dir, fname)
    if os.path.isfile(fpath):
        h = hashlib.sha256()
        with open(fpath, 'rb') as f:
            for chunk in iter(lambda: f.read(65536), b''):
                h.update(chunk)
        files[fname] = {'sha256': h.hexdigest(), 'size_bytes': os.path.getsize(fpath)}
manifest = {
  'incident_id': '${INCIDENT_ID}',
  'tenant_id': '${PERSONEL_TENANT_ID:-unknown}',
  'period_start': '${SINCE}',
  'period_end': '${UNTIL}',
  'exported_at': '$(date -u +"%Y-%m-%dT%H:%M:%SZ")',
  'exported_by': '${USER:-system}',
  'files': files
}
print(json.dumps(manifest, indent=2))
" > "${BUNDLE_DIR}/MANIFEST.json"

# Tar the bundle
BUNDLE_TAR="${BUNDLE_DIR}.tar.gz"
tar -czf "${BUNDLE_TAR}" -C "$(dirname "${BUNDLE_DIR}")" "$(basename "${BUNDLE_DIR}")"
rm -rf "${BUNDLE_DIR}"

log ""
log "Forensic bundle ready: ${BUNDLE_TAR}"
log "SHA-256: $(sha256sum "${BUNDLE_TAR}" | cut -d' ' -f1)"
log ""
log "TR: Bu paket KVKK Kurul bildirimi için teknik kanıt olarak kullanılabilir."
log "EN: This bundle can be used as technical evidence for KVKK Kurul notification."
log "    Per incident-response-playbook.md §8.2 — notify customer DPO within 24h."
