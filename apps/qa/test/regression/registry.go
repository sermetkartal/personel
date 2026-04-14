// Package regression — Faz 14 #156 regression scenario registry.
//
// Each historical bug becomes a reusable scenario file under this
// package. Scenarios register themselves via init() into a
// package-level slice. The cmd/regression binary enumerates them
// and runs Run(ctx, env).
//
// Scenario file naming: YYYY-MM-DD-<short-desc>.go. Exported
// Scenario value named `_<short>Scenario`. Example:
//
//   // File: 2026-04-13-audit-append-event-overload.go
//   var _auditAppendEventOverloadScenario = Scenario{...}
//   func init() { register(_auditAppendEventOverloadScenario) }
//
// Scenarios SHOULD be short, idempotent, and assert against a
// single invariant that historically broke. They are run against
// the same deployed stack every PR, so slow scenarios (> 5s)
// should be marked Slow and gated behind REGRESSION_SLOW=1.
package regression

import (
	"context"
	"net/http"
	"sync"
)

// Env is injected into every scenario's Run method.
type Env struct {
	APIURL     string
	Client     *http.Client
	AdminToken string
}

// Scenario is the unit of work the regression runner executes.
type Scenario struct {
	id        string
	title     string
	dateFiled string
	reference string
	slow      bool
	run       func(ctx context.Context, env Env) error
}

func (s Scenario) ID() string        { return s.id }
func (s Scenario) Title() string     { return s.title }
func (s Scenario) DateFiled() string { return s.dateFiled }
func (s Scenario) Reference() string { return s.reference }
func (s Scenario) Slow() bool        { return s.slow }
func (s Scenario) Run(ctx context.Context, env Env) error {
	if s.run == nil {
		return nil
	}
	return s.run(ctx, env)
}

var (
	registryMu sync.Mutex
	registry   []Scenario
)

func register(s Scenario) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = append(registry, s)
}

// All returns a snapshot of every registered scenario.
func All() []Scenario {
	registryMu.Lock()
	defer registryMu.Unlock()
	out := make([]Scenario, len(registry))
	copy(out, registry)
	return out
}
