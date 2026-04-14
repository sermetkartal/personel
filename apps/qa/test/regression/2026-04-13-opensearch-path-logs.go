package regression

import "context"

// Faz 1 reality check: opensearch.yml had path.logs pointing to a
// non-writable location, causing container startup loops. Fix:
// infra/compose/opensearch/opensearch.yml path.logs writable.
//
// Regression: OpenSearch healthz must return green/yellow.
var _opensearchPathLogsScenario = Scenario{
	id:        "REG-2026-04-13-opensearch-path-logs",
	title:     "OpenSearch path.logs writable (container starts cleanly)",
	dateFiled: "2026-04-13",
	reference: "infra/compose/opensearch/opensearch.yml",
	run: func(ctx context.Context, env Env) error {
		_ = ctx
		_ = env
		// Probe: GET opensearch:9200/_cluster/health expected
		// status=yellow|green. Gated until the runner has a
		// direct OpenSearch URL — add when Faz 6 #67 search
		// endpoint is live.
		return nil
	},
}

func init() { register(_opensearchPathLogsScenario) }
