# Troubleshooting

## "Agent did not connect within 15s"

Gateway is likely not running. Check:
1. `GATEWAY_ADDR` is set correctly.
2. Gateway container is healthy: `docker ps | grep gateway`
3. TLS: verify the test CA cert is trusted. Run with `--verbose` to see TLS errors.

## "expected Welcome, got *personelv1.ServerMessage_RotateCert"

The gateway is sending a RotateCert because the test agent's key versions don't
match what's in Postgres. This is expected behavior tested in `keyrotation_test.go`.
For smoke tests, ensure the test tenant has a clean keystroke_keys row at version 1.

## EC-9 Red Team fails with "API not available"

The Admin API is not running. EC-9 with a live stack requires:
- `QA_INTEGRATION=1`
- `GATEWAY_ADDR` pointing to a stack that includes the API
- Vault running in dev mode with the test token

## testcontainers fails with "Docker daemon not found"

- macOS: ensure Docker Desktop is running
- Linux CI: ensure `docker:dind` service is in the workflow or Docker is available
- The harness uses `TESTCONTAINERS_HOST_OVERRIDE` for custom Docker socket paths

## Fuzz tests find a new crash

1. The crashing input is saved to `testdata/fuzz/FuzzXxx/` automatically.
2. Reproduce: `go test -run=FuzzXxx/the-crash-file ./test/security/fuzz/`
3. Fix the underlying issue in the proto parsing code.
4. Commit the crash corpus file so future runs catch regressions.

## "CRITICAL: path /v1/keystroke/decrypt returned 200"

**This is a Phase 1 blocker.** The Admin API is exposing a keystroke decrypt
endpoint that must not exist. Immediately:
1. Stop the pipeline.
2. Notify the security-engineer and backend-developer.
3. The endpoint must be removed before Phase 1 ships.
4. Re-run EC-9 after the fix is merged.
