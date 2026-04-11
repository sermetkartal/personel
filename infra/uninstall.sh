#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Uninstall Script
# TR: Personel platformunu kaldırır. Yedekler ve denetim logları korunur.
# EN: Removes Personel platform. Backups and audit logs are preserved by default.
#
# Usage:
#   sudo ./uninstall.sh           — remove containers and stack (keep data)
#   sudo ./uninstall.sh --purge   — DESTROY ALL DATA (irreversible!)
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/compose"
DATA_DIR="/var/lib/personel"
BACKUP_DIR="${DATA_DIR}/backups"
AUDIT_DIR="/var/log/personel/audit"

PURGE=false
for arg in "$@"; do [[ "${arg}" == "--purge" ]] && PURGE=true; done

RED='\033[0;31m'; YELLOW='\033[1;33m'; NC='\033[0m'; BOLD='\033[1m'

echo ""
echo -e "${YELLOW}${BOLD}=== Personel Platform Uninstaller ===${NC}"
echo ""

if [[ "${PURGE}" == "true" ]]; then
  echo -e "${RED}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
  echo -e "${RED}${BOLD}║  --purge FLAG SET: ALL DATA WILL BE PERMANENTLY DELETED     ║${NC}"
  echo -e "${RED}${BOLD}║  TR: Tüm veriler kalıcı olarak silinecek. Geri dönüş yok.   ║${NC}"
  echo -e "${RED}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}"
  echo ""
  read -r -p "Type PURGE-ALL-DATA to confirm: " CONFIRM
  [[ "${CONFIRM}" == "PURGE-ALL-DATA" ]] || { echo "Aborted."; exit 0; }
  read -r -p "Are you absolutely sure? Type YES: " CONFIRM2
  [[ "${CONFIRM2}" == "YES" ]] || { echo "Aborted."; exit 0; }
else
  read -r -p "Stop and remove Personel containers? [y/N]: " CONFIRM
  [[ "${CONFIRM}" =~ ^[Yy]$ ]] || { echo "Aborted."; exit 0; }
fi

# Stop systemd units
echo "Stopping systemd units..."
systemctl stop personel.target 2>/dev/null || true
systemctl disable personel.target personel-compose.service \
  personel-backup.timer personel-audit-verifier.timer personel-cert-renewer.timer \
  2>/dev/null || true

# Remove systemd unit files
for unit in personel.target personel-compose.service \
  personel-backup.timer personel-backup.service \
  personel-audit-verifier.timer personel-audit-verifier.service \
  personel-cert-renewer.timer personel-cert-renewer.service; do
  rm -f "/etc/systemd/system/${unit}"
done
systemctl daemon-reload

# Stop and remove containers
if [[ -f "${COMPOSE_DIR}/.env" ]]; then
  cd "${COMPOSE_DIR}"
  docker compose down --remove-orphans --volumes 2>/dev/null || true
fi

# Remove container images (optional)
docker images "personel/*" -q | xargs -r docker rmi -f 2>/dev/null || true

echo ""
if [[ "${PURGE}" == "true" ]]; then
  echo "Purging all data..."
  rm -rf "${DATA_DIR}"
  rm -rf /var/log/personel
  rm -rf /etc/personel
  userdel personel 2>/dev/null || true
  rm -rf /etc/apparmor.d/personel-dlp
  rm -f /etc/sysctl.d/99-personel-dlp.conf
  sysctl --system --quiet
  echo "All data purged."
else
  echo ""
  echo "Services stopped and containers removed."
  echo ""
  echo "The following data was PRESERVED:"
  echo "  Backups:     ${BACKUP_DIR}"
  echo "  Audit logs:  ${AUDIT_DIR}"
  echo ""
  echo "To remove preserved data: sudo ./uninstall.sh --purge"
fi

echo ""
echo "TR: Kaldırma işlemi tamamlandı."
echo "EN: Uninstall complete."
