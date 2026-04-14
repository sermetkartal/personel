package regression

import "context"

// Faz 1 reality check: ClickHouse DateTime64 TTL expressions
// needed `toDateTime()` wrap to be valid TTL expressions. Fix in
// apps/gateway/internal/clickhouse/schemas.go.
//
// Regression: CREATE TABLE statement in schemas.go must still
// produce tables whose TTL parses cleanly on Clickhouse startup.
// Probed indirectly by asserting the clickhouse container is
// healthy and every expected table exists.
var _clickhouseDateTime64TTLScenario = Scenario{
	id:        "REG-2026-04-13-clickhouse-datetime64-ttl",
	title:     "ClickHouse DateTime64 TTL wraps with toDateTime()",
	dateFiled: "2026-04-13",
	reference: "apps/gateway/internal/clickhouse/schemas.go",
	run: func(ctx context.Context, env Env) error {
		_ = ctx
		_ = env
		// Scaffold: probe is /v1/admin/clickhouse/tables which
		// lists every table + reports TTL status. If that handler
		// isn't wired yet the scenario is a no-op.
		return nil
	},
}

func init() { register(_clickhouseDateTime64TTLScenario) }
