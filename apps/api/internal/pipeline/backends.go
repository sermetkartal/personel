// backends.go — production adapters for DLQReader, EventPublisher,
// and CHEventSource. Lives in the pipeline package so tests stay able
// to exercise the service layer through pure in-memory fakes.
//
// NATS backend uses the legacy nats.go v1 API (gonats.JetStreamContext)
// so it shares the connection owned by apps/api/internal/nats.Publisher
// — we do NOT open a second NATS connection just to satisfy the new
// jetstream package surface.

package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	gonats "github.com/nats-io/nats.go"
)

// --- NATS publisher adapter ------------------------------------------------

// natsJS is the narrow subset of gonats.JetStreamContext that we need.
// Declared as an interface so in-process tests don't drag a real
// JetStream server. The method signatures match gonats.JetStreamContext
// exactly so a *natsClientImpl can be passed in directly.
type natsJS interface {
	PublishMsg(m *gonats.Msg, opts ...gonats.PubOpt) (*gonats.PubAck, error)
	PullSubscribe(subj, durable string, opts ...gonats.SubOpt) (*gonats.Subscription, error)
	DeleteMsg(name string, seq uint64, opts ...gonats.JSOpt) error
}

// NATSPublisher adapts a gonats.JetStreamContext to the EventPublisher
// interface used by the service layer.
type NATSPublisher struct {
	js  natsJS
	log *slog.Logger
}

// NewNATSPublisher wraps a JetStream context for the pipeline service.
func NewNATSPublisher(js natsJS, log *slog.Logger) *NATSPublisher {
	return &NATSPublisher{js: js, log: log}
}

// PublishRaw re-publishes payload to subject with the given headers.
func (n *NATSPublisher) PublishRaw(_ context.Context, subject string, headers map[string]string, payload []byte) error {
	if n == nil || n.js == nil {
		return errors.New("pipeline: nats: publisher not initialised")
	}

	msg := gonats.NewMsg(subject)
	msg.Data = payload
	for k, v := range headers {
		// Drop the retry-count header on replay so the enricher sees
		// a fresh attempt. Everything else (schema-version, agent
		// identity) must be preserved verbatim.
		if k == "nats-retry-count" || k == "Nats-Retry-Count" {
			continue
		}
		msg.Header.Set(k, v)
	}
	// Mark replays so enricher logs + operators can trace them.
	msg.Header.Set("x-pipeline-replay", time.Now().UTC().Format(time.RFC3339))

	_, err := n.js.PublishMsg(msg)
	if err != nil {
		return fmt.Errorf("pipeline: publish %s: %w", subject, err)
	}
	return nil
}

// --- NATS DLQ reader adapter -----------------------------------------------

// NATSDLQReader reads DLQMessage entries from the events_dlq stream
// using a pull consumer on the legacy gonats API.
//
// Design notes:
//   - We use an EPHEMERAL pull consumer per List call. The DLQ volume
//     is expected to be low (<10k / day) so cursor state is cheap to
//     reconstruct; a durable consumer would accumulate ACK state we
//     don't actually want.
//   - Tenant + error_kind + time filters are applied IN the reader
//     after fetch, because JetStream pull consumers can only filter
//     on subject. The subject carries tenant_id so the subject filter
//     narrows to a single tenant when TenantID is set.
type NATSDLQReader struct {
	js        natsJS
	log       *slog.Logger
	// maxScan is the upper bound on messages inspected per List call
	// to avoid pathological pulls. 5000 is comfortable for a healthy
	// DLQ; operators who need more can page.
	maxScan int
}

// NewNATSDLQReader constructs a pull-consumer-backed reader.
func NewNATSDLQReader(js natsJS, log *slog.Logger) *NATSDLQReader {
	return &NATSDLQReader{js: js, log: log, maxScan: 5000}
}

