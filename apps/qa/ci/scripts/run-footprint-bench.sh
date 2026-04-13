#!/usr/bin/env bash
# run-footprint-bench.sh — Build the Rust agent and the Go harness, then
# run the 5-minute footprint benchmark and archive the JSON report.
#
# Intended for the Windows CI runner (Git Bash / MSYS2). It expects the
# Rust toolchain, Go 1.22+, and an MSVC environment pre-sourced by the
# caller (vcvars64 or GitHub Actions windows-latest + actions/setup-go).
#
# Phase 1 exit criteria addressed: EC-2 (CPU avg < 2%) + EC-3 (RSS max <
# 150 MB). The harness exits non-zero if any footprint threshold is
# breached or the agent crashes mid-run.
#
# Usage:
#   ./run-footprint-bench.sh                   # 5m default
#   DURATION=2m ./run-footprint-bench.sh        # shorter for PR lanes
#
# Env overrides:
#   DURATION        — wall-clock bench window (default 5m)
#   INTERVAL        — sample interval (default 2s)
#   REPORT_DIR      — artifact output directory (default apps/qa/reports)
#   AGENT_EXTRA_ARGS— forwarded to personel-agent.exe after "--"
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QA_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
REPO_ROOT="$(cd "${QA_ROOT}/../.." && pwd)"

DURATION="${DURATION:-5m}"
INTERVAL="${INTERVAL:-2s}"
REPORT_DIR="${REPORT_DIR:-${QA_ROOT}/reports}"
AGENT_EXTRA_ARGS="${AGENT_EXTRA_ARGS:-}"

AGENT_DIR="${REPO_ROOT}/apps/agent"
AGENT_TARGET="x86_64-pc-windows-msvc"
AGENT_BIN="${AGENT_DIR}/target/${AGENT_TARGET}/release/personel-agent.exe"
HARNESS_BIN="${QA_ROOT}/footprint-bench.exe"
THRESHOLDS="${QA_ROOT}/ci/thresholds.yaml"
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
REPORT_FILE="${REPORT_DIR}/footprint-bench-${TIMESTAMP}.json"

echo "== run-footprint-bench =="
echo "  repo    : ${REPO_ROOT}"
echo "  duration: ${DURATION}"
echo "  interval: ${INTERVAL}"
echo "  report  : ${REPORT_FILE}"

mkdir -p "${REPORT_DIR}"

echo ""
echo "[1/3] Building Rust agent (release, ${AGENT_TARGET})..."
(
  cd "${AGENT_DIR}"
  cargo build --release --target "${AGENT_TARGET}" -p personel-agent
)

if [[ ! -x "${AGENT_BIN}" ]]; then
  echo "ERROR: agent binary not found after build: ${AGENT_BIN}" >&2
  exit 2
fi

echo ""
echo "[2/3] Building footprint-bench harness..."
(
  cd "${QA_ROOT}"
  go build -o "${HARNESS_BIN}" ./cmd/footprint-bench
)

echo ""
echo "[3/3] Running benchmark (${DURATION})..."
set +e
"${HARNESS_BIN}" \
  --agent "${AGENT_BIN}" \
  --duration "${DURATION}" \
  --interval "${INTERVAL}" \
  --thresholds "${THRESHOLDS}" \
  --report "${REPORT_FILE}" \
  ${AGENT_EXTRA_ARGS:+-- ${AGENT_EXTRA_ARGS}}
BENCH_RC=$?
set -e

echo ""
if [[ ${BENCH_RC} -eq 0 ]]; then
  echo "Footprint bench PASSED. Report: ${REPORT_FILE}"
else
  echo "Footprint bench FAILED (exit ${BENCH_RC}). Report: ${REPORT_FILE}" >&2
fi
exit ${BENCH_RC}
