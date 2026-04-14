#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Final Smoke Test (Faz 17 #187)
# TR: Tam yığın üzerinde uçtan uca doğrulama: preflight → post-install →
#     smoke binary → phase1-exit. Tek JSON + Markdown raporla biter.
# EN: End-to-end validation over the full stack: preflight → post-install →
#     smoke binary → phase1-exit. Emits a single consolidated JSON +
#     Markdown summary.
#
# Time budget: 10 minutes hard ceiling.
# Companion runbook: infra/runbooks/final-smoke-test.md
#
# Usage:
#   ./final-smoke-test.sh \
#     --api-url http://192.168.5.44:8000 \
#     --gateway-url http://192.168.5.44:9443 \
#     --console-url http://192.168.5.44:3000 \
#     --portal-url http://192.168.5.44:3001 \
#     --admin-token "$ADMIN_JWT" \
#     --thresholds ../../apps/qa/ci/thresholds.yaml \
#     --out /var/log/personel/final-smoke.json \
#     --md  /var/log/personel/final-smoke.md
#
# Exit codes:
#   0 — all stages pass (or end-to-end soft warn on non-blocking checks)
#   1 — one or more blocking stages failed
#   2 — harness error (tool missing, config unreadable, timeout)
# =============================================================================
set -u -o pipefail

# -------- defaults ----------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

API_URL="${PERSONEL_API_URL:-http://192.168.5.44:8000}"
GATEWAY_URL="${PERSONEL_GATEWAY_URL:-http://192.168.5.44:9443}"
CONSOLE_URL="${PERSONEL_CONSOLE_URL:-http://192.168.5.44:3000}"
PORTAL_URL="${PERSONEL_PORTAL_URL:-http://192.168.5.44:3001}"
ADMIN_TOKEN="${PERSONEL_ADMIN_TOKEN:-}"
THRESHOLDS="${REPO_ROOT}/apps/qa/ci/thresholds.yaml"
OUT_JSON="/var/log/personel/final-smoke.json"
OUT_MD="/var/log/personel/final-smoke.md"
SKIP_PHASE1="false"
BUDGET_SECS=600   # 10-minute hard ceiling

for arg in "$@"; do
  case "${arg}" in
    --api-url=*)     API_URL="${arg#*=}" ;;
    --gateway-url=*) GATEWAY_URL="${arg#*=}" ;;
    --console-url=*) CONSOLE_URL="${arg#*=}" ;;
    --portal-url=*)  PORTAL_URL="${arg#*=}" ;;
    --admin-token=*) ADMIN_TOKEN="${arg#*=}" ;;
    --thresholds=*)  THRESHOLDS="${arg#*=}" ;;
    --out=*)         OUT_JSON="${arg#*=}" ;;
    --md=*)          OUT_MD="${arg#*=}" ;;
    --skip-phase1)   SKIP_PHASE1="true" ;;
    --budget=*)      BUDGET_SECS="${arg#*=}" ;;
    -h|--help)
      sed -n '1,60p' "${BASH_SOURCE[0]}"
      exit 0
      ;;
  esac
done

mkdir -p "$(dirname "${OUT_JSON}")" "$(dirname "${OUT_MD}")" 2>/dev/null || true

START_TS=$(date -u +%s)
ISO_START=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# -------- helpers -----------------------------------------------------------
STAGES_JSON="["
OVERALL_RC=0
FIRST=1

record_stage() {
  local name="$1" rc="$2" duration="$3" notes="$4"
  local status="pass"
  if [[ "${rc}" -ne 0 ]]; then status="fail"; OVERALL_RC=1; fi
  [[ "${FIRST}" -eq 1 ]] || STAGES_JSON+=","
  FIRST=0
  STAGES_JSON+=$(printf '{"name":"%s","status":"%s","exit_code":%s,"duration_seconds":%s,"notes":"%s"}' \
    "${name}" "${status}" "${rc}" "${duration}" "${notes//\"/\\\"}")
}

run_stage() {
  local name="$1"; shift
  local t0=$(date -u +%s)
  echo ""
  echo "=== [${name}] starting at $(date -u +%H:%M:%S) ==="
  local rc=0
  if ! "$@"; then rc=$?; fi
  local t1=$(date -u +%s)
  local dur=$(( t1 - t0 ))
  local notes=""
  if [[ "${rc}" -eq 0 ]]; then
    echo "=== [${name}] PASS in ${dur}s ==="
    notes="ok"
  else
    echo "=== [${name}] FAIL rc=${rc} after ${dur}s ==="
    notes="exit ${rc}; see stdout"
  fi
  record_stage "${name}" "${rc}" "${dur}" "${notes}"
  # abort on budget exhaustion
  local elapsed=$(( t1 - START_TS ))
  if [[ "${elapsed}" -ge "${BUDGET_SECS}" ]]; then
    echo "!!! time budget ${BUDGET_SECS}s exhausted at elapsed=${elapsed}s — skipping remaining stages"
    return 99
  fi
  return 0
}

require_bin() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "ERROR: required binary not found: $1"
    exit 2
  }
}

# -------- harness preconditions --------------------------------------------
require_bin curl
require_bin jq
require_bin go

if [[ ! -x "${SCRIPT_DIR}/preflight-check.sh" ]]; then
  echo "ERROR: preflight-check.sh not executable at ${SCRIPT_DIR}/preflight-check.sh"
  exit 2
