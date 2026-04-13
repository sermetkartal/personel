// Package search provides tenant-isolated OpenSearch full-text queries over
// the audit log and the ClickHouse-mirrored events index. Consumed by the
// admin console audit search UI (Faz 9 item #92) and later investigator
// tooling.
//
// Security contract:
//
//   - Every query AND-joins a server-side `tenant_id` term clause derived
//     from the verified principal. Client-supplied tenant_id in the request
//     body or URL is IGNORED — the query builder only reads from the typed
//     principal. This is a KVKK m.6 + SOC 2 CC6.1 non-negotiable.
//   - No endpoint may return keystroke content. The events handler redacts
//     `payload.content` server-side before serialising. If a new event kind
//     is added that contains sensitive free-text, update sanitiseEventHit.
//   - All search traffic is subject to the API-level OIDC middleware and
//     role gates declared in apps/api/internal/httpserver/server.go.
package search

import (
	"encoding/json"
	"errors"
	"time"
)

// AuditQuery is the parsed input to a /v1/search/audit request.
//
// Fields are intentionally narrow: only keyword/text search plus a small
// set of server-validated filters. Anything beyond this shape is rejected
// by the service's validate() method — notably any attempt to pass
// tenant_id via the query string is silently dropped before the query
// builder runs.
type AuditQuery struct {
	Q        string    // free-text terms, max 200 chars
	From     time.Time // inclusive lower bound, defaults to 7d ago
	To       time.Time // inclusive upper bound, defaults to now
	Action   string    // optional audit.Action allowlisted value
	ActorID  string    // optional exact match on the acting user UUID
	Page     int       // 1-indexed, 1..1000
	PageSize int       // 10..100, default 25
}

// AuditResult is the response envelope for an audit search call.
type AuditResult struct {
	Hits  []AuditHit `json:"hits"`
	Total int64      `json:"total"`
	Took  int        `json:"took_ms"`
	Page  int        `json:"page"`
	Size  int        `json:"page_size"`
}

// AuditHit is a single audit_log row projected from OpenSearch. The shape
// deliberately mirrors the Postgres audit_log schema so the console search
// UI can reuse the existing row component used by the linear audit list.
type AuditHit struct {
	ID        string          `json:"id"`
	Timestamp time.Time       `json:"timestamp"`
	Action    string          `json:"action"`
	ActorID   string          `json:"actor_id"`
	ActorIP   string          `json:"actor_ip,omitempty"`
	ActorUA   string          `json:"actor_ua,omitempty"`
	Target    string          `json:"target"`
	Payload   json.RawMessage `json:"payload"`
}

// EventQuery is the parsed input to a /v1/search/events request. Events
// are the ClickHouse-mirrored fleet telemetry stream (process.* file.*
// network.* etc). Keystroke content is NEVER in this index — ADR 0013
// + the gateway's SensitivityGuard stage strip it upstream — but the
// handler double-checks anyway via sanitiseEventHit.
type EventQuery struct {
	Q           string
	From        time.Time
	To          time.Time
	EventKind   string // optional exact match on the taxonomy enum
	EndpointID  string // optional exact match
	UserID      string // optional exact match
	ProcessName string // optional exact match
	Page        int
	PageSize    int
}

// EventResult is the response envelope for an events search call.
type EventResult struct {
	Hits  []EventHit `json:"hits"`
	Total int64      `json:"total"`
	Took  int        `json:"took_ms"`
	Page  int        `json:"page"`
	Size  int        `json:"page_size"`
}

// EventHit is a single row projected from the events-{tenant}-{YYYY-MM}
// OpenSearch index. Payload is the enricher-emitted JSON minus any
// `content` field (stripped by sanitiseEventHit before send).
type EventHit struct {
	ID          string          `json:"id"`
	Timestamp   time.Time       `json:"timestamp"`
	EventKind   string          `json:"event_kind"`
	EndpointID  string          `json:"endpoint_id"`
	UserID      string          `json:"user_id,omitempty"`
	ProcessName string          `json:"process_name,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

// ErrSearchUnavailable signals that the OpenSearch client is not
// connected (typically: backing cluster is down, URL misconfigured, or
// the api.yaml has opensearch.enabled=false). Handlers translate this
// to 503 Service Unavailable. The rest of the API remains functional.
var ErrSearchUnavailable = errors.New("search: opensearch unavailable")

// ErrValidation is returned by the service layer when the parsed query
// fails any of the server-side bounds checks. Handlers translate this
// to 400 Bad Request.
var ErrValidation = errors.New("search: validation failed")
