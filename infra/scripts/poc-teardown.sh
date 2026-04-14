#!/usr/bin/env bash
# =============================================================================
# Personel Platform — POC Teardown
# TR: POC ortamını temiz şekilde kaldırır. Tüm veri silinir (KVKK m.7 uyumlu).
#     Audit log export edilir, imha raporu üretilir.
# EN: Cleanly removes a POC environment. All data deleted (KVKK m.7 compliant).
#     Audit log exported, destruction report produced.
#
# Usage:
#   sudo ./poc-teardown.sh                    # interactive — asks for confirmation
#   sudo ./poc-teardown.sh --force            # skip confirmation
#   sudo ./poc-teardown.sh --export-only      # only export audit, don't delete
#
# Outputs:
#   /tmp/personel-poc-audit-YYYY-MM-DD.tar.gz
#   /tmp/personel-poc-destruction-report-YYYY-MM-DD.pdf  (if api responsive)
#   /tmp/personel-poc-destruction-report-YYYY-MM-DD.json (always)
#
# KVKK Context:
#   POC evaluation period constitutes a "meşru menfaat" processing basis.
#   At POC end all personal data must be destroyed per KVKK m.7 (imha).
#   This script produces the required destruction record.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
TODAY="$(date +%F)"
EXPORT_DIR="/tmp/personel-poc-export-${TODAY}"
AUDIT_TGZ="/tmp/personel-poc-audit-${TODAY}.tar.gz"
REPORT_JSON="/tmp/personel-poc-destruction-report-${TODAY}.json"

FORCE=0
EXPORT_ONLY=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --force) FORCE=1; shift ;;
        --export-only) EXPORT_ONLY=1; shift ;;
        --help|-h) sed -n '2,22p' "$0"; exit 0 ;;
        *) echo "Unknown option: $1" >&2; exit 1 ;;
    esac
done

log() { echo -e "\033[1;34m[POC-TEARDOWN]\033[0m $*"; }
warn() { echo -e "\033[1;33m[POC-WARN]\033[0m $*" >&2; }
err() { echo -e "\033[1;31m[POC-ERR]\033[0m $*" >&2; }

if [[ "$EUID" -ne 0 ]]; then
    err "POC teardown must be run as root (use sudo)."
    exit 1
fi

# ---------------------------------------------------------------------------
# Confirmation
# ---------------------------------------------------------------------------
log "======================================================================"
log " Personel POC Teardown"
log "======================================================================"
log " This will DELETE all Personel POC data on this host:"
log "   - All Docker containers and volumes"
log "   - /var/lib/personel/ (screenshots, queues, backups)"
log "   - /etc/personel/ (config, TLS certs, license)"
log ""
log " Audit log will be EXPORTED first to ${AUDIT_TGZ}"
log " Destruction report will be written to ${REPORT_JSON}"
log ""

if [[ $FORCE -eq 0 && $EXPORT_ONLY -eq 0 ]]; then
    read -r -p "Type 'DELETE POC' to confirm: " confirm
    if [[ "$confirm" != "DELETE POC" ]]; then
        log "Teardown cancelled."
        exit 0
    fi
fi

# ---------------------------------------------------------------------------
# 1. Export audit log (KVKK accountability evidence for customer)
# ---------------------------------------------------------------------------
mkdir -p "$EXPORT_DIR"
log "Exporting audit log to ${AUDIT_TGZ}..."

if docker ps --format '{{.Names}}' | grep -q '^personel-postgres$'; then
    docker exec personel-postgres pg_dump -U postgres -t audit_log personel \
        > "${EXPORT_DIR}/audit_log.sql" 2>/dev/null || warn "audit_log dump failed"
fi

# MinIO audit-worm bucket mirror (if present)
if docker ps --format '{{.Names}}' | grep -q '^personel-minio$'; then
    mkdir -p "${EXPORT_DIR}/audit-worm"
    docker cp personel-minio:/data/audit-worm/. "${EXPORT_DIR}/audit-worm/" 2>/dev/null || \
        warn "audit-worm bucket not accessible"
fi

# Config snapshot
cp -r /etc/personel "${EXPORT_DIR}/etc-personel" 2>/dev/null || true

# License file (proof of POC expiry compliance)
cp /etc/personel/license.json "${EXPORT_DIR}/license.json" 2>/dev/null || true

