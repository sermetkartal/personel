package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
)

// DLQStreamName is the JetStream stream the enricher parks failed
// messages into. MUST match apps/gateway/internal/enricher/dlq.go.
const DLQStreamName = "events_dlq"

// DLQSubjectFilter is the subject prefix used to scope DLQ pull
// consumers. Matches the enricher publish path.
const DLQSubjectFilter = "events.dlq.>"

// defaultPageSize is used when ListParams.PageSize is zero.
const defaultPageSize = 100

// maxPageSize is the upper clamp for ListParams.PageSize.
const maxPageSize = 500

// maxReplayCHEvents is the hard cap on rows reconstructed from
// ClickHouse in a single replay call (task spec).
const maxReplayCHEvents = 10000

// --- dependencies (narrow interfaces, mockable in tests) -------------------

// DLQReader reads DLQMessages from a JetStream stream. The production
// implementation wraps a pull consumer.
type DLQReader interface {
	// List returns DLQ entries matching params. Implementations
	// must respect params.PageSize and PageToken (opaque sequence).
	List(ctx context.Context, params ListParams) (*ListResult, error)

	// GetByID fetches exactly one DLQ message by its JetStream
	// sequence number (passed as a string for URL-friendliness).
	// Returns ErrDLQNotFound if missing.
	GetByID(ctx context.Context, id string) (*DLQMessage, error)

	// Delete removes the entry from the stream. Used by replay
	// when DeleteOnSuccess is set. Best-effort: an error is
	// logged by the caller but the replay is NOT rolled back.
	Delete(ctx context.Context, id string) error
}

// EventPublisher publishes a raw payload + headers to a subject. The
// production implementation wraps an nats.JetStreamContext.
type EventPublisher interface {
	// PublishRaw publishes payload to subject. The headers map is
	// attached via NATS headers; implementations must preserve the
	// ordering used in DLQMessage.OriginalHeaders.
	PublishRaw(ctx context.Context, subject string, headers map[string]string, payload []byte) error
}

// CHEventSource is the narrow query interface over the CH events_raw
// table. In Phase 1 this is projection-only: Count returns the number
// of rows that match a filter. The replay path does NOT actually
// reconstruct proto payloads from CH rows yet — that's a Phase 2
// marker so operators can still run dry_run calls for capacity
// planning.
type CHEventSource interface {
	Count(ctx context.Context, f CHReplayFilter) (int, error)
}

// Recorder is the audit recorder interface the service uses; in
// production it's *audit.Recorder. Declared as an interface for tests.
type Recorder interface {
	Append(ctx context.Context, e audit.Entry) (int64, error)
}

// --- errors ----------------------------------------------------------------

// ErrDLQNotFound is returned when GetByID cannot find the requested entry.
var ErrDLQNotFound = errors.New("pipeline: DLQ entry not found")

// ErrTenantIsolation is returned when a caller tries to access a DLQ
// entry that belongs to a different tenant and does not hold DPO +
// AllTenants.
var ErrTenantIsolation = errors.New("pipeline: tenant isolation violation")

// ErrForbiddenAllTenants is returned when a non-DPO caller passes
// ?all_tenants=true.
var ErrForbiddenAllTenants = errors.New("pipeline: all_tenants requires DPO role")

// --- service ---------------------------------------------------------------

// Service is the pipeline admin orchestrator.
type Service struct {
	reader      DLQReader
	publisher   EventPublisher
	chEvents    CHEventSource // may be nil — degrades CH replay to "unavailable"
	recorder    Recorder
	log         *slog.Logger
}

// NewService constructs a Service with the given backends. recorder is
// required; the others may be nil in specific degraded modes (callers
// get a clear error at request time rather than a nil panic).
func NewService(reader DLQReader, pub EventPublisher, ch CHEventSource, rec Recorder, log *slog.Logger) *Service {
	return &Service{
		reader:    reader,
		publisher: pub,
		chEvents:  ch,
		recorder:  rec,
		log:       log,
	}
}

// ListDLQ fetches DLQ entries with tenant isolation enforced.
func (s *Service) ListDLQ(ctx context.Context, p *auth.Principal, params ListParams) (*ListResult, error) {
	if s.reader == nil {
		return nil, errors.New("pipeline: DLQ reader unavailable")
	}
	if p == nil {
		return nil, ErrTenantIsolation
	}

	// Tenant isolation.
	if params.AllTenants {
		if !auth.HasRole(p, auth.RoleDPO) {
			return nil, ErrForbiddenAllTenants
		}
	} else {
		// Always pin to the principal's tenant — do NOT trust any
		// client-supplied value.
		params.TenantID = p.TenantID
	}

	if params.PageSize <= 0 {
		params.PageSize = defaultPageSize
	}
	if params.PageSize > maxPageSize {
		params.PageSize = maxPageSize
	}

	return s.reader.List(ctx, params)
}

// Replay dispatches to the DLQ or ClickHouse replay path.
func (s *Service) Replay(ctx context.Context, p *auth.Principal, req ReplayRequest) (*ReplayResult, error) {
	if p == nil {
		return nil, ErrTenantIsolation
	}
	if err := req.validate(); err != nil {
		return nil, err
	}

	// all_tenants requires DPO.
	if req.AllTenants && !auth.HasRole(p, auth.RoleDPO) {
		return nil, ErrForbiddenAllTenants
	}

	// --- Audit entry BEFORE any side effect. ---
	if err := s.auditReplay(ctx, p, req); err != nil {
		return nil, fmt.Errorf("pipeline: audit: %w", err)
	}

	switch req.Source {
	case ReplaySourceDLQ:
		return s.replayDLQ(ctx, p, req)
	case ReplaySourceClickHouse:
		return s.replayClickHouse(ctx, p, req)
	default:
		return nil, errInvalid("source", "must be 'dlq' or 'clickhouse'")
	}
}

