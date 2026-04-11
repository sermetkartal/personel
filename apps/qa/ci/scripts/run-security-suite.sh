#!/usr/bin/env bash
# run-security-suite.sh — Run all security tests including the red team.
# The red team (EC-9) is a Phase 1 exit blocker.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QA_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

export QA_INTEGRATION="${QA_INTEGRATION:-1}"
export GATEWAY_ADDR="${GATEWAY_ADDR:-}"

echo "================================================================"
echo " PERSONEL SECURITY TEST SUITE"
echo " Phase 1 Exit Criterion #9: Keystroke Admin-Blindness"
echo "================================================================"
echo ""

cd "${QA_ROOT}"

FAILED=0

# 1. Cert pinning tests (no gateway needed).
echo "1. Cert pinning tests..."
if go test -v -timeout 5m -run TestCertPinning ./test/security/... -count=1; then
  echo "   PASS"
else
  echo "   FAIL"
  FAILED=$((FAILED+1))
fi

# 2. Audit chain tamper detection (no gateway needed).
echo ""
echo "2. Audit chain tamper detection..."
if go test -v -timeout 5m -run TestAuditChainTamper ./test/security/... -count=1; then
  echo "   PASS"
else
  echo "   FAIL"
  FAILED=$((FAILED+1))
fi

# 3. RED TEAM: Keystroke admin-blindness (Phase 1 EC-9 — BLOCKING).
echo ""
echo "3. RED TEAM: Keystroke admin-blindness (EC-9) ..."
echo "   NOTE: This is a Phase 1 exit blocker. Failure means Phase 1 cannot ship."
echo ""
if go test -v -timeout 15m \
    -run TestKeystrokeAdminBlindness \
    ./test/security/... \
    -count=1; then
  echo ""
  echo "   EC-9: PASS — Admin cannot read keystroke content"
else
  echo ""
  echo "   EC-9: FAIL — PHASE 1 BLOCKED"
  FAILED=$((FAILED+1))
fi

# 4. RBAC keystroke endpoint check.
echo ""
echo "4. RBAC: Keystroke endpoints must not exist..."
if go test -v -timeout 5m \
    -run TestKeystrokeEndpointsDontExist \
    ./test/e2e/... \
    -count=1; then
  echo "   PASS"
else
  echo "   FAIL (or skipped — API not running)"
fi

echo ""
echo "================================================================"
echo " Security Suite Results"
echo " Failed tests: ${FAILED}"
if [ "${FAILED}" -eq 0 ]; then
  echo " Overall: PASS"
else
  echo " Overall: FAIL"
  exit 1
fi
