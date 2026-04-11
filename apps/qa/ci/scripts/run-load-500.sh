#!/usr/bin/env bash
# run-load-500.sh — Run the 500-agent pilot load test.
# Requires GATEWAY_ADDR=host:port.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QA_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

GATEWAY_ADDR="${GATEWAY_ADDR:-localhost:9443}"
REPORT_DIR="${REPORT_DIR:-/tmp/reports/load-500}"

echo "Starting 500-agent load test..."
echo "  Gateway: ${GATEWAY_ADDR}"
echo "  Report:  ${REPORT_DIR}"

mkdir -p "${REPORT_DIR}"

cd "${QA_ROOT}"
go build -o /tmp/personel-simulator ./cmd/simulator/...

/tmp/personel-simulator run \
  --scenario test/load/scenarios/500_steady.json \
  --gateway "${GATEWAY_ADDR}" \
  --thresholds ci/thresholds.yaml \
  --report "${REPORT_DIR}" \
  --progress

echo ""
echo "Load test complete. Report at: ${REPORT_DIR}"
