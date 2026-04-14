# Integration Tests (apps/api)

Run with:

```bash
cd apps/api
go test -tags=integration -race -timeout=300s ./test/integration/...
```

Requires Docker (testcontainers-go). Each test uses `testDB(t)` to
spin up a real Postgres 16 container and run all migrations.

## Pattern

1. **Build tag**: every file starts with `//go:build integration`.
2. **Helpers**: `helpers_test.go` provides `testDB(t)`, `testLogger(t)`,
   and other shared container factories.
3. **Scoping**: Use `context.Background()` + an explicit `defer pool.Close()`.
4. **Fixtures**: Seed inline with `pool.Exec(ctx, INSERT ...)`. Avoid
   shared SQL files — each test owns its data.
5. **Cleanup**: `t.Cleanup(func() { ctr.Terminate(ctx) })`. Do NOT
   rely on test ordering; containers are per-test.
6. **Scaffolds**: Tests that await a not-yet-wired service
   (Faz 6/7/8 items) still compile today — they emit `t.Log(...)`
   as a TODO beacon and return. They MUST be flipped on once the
   implementation lands.

## Current coverage (Faz 14 #147)

| File | Maps to | Status |
|---|---|---|
| `audit_test.go` | Faz 1 | wired |
| `audit_verifier_test.go` | Faz 1 | wired |
| `dsr_test.go` | Faz 1 | wired |
| `dsr_erasure_test.go` | Faz 1 | wired |
| `evidence_test.go` | Faz 3.0 | wired |
| `liveview_test.go` | Faz 1 | wired |
| `rbac_test.go` | Faz 1 | wired |
| `helpers_test.go` | shared | wired |
| `search_integration_test.go` | Faz 6 #67 | scaffold |
| `pipeline_dlq_integration_test.go` | Faz 7 #74 | scaffold |
| `endpoint_commands_integration_test.go` | Faz 6 #64/#65 | scaffold |
| `apikey_integration_test.go` | Faz 6 #72 | scaffold |
| `trends_integration_test.go` | Faz 8 #87 | scaffold |

## Promoting a scaffold to wired

1. The target service (e.g., `search.Service`) must export a
   constructor usable from the `integration` package without
   main.go's full dependency graph.
2. Replace the `t.Log(...)` placeholder with the real asserts.
3. Update this README's status column.
4. Run `go test -tags=integration` locally before committing.

## Known limitations

- **ClickHouse tests** (`trends_integration_test.go`) gated behind
  `INTEGRATION_CLICKHOUSE=1` because the container is ~300 MB.
- **OpenSearch tests** use an in-process `httptest.Server` fake
  rather than a real OpenSearch container; the real-container
  path is gated behind `INTEGRATION_OPENSEARCH=1`.
- **Keycloak tests** are in `test/e2e-playwright` (Faz 14 #148)
  instead of here — OIDC flows need a browser context.
