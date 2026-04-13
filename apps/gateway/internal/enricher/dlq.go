// dlq.go — dead letter queue publisher (Faz 7 #74).
//
// When enrichment of a single NATS message fails irrecoverably (schema
// decode error, ClickHouse insert, MinIO store, unknown schema version)
// the original payload is wrapped in a DLQMessage and published to the
// events_dlq stream. The original JetStream message is then ACKed so
// JetStream stops redelivering it — infinite redelivery of a broken
// message is the worst failure mode (it saturates the consumer loop
// and starves healthy traffic).
//
// Retry accounting: the original batch is retried in-place up to
// MaxRetries (default 3). The retry count is read from the NATS header
// "nats-retry-count" — the gateway publisher increments it on every
// Nak-with-delay before finally DLQing. DLQ consumers can see the full
// retry history via DLQMessage.RetryCount.
//
// Replay path: the Admin API exposes GET /v1/pipeline/dlq for listing
// (reads directly from JetStream) and POST /v1/pipeline/replay for
// re-injecting messages back into events_raw. See apps/api/internal/pipeline.
package enricher

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	natslib "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// DLQ error kinds are small string enums so the Admin API can filter
// and humans can grep. Each kind maps to a point in the enricher pipeline.
const (
	// DLQKindDecode — message could not be parsed by any registered
	// schema decoder (ErrUnknownSchemaVersion or proto unmarshal error).
	DLQKindDecode = "decode"

	// DLQKindEnrich — enricher.Enrich() returned an error (Postgres
	// endpoint metadata lookup, tenant resolution, etc).
	DLQKindEnrich = "enrich"

	// DLQKindClickHouse — batcher.Add() returned an error (CH is
	// unreachable, column type mismatch, etc).
	DLQKindClickHouse = "clickhouse"

	// DLQKindMinIO — blob-reference event failed to store its
	// accompanying artifact to MinIO.
	DLQKindMinIO = "minio"

	// DLQKindRetryExhausted — the message was NAKed N times and we
	// gave up; it may or may not have a more specific underlying
	// kind recorded in ErrorMessage.
	DLQKindRetryExhausted = "retry_exhausted"
)

// Header name the gateway publisher writes on redeliveries. Read by
// the enricher and incremented (if present) on the outgoing DLQ message.
const headerRetryCount = "nats-retry-count"

// DLQStreamName is the canonical JetStream stream receiving DLQ messages.
const DLQStreamName = "events_dlq"

// DLQSubjectBase is the subject prefix. Full subjects include the
// tenant so pull consumers can filter.
const DLQSubjectBase = "events.dlq"

// DLQMessage is the JSON shape published to events_dlq. The Admin API
// /v1/pipeline/dlq endpoint returns the same struct to clients.
//
// IMPORTANT: OriginalPayload is the exact wire bytes (proto binary).
// The replay endpoint republishes these bytes verbatim so a bug-fix
// deploy can re-process the event through the now-correct pipeline.
type DLQMessage struct {
	// OriginalSubject is the subject the message was originally
	// published to, e.g. "events.raw.<tenant>.<event_type>".
	OriginalSubject string `json:"original_subject"`

	// OriginalHeaders is a flattened copy of the NATS headers (first
	// value per key). Includes schema-version so the replay pipeline
	// can re-dispatch the same decoder.
	OriginalHeaders map[string]string `json:"original_headers"`

	// OriginalPayload is the raw wire bytes (proto binary).
	OriginalPayload []byte `json:"original_payload"`

	// ErrorKind categorises the failure (DLQKind* constants above).
	ErrorKind string `json:"error_kind"`

	// ErrorMessage is the full stringified error at the failure point.
	ErrorMessage string `json:"error_message"`

	// FailedAt is the wall-clock time when the failure was recorded.
	FailedAt time.Time `json:"failed_at"`

	// RetryCount is the number of redeliveries observed before the
	// failure was declared permanent. 0 = first attempt failed
	// terminally; N = N NAK-with-delay retries preceded this DLQ.
	RetryCount int `json:"retry_count"`

	// TenantID is the tenant extracted from the subject or payload
	// meta (best-effort — empty if the decode failed before tenant
	// resolution).
	TenantID string `json:"tenant_id,omitempty"`

	// BatchID is the original EventBatch.batch_id if decode
	// succeeded, else 0.
	BatchID uint64 `json:"batch_id,omitempty"`
}

