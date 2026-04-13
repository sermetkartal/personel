// Package search — OpenSearch REST client.
//
// Design note — stdlib HTTP vs opensearch-go SDK:
//
// We deliberately hit the OpenSearch REST API via net/http + encoding/json
// rather than pulling in github.com/opensearch-project/opensearch-go. The
// query surface needed for the audit + events search API is a single
// `POST /{index}/_search` with a JSON body, which is trivial in stdlib.
// Avoiding the SDK keeps the dependency graph flat, the binary small,
// and — critically — means this package adds ZERO new modules to go.sum,
// so the build is guaranteed reproducible without a network-enabled
// `go mod tidy` step at integration time. The SDK can be swapped in
// later if we need bulk indexing, streaming scroll, or the point-in-time
// API; the Service interface above is shaped so that swap is local.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config holds the connection parameters for a single OpenSearch cluster.
// In production this maps 1:1 to config.OpenSearchConfig.
type Config struct {
	// Addr is the base URL of the OpenSearch REST endpoint.
	// Example: "http://opensearch:9200".
	Addr string
	// Username + Password: optional HTTP basic auth. Empty in dev mode
	// (vm3 runs OpenSearch 2.x with the security plugin disabled); set
	// in prod via Vault-sourced env vars.
	Username string
	Password string
	// Timeout is the per-request HTTP timeout. Default 10s if zero.
	Timeout time.Duration
}

// Client is the narrow wrapper over the REST surface used by the search
// service. It intentionally exposes only the two methods the service
// layer needs so the fake implementation in the unit tests is tiny.
type Client struct {
	http    *http.Client
	addr    string
	user    string
	pass    string
	log     *slog.Logger
}

// queryClient is the internal interface Service depends on. Real Client
// and the in-memory fake in service_test.go both satisfy it. Unexported
// on purpose: no package outside search/ should be talking to OpenSearch
// through this type.
type queryClient interface {
	SearchAudit(ctx context.Context, tenantID string, q AuditQuery) (*AuditResult, error)
	SearchEvents(ctx context.Context, tenantID string, q EventQuery) (*EventResult, error)
}

// NewClient initialises a Client and pings the cluster so misconfiguration
// fails fast at process boot. If the cluster is unreachable this function
// returns (nil, err) — callers in cmd/api/main.go log at WARN and pass nil
// into NewService so that /v1/search/* returns 503 until the cluster is
// back, rather than killing the whole API process.
func NewClient(ctx context.Context, cfg Config, log *slog.Logger) (*Client, error) {
	if cfg.Addr == "" {
		return nil, fmt.Errorf("search: empty addr")
	}
	if _, err := url.Parse(cfg.Addr); err != nil {
		return nil, fmt.Errorf("search: parse addr %q: %w", cfg.Addr, err)
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	c := &Client{
		http:    &http.Client{Timeout: timeout},
		addr:    strings.TrimRight(cfg.Addr, "/"),
		user:    cfg.Username,
		pass:    cfg.Password,
		log:     log,
	}
	if err := c.ping(ctx); err != nil {
		return nil, err
	}
	log.Info("search: opensearch client ready", slog.String("addr", c.addr))
	return c, nil
}

// ping issues a GET / and expects a 200. Any non-200 is surfaced as an
// error so the caller can decide whether to degrade gracefully.
func (c *Client) ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.addr+"/", nil)
	if err != nil {
		return fmt.Errorf("search: ping build: %w", err)
	}
	c.setAuth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("search: ping do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("search: ping status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) setAuth(req *http.Request) {
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
}

// SearchAudit is called by Service.SearchAudit after input validation.
// The tenantID argument is authoritative and hardcoded into the query
// body — never taken from the user.
func (c *Client) SearchAudit(ctx context.Context, tenantID string, q AuditQuery) (*AuditResult, error) {
	if c == nil {
		return nil, ErrSearchUnavailable
	}
	filters := []map[string]any{
		{"range": map[string]any{"timestamp": map[string]any{
			"gte": q.From.UTC().Format(time.RFC3339),
			"lte": q.To.UTC().Format(time.RFC3339),
		}}},
	}
	if q.Action != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"action": q.Action}})
	}
	if q.ActorID != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"actor_id": q.ActorID}})
	}
	body := buildQuery(tenantID, q.Q, filters, []string{"action", "target", "actor_ua"}, q.Page, q.PageSize, "timestamp")

	raw, err := c.doSearch(ctx, auditIndexPattern(tenantID), body)
	if err != nil {
		return nil, err
	}
	return parseAuditResponse(raw, q.Page, q.PageSize)
}