// replayDLQ fetches one DLQ entry and re-publishes it (unless dry_run).
func (s *Service) replayDLQ(ctx context.Context, p *auth.Principal, req ReplayRequest) (*ReplayResult, error) {
	if s.reader == nil {
		return nil, errors.New("pipeline: DLQ reader unavailable")
	}

	m, err := s.reader.GetByID(ctx, req.DLQMessageID)
	if err != nil {
		return nil, err
	}

	// Tenant isolation.
	if !req.AllTenants && m.TenantID != p.TenantID {
		return nil, ErrTenantIsolation
	}

	result := &ReplayResult{
		Source:    ReplaySourceDLQ,
		DryRun:    req.DryRun,
		Projected: 1,
	}

	if req.DryRun {
		result.Notes = append(result.Notes, "dry_run: no publish performed")
		return result, nil
	}

	if s.publisher == nil {
		return nil, errors.New("pipeline: event publisher unavailable")
	}

	// Re-publish. Preserve the original subject + headers so the
	// enricher dispatches through the same schema decoder.
	if err := s.publisher.PublishRaw(ctx, m.OriginalSubject, m.OriginalHeaders, m.OriginalPayload); err != nil {
		return nil, fmt.Errorf("pipeline: re-publish: %w", err)
	}
	result.Published = 1

	if req.DeleteOnSuccess {
		if err := s.reader.Delete(ctx, req.DLQMessageID); err != nil {
			s.log.WarnContext(ctx, "pipeline: DLQ delete after replay failed",
				slog.String("id", req.DLQMessageID),
				slog.String("error", err.Error()),
			)
			result.Notes = append(result.Notes, "warning: DLQ delete failed; entry remains in stream")
		} else {
			result.Notes = append(result.Notes, "DLQ entry deleted after successful replay")
		}
	}

	return result, nil
}

// replayClickHouse projects the count of matching rows. In Phase 1 this
// is dry_run-only: even if DryRun=false we do not actually reconstruct
// proto payloads from CH rows (lossy), we return the projected count
// plus a "not implemented" note so operators can still use the endpoint
// for capacity planning and the concurrent /v1/reports/ch work can land
// without breaking this endpoint.
func (s *Service) replayClickHouse(ctx context.Context, p *auth.Principal, req ReplayRequest) (*ReplayResult, error) {
	if s.chEvents == nil {
		return nil, errors.New("pipeline: ClickHouse replay unavailable (no CH client wired)")
	}
	f := *req.CHFilter

	// Tenant pinning.
	if !req.AllTenants {
		f.TenantID = p.TenantID
	}

	n, err := s.chEvents.Count(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("pipeline: CH count: %w", err)
	}
	if n > maxReplayCHEvents {
		n = maxReplayCHEvents
	}

	result := &ReplayResult{
		Source:    ReplaySourceClickHouse,
		DryRun:    true, // Phase 1 forces dry_run
		Projected: n,
	}
	result.Notes = append(result.Notes,
		"ClickHouse replay is projection-only in Phase 1",
		"Real re-publish requires proto reconstruction (Phase 2)")
	if !req.DryRun {
		result.Notes = append(result.Notes,
			fmt.Sprintf("requested dry_run=false was demoted: no publish performed (count=%d)", n))
	}
	return result, nil
}

// auditReplay writes an audit entry before the replay side effect.
func (s *Service) auditReplay(ctx context.Context, p *auth.Principal, req ReplayRequest) error {
	details := map[string]any{
		"source":     string(req.Source),
		"dry_run":    req.DryRun,
		"target":     req.TargetStream,
		"all_tenants": req.AllTenants,
	}
	target := "pipeline"
	switch req.Source {
	case ReplaySourceDLQ:
		details["dlq_message_id"] = req.DLQMessageID
		target = "pipeline:dlq:" + req.DLQMessageID
	case ReplaySourceClickHouse:
		if req.CHFilter != nil {
			details["ch_from"] = req.CHFilter.From.Format(time.RFC3339)
			details["ch_to"] = req.CHFilter.To.Format(time.RFC3339)
			details["ch_event_kind"] = req.CHFilter.EventKind
			details["ch_tenant_id"] = req.CHFilter.TenantID
		}
		target = "pipeline:clickhouse"
	}

	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: p.TenantID,
		Action:   audit.ActionPipelineReplay,
		Target:   target,
		Details:  details,
	})
	return err
}

// --- small helpers ---------------------------------------------------------

// ParseSeqToken parses a page token into a uint64 JetStream sequence.
// Empty / invalid tokens parse to 0 (start).
func ParseSeqToken(s string) uint64 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// FormatSeqToken is the inverse of ParseSeqToken.
func FormatSeqToken(n uint64) string {
	if n == 0 {
		return ""
	}
	return strconv.FormatUint(n, 10)
}

// decodeDLQBody unmarshals the JSON body of a DLQ JetStream message.
// Exposed for the production reader implementation.
func decodeDLQBody(raw []byte) (*DLQMessage, error) {
	var m DLQMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("pipeline: decode DLQ body: %w", err)
	}
	return &m, nil
}

// compile-time interface assertion: audit.Recorder satisfies Recorder.
var _ Recorder = (*audit.Recorder)(nil)
