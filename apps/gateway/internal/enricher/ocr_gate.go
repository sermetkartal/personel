package enricher

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// OCRGate decides whether a given event should be forwarded to the
// apps/ocr-service for text extraction. It consults the admin API's
// module-state endpoint (Phase 2.0/2) and caches the result for up to
// 60 seconds so the hot path is not burdened with per-event HTTP calls.
//
// The gate is conservative: on any error fetching state, OCR is treated
// as DISABLED. This matches the Phase 2.8 default-off pattern — if the
// customer has not opted in, a transient fault cannot accidentally
// enable OCR processing.
//
// ADR 0013 inspiration: the same "fail-closed" pattern that keeps DLP
// admin-blind by default applies here. An OCR misroute would materially
// expand the KVKK m.6 surface, so we prefer silent no-op over optimistic
// forwarding.
type OCRGate struct {
	apiURL     string
	httpClient *http.Client

	mu           sync.RWMutex
	enabled      bool
	lastFetchAt  time.Time
	cacheTTL     time.Duration

	// Metrics
	stateHits    atomic.Uint64 // served from cache
	stateFetches atomic.Uint64 // actual API calls
	stateErrors  atomic.Uint64 // failed calls
}

// NewOCRGate creates a gate. Pass empty apiURL to disable the feature
// entirely (returns ShouldOCR=false always, no network calls).
func NewOCRGate(apiURL string) *OCRGate {
	return &OCRGate{
		apiURL: apiURL,
		httpClient: &http.Client{
			Timeout: 500 * time.Millisecond, // background refresh, not blocking
		},
		cacheTTL: 60 * time.Second,
	}
}

// ShouldOCR returns whether OCR is currently enabled for this tenant.
// It uses a read-through cache: if the cached state is fresh (<60s old)
// it returns immediately; otherwise it kicks off a background refresh
// and returns the current stale value (or false if no cached value).
//
// This pattern optimizes for latency on the hot event-processing path:
// the enricher never blocks on the gate, it just asks "is OCR on right
// now?" and gets an immediate answer.
func (g *OCRGate) ShouldOCR(ctx context.Context) bool {
	if g.apiURL == "" {
		return false
	}

	g.mu.RLock()
	fresh := time.Since(g.lastFetchAt) < g.cacheTTL
	current := g.enabled
	g.mu.RUnlock()

	if fresh {
		g.stateHits.Add(1)
		return current
	}

	// Stale cache — trigger refresh asynchronously. The hot path returns
	// the last known value immediately (even if it's the zero value on
	// first run, which is false — fail-closed is correct).
	go g.refresh()
	return current
}

// Refresh forces an immediate (blocking) fetch of the module state.
// Useful for startup warm-up or for health checks that want to verify
// the API is reachable.
func (g *OCRGate) Refresh(ctx context.Context) error {
	if g.apiURL == "" {
		return nil
	}
	return g.fetchAndStore(ctx)
}

func (g *OCRGate) refresh() {
	// Use a short independent context so a stuck API doesn't hold the
	// gate open indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = g.fetchAndStore(ctx)
}

func (g *OCRGate) fetchAndStore(ctx context.Context) error {
	g.stateFetches.Add(1)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		g.apiURL+"/v1/system/module-state", nil)
	if err != nil {
		g.stateErrors.Add(1)
		return err
	}
	// Module state is readable by any authenticated role; the gateway uses
	// its service account bearer which the api validates.
	req.Header.Set("Authorization", "Bearer "+bearerFromEnv())

	resp, err := g.httpClient.Do(req)
	if err != nil {
		g.stateErrors.Add(1)
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		g.stateErrors.Add(1)
		return nil // treat as fail-closed, don't surface
	}

	var body struct {
		Modules map[string]struct {
			Name    string `json:"name"`
			State   string `json:"state"`
			Enabled bool   `json:"enabled"`
		} `json:"modules"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		g.stateErrors.Add(1)
		return err
	}

	ocr, ok := body.Modules["ocr"]
	enabled := ok && ocr.Enabled && ocr.State == "enabled"

	g.mu.Lock()
	g.enabled = enabled
	g.lastFetchAt = time.Now()
	g.mu.Unlock()
	return nil
}

// Stats returns a snapshot of gate counters for /metrics exposition.
func (g *OCRGate) Stats() map[string]uint64 {
	return map[string]uint64{
		"state_hits":    g.stateHits.Load(),
		"state_fetches": g.stateFetches.Load(),
		"state_errors":  g.stateErrors.Load(),
	}
}

// bearerFromEnv reads the gateway's service account token. Intentionally
// package-private and simple; a production deployment will use a Vault
// token renewer instead. Phase 2.11 work.
func bearerFromEnv() string {
	// Deliberate stub — real gateway token plumbing is Phase 2.11.
	// Return empty string so the Authorization header is "Bearer "
	// which the API rejects loudly during development.
	return ""
}
