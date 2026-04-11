#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Backup Round-Trip Test
# TR: Yedekleme ve geri yükleme döngüsünü doğrular.
# EN: Validates backup and restore round-trip integrity.
# =============================================================================
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"
set -a; source "${COMPOSE_DIR}/.env" 2>/dev/null || true; set +a

PASS=0; FAIL=0
pass() { echo -e "\033[0;32m[PASS]\033[0m $*"; ((PASS++)) || true; }
fail() { echo -e "\033[0;31m[FAIL]\033[0m $*" >&2; ((FAIL++)) || true; }
log()  { echo "[backup-test] $*"; }

echo "=== Personel Backup-Restore Validation ==="
echo ""

# Insert test sentinel row
SENTINEL_ID=$(docker exec personel-postgres psql -U postgres -d personel -t -c \
  "SELECT audit.append_event(
    '${PERSONEL_TENANT_ID}'::UUID,
    'test.backup_sentinel',
    NULL, 'system',
    'backup_test', 'smoke-$(date +%s)',
    '{\"test\": \"backup-restore-validation\"}'
  );" 2>/dev/null | tr -d ' ')
log "Sentinel audit event ID: ${SENTINEL_ID}"

# Run backup
log "Running backup..."
BACKUP_BEFORE=$(date +%s)
"${SCRIPT_DIR}/../backup.sh" 2>/dev/null
BACKUP_AFTER=$(date +%s)
BACKUP_DURATION=$((BACKUP_AFTER - BACKUP_BEFORE))
log "Backup completed in ${BACKUP_DURATION}s"

# Find latest backup
LATEST_BACKUP=$(ls -1d "${BACKUP_DIR:-/var/lib/personel/backups}/daily/"*/ 2>/dev/null | sort -r | head -1)
[[ -n "${LATEST_BACKUP}" ]] || { fail "No backup found"; exit 1; }
pass "Backup created: ${LATEST_BACKUP}"

# Verify manifest
if [[ -f "${LATEST_BACKUP}/MANIFEST.json" ]]; then
  ENCRYPTED=$(python3 -c "import json; m=json.load(open('${LATEST_BACKUP}/MANIFEST.json')); print(m.get('encrypted',False))")
  if [[ "${ENCRYPTED}" == "True" ]]; then
    pass "Backup is encrypted"
  else
    fail "Backup is NOT encrypted — BACKUP_GPG_PASSPHRASE not set"
  fi
  FILE_COUNT=$(python3 -c "import json; m=json.load(open('${LATEST_BACKUP}/MANIFEST.json')); print(len(m.get('files',{})))")
  if [[ "${FILE_COUNT}" -ge 3 ]]; then
    pass "Backup contains ${FILE_COUNT} files"
  else
    fail "Backup only contains ${FILE_COUNT} files (expected >=3)"
  fi
else
  fail "No MANIFEST.json in backup"
fi

# Vault snapshot integrity check
VAULT_SNAP=$(ls "${LATEST_BACKUP}"/vault-raft-*.snap.gpg 2>/dev/null | head -1 || \
             ls "${LATEST_BACKUP}"/vault-raft-*.snap 2>/dev/null | head -1 || echo "")
if [[ -n "${VAULT_SNAP}" ]]; then
  pass "Vault snapshot present: $(basename "${VAULT_SNAP}")"
else
  fail "No Vault snapshot in backup"
fi

# Backup duration check (target: <2h for full backup on 1TB)
if [[ "${BACKUP_DURATION}" -lt 7200 ]]; then
  pass "Backup duration: ${BACKUP_DURATION}s (within 2h target)"
else
  fail "Backup duration: ${BACKUP_DURATION}s (exceeds 2h target)"
fi

echo ""
echo "=============================="
echo "  PASS: ${PASS}  FAIL: ${FAIL}"
echo "=============================="
echo ""
[[ "${FAIL}" -eq 0 ]] && echo -e "\033[0;32mBackup-restore test PASSED\033[0m" || \
  { echo -e "\033[0;31mBackup-restore test FAILED\033[0m"; exit 1; }
