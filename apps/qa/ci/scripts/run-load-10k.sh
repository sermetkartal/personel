#!/usr/bin/env bash
# run-load-10k.sh — Run the 10K-agent scale cliff test.
# Requires GATEWAY_ADDR=host:port on a sufficiently resourced host.
# Expected runtime: ~45 minutes.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QA_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

GATEWAY_ADDR="${GATEWAY_ADDR:-localhost:9443}"
REPORT_DIR="${REPORT_DIR:-/tmp/reports/load-10k}"
SCENARIO="${SCENARIO:-ramp}"  # ramp | burst | chaos

case "${SCENARIO}" in
  ramp)   SCENARIO_FILE="test/load/scenarios/10k_ramp.json" ;;
  burst)  SCENARIO_FILE="test/load/scenarios/10k_burst.json" ;;
  chaos)  SCENARIO_FILE="test/load/scenarios/chaos_mix.json" ;;
  *)      echo "Unknown scenario: ${SCENARIO}. Use: ramp|burst|chaos"; exit 1 ;;
esac

echo "Starting 10K-agent load test (${SCENARIO})..."
echo "  Gateway:  ${GATEWAY_ADDR}"
echo "  Scenario: ${SCENARIO_FILE}"
echo "  Report:   ${REPORT_DIR}"
echo ""
echo "NOTE: This test takes ~45 minutes and requires significant system resources."
echo "      Gateway host needs >= 16 GB RAM and fast NVMe for NATS + ClickHouse."
echo ""

mkdir -p "${REPORT_DIR}"

cd "${QA_ROOT}"
go build -o /tmp/personel-simulator ./cmd/simulator/...

/tmp/personel-simulator run \
  --scenario "${SCENARIO_FILE}" \
  --gateway "${GATEWAY_ADDR}" \
  --thresholds ci/thresholds.yaml \
  --report "${REPORT_DIR}" \
  --progress

echo ""
echo "10K load test complete. Report at: ${REPORT_DIR}"
