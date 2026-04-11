package hris

import (
	"fmt"
	"sync"
)

// Factory constructs a Connector from a Config. Each adapter package
// registers a Factory at init() time.
type Factory func(cfg Config) (Connector, error)

// registry is the global compile-time registry of HRIS adapter factories.
// Adapters register themselves in their init() functions by calling
// Register; the api cmd/ binary imports the adapter packages by path
// (with blank imports if needed) to trigger their init() functions.
//
// This pattern is deliberately compile-time: runtime plugin loading
// (plugin.Open) is rejected by ADR 0018 for security reasons. The set
// of available adapters is frozen at build time.
var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register associates a Factory with a connector name. Panics on duplicate
// registration because this indicates a build-time error. Called from
// adapter package init() functions.
func Register(name string, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("hris: duplicate factory registration for %q", name))
	}
	registry[name] = factory
}

// Build constructs a Connector from a Config by looking up the factory
// registered for cfg.Name. Returns ErrorPermanent if the name is unknown.
func Build(cfg Config) (Connector, error) {
	registryMu.RLock()
	factory, ok := registry[cfg.Name]
	registryMu.RUnlock()

	if !ok {
		return nil, Wrap(cfg.Name, ErrorPermanent,
			fmt.Errorf("no registered adapter for %q; known: %v", cfg.Name, KnownAdapters()))
	}
	return factory(cfg)
}

// KnownAdapters returns a sorted list of registered adapter names.
// Used for error messages and the `TestConnection` endpoint that
// validates customer-supplied config.
func KnownAdapters() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	// Caller-side sort to keep the registry deterministic. Go 1.21+ has
	// slices.Sort, but for Go 1.22 compatibility we use a manual sort.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
