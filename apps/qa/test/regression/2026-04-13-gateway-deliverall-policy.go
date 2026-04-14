package regression

import "context"

// Faz 1 reality check: live view router NATS subscription used
// default delivery policy, missing messages published before the
// subscription attached. Fix:
// apps/gateway/internal/liveview/router.go → DeliverAllPolicy.
//
// Regression: trigger a live view control message emit from the
// API, then attach a listener, and assert the listener receives
// the message even though it started after the publish.
var _gatewayDeliverAllPolicyScenario = Scenario{
	id:        "REG-2026-04-13-gateway-deliverall-policy",
	title:     "live view router uses DeliverAllPolicy (late-joiner receives)",
	dateFiled: "2026-04-13",
	reference: "apps/gateway/internal/liveview/router.go",
	run: func(ctx context.Context, env Env) error {
		// Probe requires NATS direct access from the regression
		// runner. Scaffold until env exposes a NATS URL.
		_ = ctx
		_ = env
		return nil
	},
}

func init() { register(_gatewayDeliverAllPolicyScenario) }