// SearchEvents queries the events-{tenant}-* index. Same tenant-injection
// rules apply.
func (c *Client) SearchEvents(ctx context.Context, tenantID string, q EventQuery) (*EventResult, error) {
	if c == nil {
		return nil, ErrSearchUnavailable
	}
	filters := []map[string]any{
		{"range": map[string]any{"timestamp": map[string]any{
			"gte": q.From.UTC().Format(time.RFC3339),
			"lte": q.To.UTC().Format(time.RFC3339),
		}}},
	}
	if q.EventKind != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"event_kind": q.EventKind}})
	}
	if q.EndpointID != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"endpoint_id": q.EndpointID}})
	}
	if q.UserID != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"user_id": q.UserID}})
	}
	if q.ProcessName != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"process_name": q.ProcessName}})
	}
	body := buildQuery(tenantID, q.Q, filters,
		[]string{"event_kind", "process_name", "window_title", "file_path", "url"},
		q.Page, q.PageSize, "timestamp")

	raw, err := c.doSearch(ctx, eventsIndexPattern(tenantID), body)
	if err != nil {
		return nil, err
	}
	return parseEventsResponse(raw, q.Page, q.PageSize)
}

// buildQuery constructs the OpenSearch request body.
//
// The critical line is the leading `term: tenant_id`. Every caller in
// this package passes tenantID from the verified Principal, and the
// filters argument can never contain a tenant_id clause because nothing
// in this package builds one from untrusted input. A unit test in
// service_test.go exercises the bypass path and asserts the injected
// clause wins.
func buildQuery(
	tenantID string,
	userQuery string,
	extraFilters []map[string]any,
	qFields []string,
	page, pageSize int,
	sortField string,
) map[string]any {
	must := []map[string]any{}
	if strings.TrimSpace(userQuery) != "" {
		must = append(must, map[string]any{
			"multi_match": map[string]any{
				"query":  userQuery,
				"fields": qFields,
				"type":   "best_fields",
				// `and` reduces false positives for audit search where
				// operators tend to type specific terms like user names
				// or action verbs and expect all of them to match.
				"operator": "and",
			},
		})
	} else {
		// Empty query matches everything in the tenant+time slice.
		must = append(must, map[string]any{"match_all": map[string]any{}})
	}

	// tenant_id filter is ALWAYS first and ALWAYS server-supplied.
	filter := []map[string]any{
		{"term": map[string]any{"tenant_id": tenantID}},
	}
	filter = append(filter, extraFilters...)

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 25
	}
	from := (page - 1) * pageSize

	return map[string]any{
		"from": from,
		"size": pageSize,
		"sort": []map[string]any{
			{sortField: map[string]any{"order": "desc"}},
		},
		"query": map[string]any{
			"bool": map[string]any{
				"must":   must,
				"filter": filter,
			},
		},
		"track_total_hits": true,
	}
}

func (c *Client) doSearch(ctx context.Context, index string, body map[string]any) ([]byte, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("search: marshal body: %w", err)
	}
	u := fmt.Sprintf("%s/%s/_search", c.addr, url.PathEscape(index))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("search: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		c.log.Warn("search: request failed", slog.String("index", index), slog.Any("error", err))
		return nil, ErrSearchUnavailable
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("search: read body: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return data, nil
	case http.StatusNotFound:
		// Index pattern matched zero indices — legal empty result,
		// not a failure. Observed during pilot bring-up before the
		// gateway/enricher starts indexing.
		return []byte(`{"hits":{"total":{"value":0},"hits":[]}}`), nil
	case http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return nil, ErrSearchUnavailable
	default:
		return nil, fmt.Errorf("search: status %d: %s", resp.StatusCode, snippet(data))
	}
}

// snippet trims long error bodies to a digestible preview for logs.
func snippet(b []byte) string {
	const max = 256
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}

