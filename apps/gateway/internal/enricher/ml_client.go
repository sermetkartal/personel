package enricher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// MLClassifierClient is a thin HTTP client for the Python ml-classifier
// service (apps/ml-classifier). It is used by the enricher to categorize
// events before the ClickHouse insert. The client has a hard 50ms deadline
// per request — if the ML service is slow or unavailable, the enricher
// degrades to RegexFallbackClassifier.
//
// ADR 0017 Phase 2.3 architecture:
//
//	agent → gateway → enricher ─┬─→ MLClassifierClient ─→ ml-classifier (net_ml)
//	                            └─→ RegexFallbackClassifier (local, always available)
//
// Network topology: the enricher runs on `data` + `net_ml` Docker networks;
// ml-classifier runs on `net_ml` only (internal: true, no egress).
type MLClassifierClient struct {
	baseURL    string
	httpClient *http.Client
	fallback   *RegexFallbackClassifier

	// Metrics (cheap atomic counters — full Prometheus wiring is in
	// observability/metrics.go which we don't touch from here).
	callsTotal    atomic.Uint64
	callsTimeout  atomic.Uint64
	callsError    atomic.Uint64
	callsFallback atomic.Uint64
}

// MLClassifierConfig is the constructor input.
type MLClassifierConfig struct {
	// BaseURL is the ml-classifier service endpoint, e.g. "http://ml-classifier:8080".
	// Empty string disables ML and forces fallback-only.
	BaseURL string
	// Timeout is the per-request deadline. Default 50ms.
	Timeout time.Duration
}

// NewMLClassifierClient creates a client. If cfg.BaseURL is empty, every
// Classify call goes directly to the fallback.
func NewMLClassifierClient(cfg MLClassifierConfig) *MLClassifierClient {
	if cfg.Timeout == 0 {
		cfg.Timeout = 50 * time.Millisecond
	}
	return &MLClassifierClient{
		baseURL: cfg.BaseURL,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     60 * time.Second,
				// Force HTTP/1.1 with keep-alives — ml-classifier is uvicorn.
			},
		},
		fallback: NewRegexFallbackClassifier(),
	}
}

// Classify calls the ML service and returns the result. On any error
// (timeout, non-200, JSON parse, ADR 0017 confidence threshold violation)
// it silently falls through to the regex fallback. The fallback ALWAYS
// returns a result, so Classify never returns an error.
//
// Callers set the resulting fields on EventMeta (category, category_confidence)
// during enrichment.
func (c *MLClassifierClient) Classify(ctx context.Context, appName, windowTitle, url string) ClassifyResult {
	c.callsTotal.Add(1)

	// Short-circuit if ML is disabled.
	if c.baseURL == "" {
		c.callsFallback.Add(1)
		return c.fallback.Classify(appName, windowTitle, url)
	}

	body, err := json.Marshal(map[string]string{
		"app_name":     appName,
		"window_title": windowTitle,
		"url":          url,
	})
	if err != nil {
		c.callsError.Add(1)
		return c.fallback.Classify(appName, windowTitle, url)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/classify", bytes.NewReader(body))
	if err != nil {
		c.callsError.Add(1)
		return c.fallback.Classify(appName, windowTitle, url)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Timeout or connection refused — both degrade to fallback.
		c.callsTimeout.Add(1)
		return c.fallback.Classify(appName, windowTitle, url)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		c.callsError.Add(1)
		return c.fallback.Classify(appName, windowTitle, url)
	}

	var out ClassifyResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		c.callsError.Add(1)
		return c.fallback.Classify(appName, windowTitle, url)
	}

	// ADR 0017 threshold enforcement: if the service returns a result below
	// the confidence threshold, we still surface it as "unknown" rather than
	// trust a low-confidence categorization. Belt-and-suspenders: the service
	// ALSO enforces this threshold internally, but the enricher should not
	// trust upstream services to follow ADR invariants without verification.
	if out.Confidence < 0.70 {
		out = ClassifyResult{
			Category:   CategoryUnknown,
			Confidence: out.Confidence,
			Backend:    "ml-threshold-below-0.70",
		}
	}

	// Tag backend for observability.
	if out.Backend == "" {
		out.Backend = "llama"
	}

	return out
}

// Stats returns a snapshot of client counters (for /metrics exposition).
func (c *MLClassifierClient) Stats() map[string]uint64 {
	return map[string]uint64{
		"calls_total":    c.callsTotal.Load(),
		"calls_timeout":  c.callsTimeout.Load(),
		"calls_error":    c.callsError.Load(),
		"calls_fallback": c.callsFallback.Load(),
	}
}

// Assert interface conformance at compile time: any Classifier can be
// used interchangeably with *MLClassifierClient.
type Classifier interface {
	Classify(ctx context.Context, appName, windowTitle, url string) ClassifyResult
}

// contextAwareFallback wraps the regex fallback in a Classifier-compatible
// signature (it ignores the context). Used for tests and for ML-disabled
// deployments.
type contextAwareFallback struct {
	inner *RegexFallbackClassifier
}

// NewFallbackOnlyClassifier returns a Classifier that never calls the ML
// service. Use this in deployments where the ml-classifier container is
// not started (e.g. customers who haven't opted in to Phase 2 features).
func NewFallbackOnlyClassifier() Classifier {
	return &contextAwareFallback{inner: NewRegexFallbackClassifier()}
}

// Classify delegates to the underlying regex classifier.
func (f *contextAwareFallback) Classify(_ context.Context, appName, windowTitle, url string) ClassifyResult {
	return f.inner.Classify(appName, windowTitle, url)
}

// compile-time interface check
var _ Classifier = (*MLClassifierClient)(nil)
var _ Classifier = (*contextAwareFallback)(nil)

// helper not exported — used in logging.
func formatClassifyError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("ml-classifier: %v", err)
}
