#!/usr/bin/env bash
# run-e2e.sh — Run all e2e tests.
# Requires QA_INTEGRATION=1 and optionally GATEWAY_ADDR=host:port.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QA_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

export QA_INTEGRATION="${QA_INTEGRATION:-1}"
export GATEWAY_ADDR="${GATEWAY_ADDR:-}"

echo "Running e2e tests..."
echo "  QA_INTEGRATION=${QA_INTEGRATION}"
echo "  GATEWAY_ADDR=${GATEWAY_ADDR:-<not set — gateway tests will skip>}"
echo ""

cd "${QA_ROOT}"

go test -v -timeout 20m \
  ./test/e2e/... \
  -count=1 \
  "$@"