// jsPublisher is the narrow interface DLQPublisher depends on. Satisfied
// by jetstream.JetStream (production) and by fakes in tests. Keeping the
// dependency narrow means we don't need a 200-method mock.
type jsPublisher interface {
	Publish(ctx context.Context, subj string, data []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// DLQPublisher writes DLQMessages to the events_dlq JetStream stream.
// It is safe for concurrent use (JetStream.Publish is).
type DLQPublisher struct {
	js         jsPublisher
	log        *slog.Logger
	maxRetries int
}

// DLQPublisherConfig configures a new DLQPublisher.
type DLQPublisherConfig struct {
	// MaxRetries is the threshold beyond which the consumer gives up
	// retrying and DLQs the message. 0 → default 3.
	MaxRetries int
}

// NewDLQPublisher constructs a DLQPublisher. js must be a live
// JetStream context (or any value satisfying jsPublisher).
func NewDLQPublisher(js jsPublisher, cfg DLQPublisherConfig, log *slog.Logger) *DLQPublisher {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	return &DLQPublisher{
		js:         js,
		log:        log,
		maxRetries: cfg.MaxRetries,
	}
}

// MaxRetries exposes the configured retry threshold so the consumer
// loop can decide between NAK and DLQ.
func (d *DLQPublisher) MaxRetries() int { return d.maxRetries }

// Publish serialises m to JSON and publishes to events.dlq.<tenant>.
// If TenantID is empty the suffix is "unknown". Returns the JetStream
// sequence number on success.
func (d *DLQPublisher) Publish(ctx context.Context, m *DLQMessage) (uint64, error) {
	if m == nil {
		return 0, fmt.Errorf("dlq: nil message")
	}
	if m.FailedAt.IsZero() {
		m.FailedAt = time.Now().UTC()
	}
	if m.ErrorKind == "" {
		m.ErrorKind = "unknown"
	}

	body, err := json.Marshal(m)
	if err != nil {
		return 0, fmt.Errorf("dlq: marshal: %w", err)
	}

	tenant := m.TenantID
	if tenant == "" {
		tenant = "unknown"
	}
	subject := fmt.Sprintf("%s.%s", DLQSubjectBase, sanitiseSubjectToken(tenant))

	ack, err := d.js.Publish(ctx, subject, body)
	if err != nil {
		d.log.ErrorContext(ctx, "dlq: publish failed",
			slog.String("subject", subject),
			slog.String("error_kind", m.ErrorKind),
			slog.String("error", err.Error()),
		)
		return 0, fmt.Errorf("dlq: publish: %w", err)
	}

	d.log.WarnContext(ctx, "dlq: message parked",
		slog.String("subject", subject),
		slog.String("error_kind", m.ErrorKind),
		slog.String("tenant_id", m.TenantID),
		slog.Int("retry_count", m.RetryCount),
		slog.Uint64("seq", ack.Sequence),
	)
	return ack.Sequence, nil
}

// BuildDLQMessage is a helper that converts a failing jetstream.Msg and
// an error into a DLQMessage ready for Publish.
func BuildDLQMessage(msg jetstream.Msg, errKind, errMsg, tenantID string, batchID uint64) *DLQMessage {
	headers := flattenHeaders(msg.Headers())
	retry := 0
	if r, ok := headers[headerRetryCount]; ok {
		if n, err := strconv.Atoi(r); err == nil {
			retry = n
		}
	}
	return &DLQMessage{
		OriginalSubject: msg.Subject(),
		OriginalHeaders: headers,
		OriginalPayload: msg.Data(),
		ErrorKind:       errKind,
		ErrorMessage:    errMsg,
		FailedAt:        time.Now().UTC(),
		RetryCount:      retry,
		TenantID:        tenantID,
		BatchID:         batchID,
	}
}

// RetryCountOf reads the nats-retry-count header, returning 0 if absent.
func RetryCountOf(h natslib.Header) int {
	if h == nil {
		return 0
	}
	v := h.Get(headerRetryCount)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

// flattenHeaders converts NATS headers (map[string][]string) to a
// single-valued map[string]string. If a key has multiple values only
// the first is retained — this matches the DLQMessage contract and
// keeps JSON payloads small. Returns an empty (non-nil) map when
// headers is nil so downstream consumers don't have to nil-check.
func flattenHeaders(h natslib.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			out[k] = v[0]
		} else {
			out[k] = ""
		}
	}
	return out
}

// sanitiseSubjectToken strips characters forbidden in NATS subject
// tokens (space, star, greater-than, dot). The tenant UUID should
// already be safe but we belt-and-brace against garbage input.
func sanitiseSubjectToken(s string) string {
	if s == "" {
		return "unknown"
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case ' ', '.', '*', '>', '\t', '\r', '\n':
			out = append(out, '_')
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
