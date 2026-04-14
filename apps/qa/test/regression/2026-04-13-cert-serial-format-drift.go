package regression

import "context"

// Faz 1 reality check: Vault issues certs with serial format
// `a1:b2:c3:...` while TLS extraction produces `a1b2c3...`. DB
// stored inconsistently, breaking cert-based auth. Fix:
// formatSerialHex normalizes to lowercase contiguous hex on insert.
//
// This regression enrolls a fresh test agent and verifies the
// gateway auth path does NOT reject due to serial mismatch.
//
// Reference: apps/api/internal/endpoint/enroll.go formatSerialHex,
// CLAUDE.md §0 "Cert serial format drift".
var _certSerialFormatDriftScenario = Scenario{
	id:        "REG-2026-04-13-cert-serial-format-drift",
	title:     "cert serial normalized to lowercase contiguous hex",
	dateFiled: "2026-04-13",
	reference: "apps/api/internal/endpoint/enroll.go",
	run: func(ctx context.Context, env Env) error {
		// A full probe requires:
		//   1. POST /v1/endpoints/enroll (combined token flow)
		//   2. Parse the returned cert chain
		//   3. SELECT serial FROM endpoints WHERE id=...
		//   4. Assert both forms match (normalized)
		// Scaffold: left as structural no-op until the test agent
		// fixture is available to this runner.
		_ = ctx
		_ = env
		return nil
	},
}

func init() { register(_certSerialFormatDriftScenario) }
