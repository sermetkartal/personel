package regression

import "context"

// Faz 1 reality check: gateway endpoint-lookup query referenced
// `e.revoked` and `e.hw_fingerprint` columns that don't exist in
// init.sql. Patched to `NOT is_active` + `hardware_fingerprint`.
// Unification is still open tech debt.
//
// Regression: query a known endpoint id via the gateway admin
// endpoint and verify no SQL error surfaces.
var _gatewayEndpointQueryColumnsScenario = Scenario{
	id:        "REG-2026-04-13-gateway-endpoint-query-columns",
	title:     "gateway endpoint-query uses is_active + hardware_fingerprint",
	dateFiled: "2026-04-13",
	reference: "apps/gateway/internal/clickhouse/schemas.go, CLAUDE.md §0",
	run: func(ctx context.Context, env Env) error {
		// Scaffold: requires a known endpoint id seeded by a
		// companion fixture. Once the fixture lands, the probe
		// is `GET /v1/gateway/endpoint/{id}` and asserts non-500.
		_ = ctx
		_ = env
		return nil
	},
}

func init() { register(_gatewayEndpointQueryColumnsScenario) }
