# How to Run the QA Framework

## Prerequisites

- Go 1.22+
- Docker (for testcontainers integration tests)
- `QA_INTEGRATION=1` env var to enable tests that start containers
- `GATEWAY_ADDR=host:port` for tests that require a running gateway

## Quick Start

```bash
cd apps/qa

# Unit tests only (fast, no Docker needed):
make unit

# Full e2e (requires Docker):
QA_INTEGRATION=1 make e2e

# Security tests + EC-9 red team:
QA_INTEGRATION=1 make security

# Single-agent probe against running gateway:
GATEWAY_ADDR=localhost:9443 make probe

# 500-agent load test:
GATEWAY_ADDR=localhost:9443 make load-500
```

## Running the Red Team (EC-9)

```bash
# The red team requires a full running stack.
# It will skip gracefully if the API is not available.
QA_INTEGRATION=1 GATEWAY_ADDR=localhost:9443 make redteam
```

## Running the 10K Load Test

The 10K test requires a gateway and infrastructure sized appropriately:
- Gateway host: >= 8 cores, 16 GB RAM
- NATS host: NVMe storage, >= 8 GB RAM
- ClickHouse: >= 32 GB RAM for write throughput

```bash
GATEWAY_ADDR=staging.personel.internal:9443 make load-10k
```

## Footprint Bench (Windows Only)

The footprint bench measures the real Rust agent on a Windows host:

1. Install the Rust agent: `personel-agent.exe --config agent.toml`
2. Note the agent PID: `tasklist | findstr personel`
3. Run the bench: `footprint-bench.exe --pid <PID> --exe "C:\Program Files\Personel" --duration 30m`

## CI Integration

The full CI pipeline is defined in `ci/github-actions/qa.yml`.

Key jobs:
- `lint-and-build`: fast, runs on every PR
- `unit-tests`: fast, runs on every PR
- `security-tests`: includes EC-9 red team — BLOCKING on main
- `e2e-tests`: integration tests with testcontainers
- `load-test-500`: weekly + manual trigger
- `load-test-10k`: manual trigger only
