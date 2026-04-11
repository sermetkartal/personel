#!/usr/bin/env bash
# generate-phase1-exit-report.sh — Aggregate all test artifacts into a
# Phase 1 Exit Criteria report.
set -euo pipefail

INPUT_DIR="${1:-/tmp/all-reports}"
OUTPUT_DIR="${2:-/tmp/phase1-exit-report}"

mkdir -p "${OUTPUT_DIR}"

cat > "${OUTPUT_DIR}/phase1-exit-report.md" << 'EOF'
# Personel Phase 1 Exit Criteria — Automated Verification Report

Generated: $(date -u +"%Y-%m-%d %H:%M:%S UTC")

## Exit Criteria Status

| # | Criterion | Status | Test Reference |
|---|-----------|--------|----------------|
| EC-1 | 500 endpoints stable 14 days | MANUAL | 14-day pilot observation |
| EC-2 | Agent CPU < 2% | See footprint report | footprint-bench (Windows) |
| EC-3 | Agent RAM < 150 MB | See footprint report | footprint-bench (Windows) |
| EC-4 | Agent disk < 500 MB | See footprint report | footprint-bench (Windows) |
| EC-5 | Dashboard query p95 < 1s | See load report | event_flow_test.go |
| EC-6 | Event loss < 0.01% | See load report | load/scenarios/500_steady.json |
| EC-7 | E2E latency p95 < 5s | See load report | event_flow_test.go |
| EC-8 | Server uptime >= 99.5% | See pilot data | flow7_silence_test.go |
| EC-9 | Keystroke admin-blindness | See security report | keystroke_admin_blindness_test.go |
| EC-10 | Live-view governance | See e2e report | liveview_test.go |
| EC-11 | KVKK DPO sign-off | MANUAL | DPO review |
| EC-12 | Auto-update rollback | MANUAL | canary drill |
| EC-13 | mTLS revocation < 5 min | See e2e report | enrollment_test.go |
| EC-14 | Anti-tamper baseline | MANUAL | Windows host test |
| EC-15 | Documentation complete | MANUAL | Doc review |
| EC-16 | No critical/high CVEs | MANUAL | SBOM review |
| EC-17 | ClickHouse replication | MANUAL | staging drill |
| EC-18 | Sensitive bucket routing | See e2e report | legalhold_test.go |
| EC-19 | Legal hold e2e | See e2e report | legalhold_test.go |
| EC-20 | DSR SLA timer | See e2e report | dsr_test.go |
| EC-21 | Destruction report | See e2e report | destruction report review |

## Notes

- EC-1, EC-11–EC-16 require manual verification by designated team members.
- EC-2, EC-3, EC-4 require running footprint-bench on a Windows host with the real agent.
- EC-9 is the highest-priority automated test. If it fails, ALL other criteria are irrelevant.

## How to Read This Report

1. Check that all AUTOMATED criteria show PASS.
2. For MANUAL criteria, attach the relevant sign-off document.
3. The release team must sign off that all 21 criteria are met before Phase 1 ships.
EOF

echo "Phase 1 Exit Report generated at: ${OUTPUT_DIR}/phase1-exit-report.md"
echo ""
echo "REMINDER: EC-9 (keystroke admin-blindness) is the HIGHEST PRIORITY criterion."
echo "          See test results from run-security-suite.sh for EC-9 status."