// List fetches matching DLQ entries via a short-lived pull consumer.
func (r *NATSDLQReader) List(ctx context.Context, params ListParams) (*ListResult, error) {
	if r == nil || r.js == nil {
		return nil, errors.New("pipeline: DLQ reader not initialised")
	}

	subject := DLQSubjectFilter
	if params.TenantID != "" {
		subject = "events.dlq." + sanitiseSubject(params.TenantID)
	}

	sub, err := r.js.PullSubscribe(subject, "",
		gonats.BindStream(DLQStreamName),
		gonats.DeliverAll(),
		gonats.AckExplicit(),
	)
	if err != nil {
		return nil, fmt.Errorf("pipeline: pull subscribe: %w", err)
	}
	defer func() {
		_ = sub.Drain()
	}()

	result := &ListResult{
		Messages: make([]*DLQMessage, 0, params.PageSize),
	}

	cursor := ParseSeqToken(params.PageToken)
	scanned := 0
	const fetchBatch = 100

	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	for len(result.Messages) < params.PageSize && scanned < r.maxScan {
		msgs, err := sub.Fetch(fetchBatch, gonats.Context(fetchCtx))
		if err != nil {
			// No more messages is not an error.
			if errors.Is(err, gonats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
				break
			}
			return nil, fmt.Errorf("pipeline: fetch: %w", err)
		}
		if len(msgs) == 0 {
			break
		}

		for _, m := range msgs {
			scanned++
			md, mdErr := m.Metadata()
			if mdErr != nil {
				_ = m.Ack()
				continue
			}
			if md.Sequence.Stream <= cursor {
				_ = m.Ack()
				continue
			}

			dm, derr := decodeDLQBody(m.Data)
			if derr != nil {
				r.log.WarnContext(ctx, "pipeline: skip unparseable DLQ entry",
					slog.Uint64("seq", md.Sequence.Stream),
					slog.String("error", derr.Error()),
				)
				_ = m.Ack()
				continue
			}
			dm.StreamSequence = md.Sequence.Stream

			// Post-fetch filtering.
			if !matchesListParams(dm, params) {
				_ = m.Ack()
				continue
			}

			result.Messages = append(result.Messages, dm)
			_ = m.Ack()

			if len(result.Messages) >= params.PageSize {
				// Set the next page token to the last included seq.
				result.NextPageToken = FormatSeqToken(md.Sequence.Stream)
				break
			}
		}
		if scanned >= r.maxScan {
			break
		}
	}

	result.TotalScanned = scanned
	if result.NextPageToken == "" && len(result.Messages) > 0 {
		result.NextPageToken = FormatSeqToken(result.Messages[len(result.Messages)-1].StreamSequence)
	}
	return result, nil
}

// GetByID fetches a single DLQ entry by JetStream sequence. Implemented
// as a scan-and-match up to maxScan; for the expected DLQ volume this
// is fine and it avoids pulling in the v2 jetstream package that owns
// the direct-get API.
func (r *NATSDLQReader) GetByID(ctx context.Context, id string) (*DLQMessage, error) {
	if r == nil || r.js == nil {
		return nil, errors.New("pipeline: DLQ reader not initialised")
	}
	target := ParseSeqToken(id)
	if target == 0 {
		return nil, ErrDLQNotFound
	}

	sub, err := r.js.PullSubscribe(DLQSubjectFilter, "",
		gonats.BindStream(DLQStreamName),
		gonats.DeliverAll(),
		gonats.AckExplicit(),
	)
	if err != nil {
		return nil, fmt.Errorf("pipeline: pull subscribe: %w", err)
	}
	defer func() {
		_ = sub.Drain()
	}()

	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	scanned := 0
	for scanned < r.maxScan {
		msgs, err := sub.Fetch(100, gonats.Context(fetchCtx))
		if err != nil {
			if errors.Is(err, gonats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
				break
			}
			return nil, fmt.Errorf("pipeline: fetch: %w", err)
		}
		if len(msgs) == 0 {
			break
		}
		for _, m := range msgs {
			scanned++
			md, mdErr := m.Metadata()
			_ = m.Ack()
			if mdErr != nil {
				continue
			}
			if md.Sequence.Stream != target {
				continue
			}
			dm, derr := decodeDLQBody(m.Data)
			if derr != nil {
				return nil, fmt.Errorf("pipeline: decode matched DLQ entry: %w", derr)
			}
			dm.StreamSequence = md.Sequence.Stream
			return dm, nil
		}
	}
	return nil, ErrDLQNotFound
}

// Delete removes a DLQ entry from the stream by sequence.
func (r *NATSDLQReader) Delete(_ context.Context, id string) error {
	if r == nil || r.js == nil {
		return errors.New("pipeline: DLQ reader not initialised")
	}
	seq := ParseSeqToken(id)
	if seq == 0 {
		return ErrDLQNotFound
	}
	return r.js.DeleteMsg(DLQStreamName, seq)
}

// matchesListParams checks if a DLQMessage satisfies the ListParams
// filters (tenant_id, error_kind, from, to). Called after the subject
// filter has already narrowed the candidates.
func matchesListParams(m *DLQMessage, p ListParams) bool {
	if p.ErrorKind != "" && m.ErrorKind != p.ErrorKind {
		return false
	}
	if !p.From.IsZero() && m.FailedAt.Before(p.From) {
		return false
	}
	if !p.To.IsZero() && m.FailedAt.After(p.To) {
		return false
	}
	if !p.AllTenants && p.TenantID != "" && m.TenantID != p.TenantID {
		return false
	}
	return true
}

// sanitiseSubject strips characters forbidden in NATS subject tokens.
// Duplicated from the gateway implementation — both sides of the wire
// must sanitise the same way. Tenant UUIDs are already safe; this
// belt-and-braces protects against operator input drift.
func sanitiseSubject(s string) string {
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