// auditIndexPattern returns the OpenSearch index pattern covering every
// month in the audit-{tenant}-{YYYY-MM} family. OpenSearch accepts
// wildcards in the index portion of the URL, so we let the cluster
// resolve the list at query time.
func auditIndexPattern(tenantID string) string {
	return fmt.Sprintf("audit-%s-*", tenantID)
}

func eventsIndexPattern(tenantID string) string {
	return fmt.Sprintf("events-%s-*", tenantID)
}

// --- Response parsing ------------------------------------------------------
//
// OpenSearch returns:
//   { "took": N, "hits": { "total": { "value": N }, "hits": [ { "_id", "_source": {...} } ] } }
//
// We decode once into the generic envelope, then project into our typed
// Hit structs.

type osEnvelope struct {
	Took int `json:"took"`
	Hits struct {
		Total struct {
			Value int64 `json:"value"`
		} `json:"total"`
		Hits []struct {
			ID     string          `json:"_id"`
			Source json.RawMessage `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

func parseAuditResponse(raw []byte, page, size int) (*AuditResult, error) {
	var env osEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("search: parse audit: %w", err)
	}
	out := &AuditResult{
		Hits:  make([]AuditHit, 0, len(env.Hits.Hits)),
		Total: env.Hits.Total.Value,
		Took:  env.Took,
		Page:  page,
		Size:  size,
	}
	for _, h := range env.Hits.Hits {
		var src struct {
			Timestamp time.Time       `json:"timestamp"`
			Action    string          `json:"action"`
			ActorID   string          `json:"actor_id"`
			ActorIP   string          `json:"actor_ip"`
			ActorUA   string          `json:"actor_ua"`
			Target    string          `json:"target"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(h.Source, &src); err != nil {
			continue
		}
		out.Hits = append(out.Hits, AuditHit{
			ID:        h.ID,
			Timestamp: src.Timestamp,
			Action:    src.Action,
			ActorID:   src.ActorID,
			ActorIP:   src.ActorIP,
			ActorUA:   src.ActorUA,
			Target:    src.Target,
			Payload:   src.Payload,
		})
	}
	return out, nil
}

func parseEventsResponse(raw []byte, page, size int) (*EventResult, error) {
	var env osEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("search: parse events: %w", err)
	}
	out := &EventResult{
		Hits:  make([]EventHit, 0, len(env.Hits.Hits)),
		Total: env.Hits.Total.Value,
		Took:  env.Took,
		Page:  page,
		Size:  size,
	}
	for _, h := range env.Hits.Hits {
		var src struct {
			Timestamp   time.Time       `json:"timestamp"`
			EventKind   string          `json:"event_kind"`
			EndpointID  string          `json:"endpoint_id"`
			UserID      string          `json:"user_id"`
			ProcessName string          `json:"process_name"`
			Payload     json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(h.Source, &src); err != nil {
			continue
		}
		hit := EventHit{
			ID:          h.ID,
			Timestamp:   src.Timestamp,
			EventKind:   src.EventKind,
			EndpointID:  src.EndpointID,
			UserID:      src.UserID,
			ProcessName: src.ProcessName,
			Payload:     src.Payload,
		}
		sanitiseEventHit(&hit)
		out.Hits = append(out.Hits, hit)
	}
	return out, nil
}

// sanitiseEventHit enforces the ADR 0013 invariant that NO admin-facing
// read API may return raw keystroke content. The enricher is already
// responsible for never indexing it, but we defence-in-depth here in
// case a future enricher regression or misconfigured index mapping
// leaks the field.
//
// If payload is a JSON object and contains a `content` key, the key is
// replaced with the literal string "[redacted]" before the hit is
// serialised to the client.
func sanitiseEventHit(hit *EventHit) {
	if len(hit.Payload) == 0 {
		return
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(hit.Payload, &m); err != nil {
		return
	}
	changed := false
	if _, ok := m["content"]; ok {
		m["content"] = json.RawMessage(`"[redacted]"`)
		changed = true
	}
	if _, ok := m["keystroke_content"]; ok {
		m["keystroke_content"] = json.RawMessage(`"[redacted]"`)
		changed = true
	}
	if !changed {
		return
	}
	if b, err := json.Marshal(m); err == nil {
		hit.Payload = b
	}
}
