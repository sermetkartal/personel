// Package license — online validation (phone-home) + heartbeat.
//
// Online validation is OPTIONAL. It exists so that Personel can
// (a) revoke a license that was cloned to multiple hosts, and
// (b) collect coarse-grained telemetry for capacity planning.
//
// Privacy
// -------
//
// The phone-home payload contains ONLY:
//   - customer_id (from the license file)
//   - endpoint_count (integer)
//   - version (Personel build)
//   - license_fingerprint (from the license file, if any)
//
// It does NOT contain:
//   - Any user data
//   - Any event data
//   - IP address (the license server sees the source IP but must not log it)
//   - Any KVKK-sensitive information
//
// Customers in regulated sectors can disable online validation entirely
// via the license.online_validation=false claim. Air-gapped deployments
// rely on offline signature check only.
//
// Cached "valid" responses survive 7 days of connectivity loss before
// being treated as stale and the license is downgraded to offline-only.
package license

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// OnlineClient phones home to the license server to fetch a
// revocation status. Configure OnlineValidationURL on each customer's
// license file. The URL is typically customer-hosted or Personel-
// hosted depending on the deployment agreement.
type OnlineClient struct {
	URL       string
	HTTPClient *http.Client
	log       *slog.Logger

	mu         sync.RWMutex
	lastStatus OnlineStatus
	lastCheck  time.Time
}

// OnlineStatus is what the license server returns.
type OnlineStatus struct {
	Valid      bool      `json:"valid"`
	Reason     string    `json:"reason,omitempty"`
	ValidUntil time.Time `json:"valid_until,omitempty"`
}

// StaleAfter is how long a cached "valid" response is accepted
// before the license is downgraded.
const StaleAfter = 7 * 24 * time.Hour

// NewOnlineClient constructs a phone-home client. Pass an empty URL
// to disable online validation entirely (offline-only mode).
func NewOnlineClient(url string, log *slog.Logger) *OnlineClient {
	if log == nil {
		log = slog.Default()
	}
	return &OnlineClient{
		URL: url,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		log: log,
	}
}

// Heartbeat is the outbound payload. Minimal by design.
type Heartbeat struct {
	CustomerID       string `json:"customer_id"`
	EndpointCount    int    `json:"endpoint_count"`
	Version          string `json:"version"`
	LicenseFingerprint string `json:"license_fingerprint,omitempty"`
}

// Validate sends a heartbeat and returns the server's judgment.
// Returns (lastKnown, true, nil) when offline and cached response is
// still fresh (within StaleAfter). Returns (_, false, err) when no
// valid cached response exists and network call failed.
func (c *OnlineClient) Validate(ctx context.Context, hb Heartbeat) (OnlineStatus, bool, error) {
	if c.URL == "" {
		// Online validation disabled — return a synthetic "valid" that
		// never goes stale.
		return OnlineStatus{Valid: true, Reason: "online_disabled"}, true, nil
	}

	body, err := json.Marshal(hb)
	if err != nil {
		return OnlineStatus{}, false, fmt.Errorf("license: marshal heartbeat: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return c.fallbackCache("request_error")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "personel-api-license/1.0")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		c.log.Warn("license phone-home failed; using cache",
			slog.String("err", err.Error()))
		return c.fallbackCache("network_error")
	}
	defer func() { _ = resp.Body.Close() }()

	rawResp, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	if err != nil {
		return c.fallbackCache("read_error")
	}
	if resp.StatusCode >= 500 {
		c.log.Warn("license server returned 5xx; using cache",
			slog.Int("status", resp.StatusCode))
		return c.fallbackCache("server_error")
	}
	if resp.StatusCode >= 400 {
		// 4xx is authoritative — server tells us we're invalid.
		return OnlineStatus{Valid: false, Reason: fmt.Sprintf("http_%d", resp.StatusCode)}, false, nil
	}

	var status OnlineStatus
	if err := json.Unmarshal(rawResp, &status); err != nil {
		return c.fallbackCache("parse_error")
	}

	c.mu.Lock()
	c.lastStatus = status
	c.lastCheck = time.Now()
	c.mu.Unlock()

	return status, true, nil
}

// fallbackCache returns the last-known status if still fresh, or an
// error if the cache is stale/empty.
func (c *OnlineClient) fallbackCache(reason string) (OnlineStatus, bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.lastCheck.IsZero() {
		return OnlineStatus{}, false, errors.New("license: no cached response available")
	}
	if time.Since(c.lastCheck) > StaleAfter {
		c.log.Error("license cache is stale; downgrading",
			slog.Duration("age", time.Since(c.lastCheck)),
			slog.String("reason", reason))
		return c.lastStatus, false, fmt.Errorf("license: cached response is stale (%s)", reason)
	}
	return c.lastStatus, true, nil
}

// LastCheck returns the timestamp of the most recent successful
// phone-home, or the zero time if none has occurred.
func (c *OnlineClient) LastCheck() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastCheck
}