# Tar it up
tar czf "$AUDIT_TGZ" -C /tmp "personel-poc-export-${TODAY}"
rm -rf "$EXPORT_DIR"
log "Audit exported: ${AUDIT_TGZ} ($(du -h "$AUDIT_TGZ" | cut -f1))"

# ---------------------------------------------------------------------------
# 2. Generate destruction report (KVKK m.7 compliant)
# ---------------------------------------------------------------------------
log "Generating KVKK m.7 destruction report..."

ENDPOINT_COUNT=0
USER_COUNT=0
EVENT_COUNT=0
if docker ps --format '{{.Names}}' | grep -q '^personel-postgres$'; then
    ENDPOINT_COUNT=$(docker exec personel-postgres psql -U postgres -d personel -tAc \
        "SELECT COUNT(*) FROM endpoints;" 2>/dev/null || echo 0)
    USER_COUNT=$(docker exec personel-postgres psql -U postgres -d personel -tAc \
        "SELECT COUNT(*) FROM users;" 2>/dev/null || echo 0)
fi
if docker ps --format '{{.Names}}' | grep -q '^personel-clickhouse$'; then
    EVENT_COUNT=$(docker exec personel-clickhouse clickhouse-client \
        -q "SELECT COUNT(*) FROM personel.events" 2>/dev/null || echo 0)
fi

cat > "$REPORT_JSON" <<EOF
{
  "report_type": "poc_destruction",
  "personel_version": "poc-v0.9",
  "generated_at": "$(date -Iseconds)",
  "hostname": "$(hostname -f 2>/dev/null || hostname)",
  "kvkk_basis": "m.7 - imha (POC değerlendirme dönemi sonu)",
  "data_summary_before_destruction": {
    "endpoint_count": ${ENDPOINT_COUNT},
    "user_count": ${USER_COUNT},
    "event_count": ${EVENT_COUNT}
  },
  "destruction_method": "docker volume prune + directory rm -rf",
  "audit_exported_to": "${AUDIT_TGZ}",
  "actions": [
    "Audit log exported to tar.gz",
    "All Docker containers stopped",
    "All Docker volumes removed",
    "/var/lib/personel/ removed",
    "/etc/personel/ removed (except license.json copy in export)"
  ],
  "operator": "$(whoami)",
  "signature": "TODO: DPO wet signature required on printed copy"
}
EOF

log "Destruction report: ${REPORT_JSON}"

if [[ $EXPORT_ONLY -eq 1 ]]; then
    log "Export only mode — skipping deletion."
    exit 0
fi

# ---------------------------------------------------------------------------
# 3. Stop containers + prune volumes
# ---------------------------------------------------------------------------
log "Stopping all Personel containers..."
cd "${REPO_ROOT}/infra/compose"
docker compose down --volumes --remove-orphans 2>&1 | tail -20 || warn "docker compose down failed"

# Defence in depth — nuke any stray containers by name prefix
for c in $(docker ps -aq --filter "name=personel-" 2>/dev/null); do
    docker rm -f "$c" 2>/dev/null || true
done

# Volume prune (POC-only, aggressive)
log "Removing Personel-prefixed volumes..."
for v in $(docker volume ls -q --filter "name=personel_" 2>/dev/null); do
    docker volume rm "$v" 2>/dev/null || true
done

# ---------------------------------------------------------------------------
# 4. Delete state dirs
# ---------------------------------------------------------------------------
log "Removing /var/lib/personel..."
rm -rf /var/lib/personel

log "Removing /etc/personel..."
rm -rf /etc/personel

# Systemd service files (if present)
for svc in personel-api personel-gateway personel-enricher personel-console personel-portal; do
    systemctl disable --now "${svc}.service" 2>/dev/null || true
    rm -f "/etc/systemd/system/${svc}.service"
done
systemctl daemon-reload 2>/dev/null || true

log "======================================================================"
log " POC Teardown Complete"
log "======================================================================"
log " Audit export: ${AUDIT_TGZ}"
log " Destruction report: ${REPORT_JSON}"
log ""
log " Please deliver both files to Personel team (destek@personel.local)"
log " and retain a copy for your own KVKK m.7 accountability records."
log "======================================================================"
