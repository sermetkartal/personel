package siem

import (
	"fmt"
	"sync"
)

// Factory constructs an Exporter from a Config.
type Factory func(cfg Config) (Exporter, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register associates a Factory with an exporter name. Panics on
// duplicate registration (build-time error).
func Register(name string, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("siem: duplicate factory registration for %q", name))
	}
	registry[name] = factory
}

// Build constructs an Exporter from a Config.
func Build(cfg Config) (Exporter, error) {
	registryMu.RLock()
	factory, ok := registry[cfg.Name]
	registryMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("siem: no registered exporter for %q; known: %v",
			cfg.Name, KnownExporters())
	}
	return factory(cfg)
}

// KnownExporters returns a sorted list of registered exporter names.
func KnownExporters() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
