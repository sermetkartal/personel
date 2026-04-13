// Package pipeline exposes admin operations on the NATS event
// pipeline: dead-letter queue inspection (Faz 7 #74) and replay of
// failed / historical events (Faz 7 #75).
//
// The package has NO dependency on the gateway Go module — DLQMessage
// is re-declared here with the same JSON shape so the API does not
// need a cross-module import. The JSON field tags MUST stay in lockstep
// with apps/gateway/internal/enricher/dlq.go::DLQMessage; a CI test
// round-trips a sample payload through both definitions.
//
// Security model:
//   - All handlers require a verified Principal.
//   - DLQ list + replay are gated to admin / dpo / investigator via
//     the chi Router. Tenant isolation is enforced INSIDE the service
//     layer: every DLQ message is filtered by principal.TenantID unless
//     the caller passes ?all_tenants=true and holds the DPO role.
//   - Every replay emits an audit entry BEFORE publishing.
//   - dry_run=true never publishes — returns a projected count.
package pipeline

import (
	"time"
)

// DLQMessage mirrors apps/gateway/internal/enricher/dlq.go::DLQMessage.
// Keep in lockstep — the JSON shape is the wire contract between the
// gateway and this API package.
type DLQMessage struct {
	OriginalSubject string            `json:"original_subject"`
	OriginalHeaders map[string]string `json:"original_headers"`
	OriginalPayload []byte            `json:"original_payload"`
	ErrorKind       string            `json:"error_kind"`
	ErrorMessage    string            `json:"error_message"`
	FailedAt        time.Time         `json:"failed_at"`
	RetryCount      int               `json:"retry_count"`
	TenantID        string            `json:"tenant_id,omitempty"`
	BatchID         uint64            `json:"batch_id,omitempty"`

	// StreamSequence is the JetStream sequence number of the DLQ entry
	// itself, used as the opaque "message id" for replay. Populated by
	// the service layer when reading — NOT serialised back from gateway
	// publishes (it's assigned by the server).
	StreamSequence uint64 `json:"stream_sequence,omitempty"`
}

// ListParams controls GET /v1/pipeline/dlq.
type ListParams struct {
	// TenantID filters to a single tenant. When empty, the service
	// infers it from the principal. DPO + ?all_tenants=true widens.
	TenantID string

	// AllTenants, when true, disables tenant filtering. Requires the
	// principal to hold the DPO role. The handler enforces this.
	AllTenants bool

	// From and To are inclusive wall-clock bounds on DLQMessage.FailedAt.
	// Zero = unbounded on that side.
	From time.Time
	To   time.Time

	// ErrorKind filters on the string enum (DLQKindDecode etc). Empty
	// means no filter.
	ErrorKind string

	// PageSize caps the returned slice length. 1..500 (default 100).
	PageSize int

	// PageToken is the opaque cursor — currently the JetStream
	// sequence number to start AFTER. Empty = start from oldest.
	PageToken string
}

// ListResult is the response shape returned by GET /v1/pipeline/dlq.
type ListResult struct {
	Messages      []*DLQMessage `json:"messages"`
	NextPageToken string        `json:"next_page_token,omitempty"`
	TotalScanned  int           `json:"total_scanned"`
}

// ReplaySource chooses which data source to replay from.
type ReplaySource string

const (
	// ReplaySourceDLQ fetches a specific DLQ entry by its JetStream
	// sequence number and re-publishes the original payload + headers
	// to events_raw.
	ReplaySourceDLQ ReplaySource = "dlq"

	// ReplaySourceClickHouse queries events_raw (the CH table) for
	// rows matching CHFilter, reconstructs a minimal Event per row,
	// and publishes each one back into the events_raw NATS stream.
	// This mode is dry_run-first by policy.
	ReplaySourceClickHouse ReplaySource = "clickhouse"
)

// CHReplayFilter is the time-range + optional dimensional filter for
// ClickHouse-source replays.
type CHReplayFilter struct {
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
	TenantID  string    `json:"tenant_id,omitempty"`
	EventKind string    `json:"event_kind,omitempty"`
}

// ReplayRequest is the POST /v1/pipeline/replay body.
type ReplayRequest struct {
	Source         ReplaySource    `json:"source"`
	DLQMessageID   string          `json:"dlq_message_id,omitempty"`
	CHFilter       *CHReplayFilter `json:"ch_filter,omitempty"`
	TargetStream   string          `json:"target_stream,omitempty"`
	DryRun         bool            `json:"dry_run"`
	DeleteOnSuccess bool           `json:"delete_on_success,omitempty"`

	// AllTenants — as in ListParams, DPO-only + narrow escape hatch.
	AllTenants bool `json:"all_tenants,omitempty"`
}

// ReplayResult is the response body for POST /v1/pipeline/replay.
type ReplayResult struct {
	// Projected is the count of events that would be / were replayed.
	// Populated for both dry_run and real calls.
	Projected int `json:"projected"`

	// Published is the count of events actually published to NATS.
	// Zero when DryRun=true.
	Published int `json:"published"`

	// Source echoes the request.
	Source ReplaySource `json:"source"`

	// DryRun echoes the request.
	DryRun bool `json:"dry_run"`

	// Notes carries operator-facing remarks (e.g. "CH replay is
	// dry_run-only in Phase 1").
	Notes []string `json:"notes,omitempty"`
}

// validate performs request-level invariant checks. Service-layer
// additional checks live in service.go.
func (r *ReplayRequest) validate() error {
	switch r.Source {
	case ReplaySourceDLQ:
		if r.DLQMessageID == "" {
			return errRequired("dlq_message_id")
		}
	case ReplaySourceClickHouse:
		if r.CHFilter == nil {
			return errRequired("ch_filter")
		}
		if r.CHFilter.From.IsZero() || r.CHFilter.To.IsZero() {
			return errRequired("ch_filter.from, ch_filter.to")
		}
		if r.CHFilter.To.Before(r.CHFilter.From) {
			return errInvalid("ch_filter", "to must be >= from")
		}
	default:
		return errInvalid("source", "must be 'dlq' or 'clickhouse'")
	}
	if r.TargetStream == "" {
		r.TargetStream = "events_raw"
	}
	if r.TargetStream != "events_raw" {
		return errInvalid("target_stream", "only 'events_raw' is supported in Phase 1")
	}
	return nil
}

// --- tiny internal error helpers ---

type validationErr struct {
	Field   string
	Message string
}

func (v *validationErr) Error() string { return v.Field + ": " + v.Message }

func errRequired(field string) error {
	return &validationErr{Field: field, Message: "required"}
}

func errInvalid(field, msg string) error {
	return &validationErr{Field: field, Message: msg}
}