fi
if [[ ! -x "${SCRIPT_DIR}/post-install-validate.sh" ]]; then
  echo "ERROR: post-install-validate.sh not executable at ${SCRIPT_DIR}/post-install-validate.sh"
  exit 2
fi

echo "================================================================"
echo "Personel Final Smoke Test — Faz 17 #187"
echo "  started: ${ISO_START}"
echo "  budget:  ${BUDGET_SECS}s"
echo "  api:     ${API_URL}"
echo "  gateway: ${GATEWAY_URL}"
echo "  console: ${CONSOLE_URL}"
echo "  portal:  ${PORTAL_URL}"
echo "  out:     ${OUT_JSON}"
echo "================================================================"

# -------- Stage 1: preflight (non-blocking on warn) ------------------------
run_stage "preflight" bash "${SCRIPT_DIR}/preflight-check.sh" --quiet || true

# -------- Stage 2: post-install-validate -----------------------------------
POST_INSTALL_JSON="/tmp/personel-final-postinstall.json"
run_stage "post-install-validate" bash "${SCRIPT_DIR}/post-install-validate.sh" \
  --report="${POST_INSTALL_JSON}" --quick || true

# -------- Stage 3: smoke binary --------------------------------------------
SMOKE_JSON="/tmp/personel-final-smoke.json"
SMOKE_BIN="${REPO_ROOT}/apps/qa/bin/smoke"
if [[ ! -x "${SMOKE_BIN}" ]]; then
  run_stage "smoke-build" bash -c "cd '${REPO_ROOT}/apps/qa' && go build -o bin/smoke ./cmd/smoke"
fi
if [[ -x "${SMOKE_BIN}" ]]; then
  run_stage "smoke" "${SMOKE_BIN}" \
    --api="${API_URL}" \
    --gateway="${GATEWAY_URL}" \
    --console="${CONSOLE_URL}" \
    --portal="${PORTAL_URL}" \
    --admin-token="${ADMIN_TOKEN}" \
    --out="${SMOKE_JSON}" || true
fi

# -------- Stage 4: phase1-exit (optional, can be slow) ---------------------
if [[ "${SKIP_PHASE1}" != "true" ]]; then
  PHASE1_JSON="/tmp/personel-final-phase1.json"
  PHASE1_MD="/tmp/personel-final-phase1.md"
  PHASE1_BIN="${REPO_ROOT}/apps/qa/bin/phase1-exit"
  if [[ ! -x "${PHASE1_BIN}" ]]; then
    run_stage "phase1-exit-build" bash -c "cd '${REPO_ROOT}/apps/qa' && go build -o bin/phase1-exit ./cmd/phase1-exit"
  fi
  if [[ -x "${PHASE1_BIN}" && -r "${THRESHOLDS}" ]]; then
    run_stage "phase1-exit" "${PHASE1_BIN}" \
      --thresholds="${THRESHOLDS}" \
      --api-url="${API_URL}" \
      --out="${PHASE1_JSON}" \
      --md="${PHASE1_MD}" || true
  else
    echo "warn: phase1-exit skipped (binary or thresholds missing)"
  fi
fi

STAGES_JSON+="]"
END_TS=$(date -u +%s)
TOTAL_DUR=$(( END_TS - START_TS ))
ISO_END=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# -------- emit JSON + MD ----------------------------------------------------
cat > "${OUT_JSON}" <<EOF
{
  "tool": "final-smoke-test.sh",
  "phase": "Faz 17 #187",
  "started_at": "${ISO_START}",
  "ended_at": "${ISO_END}",
  "total_duration_seconds": ${TOTAL_DUR},
  "budget_seconds": ${BUDGET_SECS},
  "overall": "$( [[ "${OVERALL_RC}" -eq 0 ]] && echo pass || echo fail )",
  "exit_code": ${OVERALL_RC},
  "endpoints": {
    "api": "${API_URL}",
    "gateway": "${GATEWAY_URL}",
    "console": "${CONSOLE_URL}",
    "portal": "${PORTAL_URL}"
  },
  "stages": ${STAGES_JSON}
}
EOF

{
  echo "# Personel Final Smoke Test Report"
  echo ""
  echo "- **Started**: ${ISO_START}"
  echo "- **Ended**: ${ISO_END}"
  echo "- **Duration**: ${TOTAL_DUR}s (budget ${BUDGET_SECS}s)"
  echo "- **Overall**: $( [[ "${OVERALL_RC}" -eq 0 ]] && echo "PASS" || echo "FAIL" )"
  echo ""
  echo "## Stages"
  echo ""
  echo "See \`${OUT_JSON}\` for structured data."
  echo ""
  echo "## Next Steps"
  if [[ "${OVERALL_RC}" -eq 0 ]]; then
    echo "- All stages green. Attach this report + phase1-exit-report.md to the pilot sign-off ticket."
  else
    echo "- One or more stages failed. Consult the per-stage logs above and the Turkish runbook \`infra/runbooks/final-smoke-test.md\` §Sorun Giderme."
  fi
} > "${OUT_MD}"

echo ""
echo "================================================================"
echo "final-smoke-test complete: overall=$( [[ "${OVERALL_RC}" -eq 0 ]] && echo PASS || echo FAIL ) duration=${TOTAL_DUR}s"
echo "  json: ${OUT_JSON}"
echo "  md:   ${OUT_MD}"
echo "================================================================"

exit "${OVERALL_RC}"
