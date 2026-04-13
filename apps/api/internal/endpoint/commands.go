// Package endpoint — remote command issuance for deactivate / wipe / revoke.
//
// Faz 6 items #64 (single-endpoint deactivate/wipe) and #65 (bulk batch
// operations). Admins issue commands through the console; the API row-logs
// each command to endpoint_commands (state: pending → acknowledged →
// completed / failed / timeout), publishes a NATS message on
// endpoints.command.{tenant_id}.{endpoint_id}, and writes an audit entry.
//
// The gateway subscribes to those subjects, forwards the command over the
// agent bidi stream, and reports back via POST /v1/internal/commands/{id}/ack
// — that path is not authenticated by Keycloak; it relies on a shared-secret
// header so the in-cluster gateway can call the admin API without holding a
// user JWT.
//
// KVKK m.7 note: wipe = crypto-erase of DPAPI-sealed key material, not
// filesystem shredding. The agent zeroizes its sealed queue key and its
// DPAPI-sealed TLS identity, making every local encrypted blob unrecoverable.
// Wipe is refused if the endpoint is under an active legal hold — the DPO
// must release the hold before the crypto-erase can proceed, otherwise
// evidence integrity is lost.
package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
)

// CommandKind enumerates the remote command types the API can issue.
// The agent decodes the matching kind from the published payload and
// dispatches to its registered handler.
type CommandKind string

const (
	// CommandDeactivate tells the agent to stop all collectors but
	// preserve its queue, TLS identity, and config. The endpoint can
	// be reactivated without re-enrollment.
	CommandDeactivate CommandKind = "deactivate"

	// CommandWipe is a crypto-erase. The agent stops collectors,
	// flushes and drops the SQLite queue, zeroizes DPAPI-sealed key
	// material, and uninstalls its Windows service. Irreversible —
	// the endpoint must re-enroll to come back online.
	CommandWipe CommandKind = "wipe"

	// CommandRevoke tells the agent its cert has been revoked. The
	// agent stops publishing but may remain installed for forensic
	// inspection. Complements the legacy Revoke path that also
	// flips endpoints.is_active to false.
	CommandRevoke CommandKind = "revoke"
)

// CommandState enumerates the lifecycle states persisted in
// endpoint_commands.state.
type CommandState string

const (
	CommandStatePending      CommandState = "pending"
	CommandStateAcknowledged CommandState = "acknowledged"
	CommandStateCompleted    CommandState = "completed"
	CommandStateFailed       CommandState = "failed"
	CommandStateTimeout      CommandState = "timeout"
)

// Command is a persisted remote command row.
type Command struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenant_id"`
	EndpointID     string          `json:"endpoint_id"`
	IssuedBy       string          `json:"issued_by"`
	Kind           CommandKind     `json:"kind"`
	Reason         string          `json:"reason"`
	State          CommandState    `json:"state"`
	IssuedAt       time.Time       `json:"issued_at"`
	AcknowledgedAt *time.Time      `json:"acknowledged_at,omitempty"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
	ErrorMessage   *string         `json:"error_message,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
}

// CommandPayload is the wire shape agents decode from NATS. It is
// intentionally protojson-friendly so a proto schema can be introduced
// later without breaking compatibility — field names are already
// snake_case and kind is a string.
type CommandPayload struct {
	CommandID  string      `json:"command_id"`
	IssuedAt   time.Time   `json:"issued_at"`
	Kind       CommandKind `json:"kind"`
	Issuer     string      `json:"issuer"`
	Reason     string      `json:"reason"`
	RequireAck bool        `json:"require_ack"`
}

// CommandStore is the persistence surface needed by the command paths.
// It is an interface so unit tests can substitute an in-memory fake.
type CommandStore interface {
	Create(ctx context.Context, c *Command) error
	UpdateState(ctx context.Context, id, state, errorMsg string) error
	GetByID(ctx context.Context, tenantID, id string) (*Command, error)
	ListByEndpoint(ctx context.Context, tenantID, endpointID string, limit int) ([]Command, error)
	ListByTenant(ctx context.Context, tenantID string, page, pageSize int) ([]Command, int, error)

	// EndpointExists is a tenant-scoped existence check used before
	// creating a command row to prevent cross-tenant enumeration.
	// Returns true only when the endpoint belongs to the supplied tenant.
	EndpointExists(ctx context.Context, tenantID, endpointID string) (bool, error)

	// IsUnderLegalHold returns true if there is an active legal_holds
	// row scoped to (tenant, endpoint). Wipe is rejected when true.
	IsUnderLegalHold(ctx context.Context, tenantID, endpointID string) (bool, error)
}

// CommandPublisher is the minimal NATS surface needed. The existing
// *nats.Publisher already satisfies it with Publish(ctx, subject, data).
type CommandPublisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}

// commandAuditor is the subset of audit.Recorder this file uses. Defined
// as an interface so unit tests can inject a fake; *audit.Recorder is
// wrapped by a small adapter (see newAuditAdapter) because its Append
// signature takes an audit.Entry struct directly.
type commandAuditor interface {
	Append(ctx context.Context, e audit.Entry) (int64, error)
}

// Sentinel errors returned by the command paths. Handlers translate
// these into RFC 7807 problem responses with the matching HTTP status.
var (
	ErrEndpointNotFound  = errors.New("endpoint: not found")
	ErrReasonRequired    = errors.New("endpoint: reason is required")
	ErrUnderLegalHold    = errors.New("endpoint: under active legal hold")
	ErrBulkLimitExceeded = errors.New("endpoint: bulk operation limit exceeded")
	ErrUnknownOperation  = errors.New("endpoint: unknown bulk operation")
	ErrPublishFailed     = errors.New("endpoint: command publish failed")
)

// BulkLimit is the hard cap on a single bulk operation request body.
// Above this the request is rejected outright rather than partially
// processed — an admin who needs to wipe >500 endpoints should use a
// job runner, not the interactive API.
const BulkLimit = 500

// BulkResult is the per-endpoint outcome of a BulkOperation call.
type BulkResult struct {
	EndpointID string `json:"endpoint_id"`
	Success    bool   `json:"success"`
	CommandID  string `json:"command_id,omitempty"`
	Error      string `json:"error,omitempty"`
}

// commandSubject returns the canonical NATS subject for a command
// addressed to a specific endpoint. endpoints.command.{tenant}.{endpoint}
// is a new subject namespace reserved by this module; the gateway must
// subscribe to endpoints.command.> once wired.
func commandSubject(tenantID, endpointID string) string {
	return fmt.Sprintf("endpoints.command.%s.%s", tenantID, endpointID)
}

// commandTarget formats the audit entry Target field so searches by
// endpoint row in the audit log return command issuances as well.
func commandTarget(endpointID, commandID string) string {
	return "endpoint:" + endpointID + "/command:" + commandID
}

// issueOpts is the common input assembled by IssueWipe/IssueDeactivate
// before calling issueCommand. Centralising the shape here keeps the
// validation + audit + publish ordering identical across command kinds.
type issueOpts struct {
	kind       CommandKind
	action     audit.Action
	endpointID string
	reason     string
	// checkLegalHold is true only for wipe — deactivate/revoke do not
	// destroy data and are therefore allowed under legal hold.
	checkLegalHold bool
}

// issueCommand is the internal shared path for single-endpoint command
// issuance. The happy path is:
//   1. validate reason + endpoint tenant membership
//   2. (wipe only) reject if the endpoint is under active legal hold
//   3. build the Command row + canonical payload
//   4. INSERT endpoint_commands
//   5. publish to NATS
//   6. on NATS failure → mark row failed (state=failed, error_message)
//      so the admin sees a coherent failure rather than a stuck pending
//   7. write the audit entry
//
// Audit order note: backup/liveview write audit BEFORE the side effect.
// Here we invert that: the audit row must reference the persisted
// command_id (so auditors can trace the NATS message back to the admin
// action), which only exists after the INSERT. The window between
// INSERT and the audit Append is guarded by the hash-chain invariant:
// if the audit call fails we roll the row back to state=failed, so the
// admin sees the failure and no silent drift exists.
func (s *CommandService) issueCommand(ctx context.Context, p *auth.Principal, opts issueOpts) (*Command, error) {
	if opts.reason == "" {
		return nil, ErrReasonRequired
	}
	if opts.endpointID == "" {
		return nil, ErrEndpointNotFound
	}

	exists, err := s.store.EndpointExists(ctx, p.TenantID, opts.endpointID)
	if err != nil {
		return nil, fmt.Errorf("endpoint: check membership: %w", err)
	}
	if !exists {
		// Deliberate enumeration-prevention: same error as not-found
		// regardless of whether the endpoint exists in another tenant.
		return nil, ErrEndpointNotFound
	}

	if opts.checkLegalHold {
		onHold, err := s.store.IsUnderLegalHold(ctx, p.TenantID, opts.endpointID)
		if err != nil {
			return nil, fmt.Errorf("endpoint: legal hold check: %w", err)
		}
		if onHold {
			return nil, ErrUnderLegalHold
		}
	}

	now := time.Now().UTC()
	payload := CommandPayload{
		// ID is assigned server-side by Postgres default (gen_random_uuid);
		// we backfill after the INSERT. Keep a placeholder for payload.
		CommandID:  "",
		IssuedAt:   now,
		Kind:       opts.kind,
		Issuer:     p.UserID,
		Reason:     opts.reason,
		RequireAck: true,
	}
	cmd := &Command{
		TenantID:   p.TenantID,
		EndpointID: opts.endpointID,
		IssuedBy:   p.UserID,
		Kind:       opts.kind,
		Reason:     opts.reason,
		State:      CommandStatePending,
		IssuedAt:   now,
	}

	// Create first so we have the command_id to embed in the payload
	// and the audit target.
	if err := s.store.Create(ctx, cmd); err != nil {
		return nil, fmt.Errorf("endpoint: create command: %w", err)
	}
	payload.CommandID = cmd.ID

	raw, err := json.Marshal(payload)
	if err != nil {
		// Extremely unlikely — json.Marshal of a struct with primitive
		// fields only fails on OOM. Still, mark the row failed so it
		// doesn't stick around as "pending" forever.
		_ = s.store.UpdateState(ctx, cmd.ID, string(CommandStateFailed), "marshal: "+err.Error())
		return nil, fmt.Errorf("endpoint: marshal payload: %w", err)
	}
	cmd.Payload = raw

	if err := s.publisher.Publish(ctx, commandSubject(p.TenantID, opts.endpointID), raw); err != nil {
		_ = s.store.UpdateState(ctx, cmd.ID, string(CommandStateFailed), "publish: "+err.Error())
		return nil, fmt.Errorf("%w: %v", ErrPublishFailed, err)
	}

	if s.recorder != nil {
		if _, err := s.recorder.Append(ctx, audit.Entry{
			Actor:    p.UserID,
			TenantID: p.TenantID,
			Action:   opts.action,
			Target:   commandTarget(opts.endpointID, cmd.ID),
			Details: map[string]any{
				"command_id": cmd.ID,
				"kind":       string(opts.kind),
				"reason":     opts.reason,
			},
		}); err != nil {
			// The command has already been published — we cannot
			// un-publish. Flag the row as failed (from the API's POV)
			// and return the error so the admin retries and the
			// reviewer sees both rows.
			_ = s.store.UpdateState(ctx, cmd.ID, string(CommandStateFailed), "audit: "+err.Error())
			return nil, fmt.Errorf("endpoint: audit: %w", err)
		}
	}

	return cmd, nil
}

// IssueWipe publishes a crypto-erase command to one endpoint. Caller
// must be admin / it_manager / investigator (enforced at the handler).
func (s *CommandService) IssueWipe(ctx context.Context, p *auth.Principal, endpointID, reason string) (*Command, error) {
	return s.issueCommand(ctx, p, issueOpts{
		kind:           CommandWipe,
		action:         audit.ActionEndpointWipe,
		endpointID:     endpointID,
		reason:         reason,
		checkLegalHold: true,
	})
}

// IssueDeactivate publishes a deactivate command to one endpoint.
func (s *CommandService) IssueDeactivate(ctx context.Context, p *auth.Principal, endpointID, reason string) (*Command, error) {
	return s.issueCommand(ctx, p, issueOpts{
		kind:       CommandDeactivate,
		action:     audit.ActionEndpointDeactivate,
		endpointID: endpointID,
		reason:     reason,
	})
}

// BulkOperation fans an operation out over up to BulkLimit endpoints.
// Per-endpoint failures are captured in BulkResult.Error and do not
// short-circuit the loop; the caller sees which specific endpoints
// failed and can retry just those.
//
// One single audit.ActionEndpointCommandBulk row is written at the end
// summarising the batch (operation, input count, success/fail tallies).
// Each individual command row already has its own per-kind audit entry
// from issueCommand, so the bulk summary is a convenience, not a
// replacement — an auditor can verify bulk runs either way.
func (s *CommandService) BulkOperation(ctx context.Context, p *auth.Principal, operation string, endpointIDs []string, reason string) ([]BulkResult, error) {
	if reason == "" {
		return nil, ErrReasonRequired
	}
	if len(endpointIDs) == 0 {
		return nil, fmt.Errorf("endpoint: at least one endpoint_id is required")
	}
	if len(endpointIDs) > BulkLimit {
		return nil, ErrBulkLimitExceeded
	}

	results := make([]BulkResult, 0, len(endpointIDs))
	var successCount, failCount int

	for _, id := range endpointIDs {
		var cmd *Command
		var err error
		switch operation {
		case "wipe":
			cmd, err = s.IssueWipe(ctx, p, id, reason)
		case "deactivate":
			cmd, err = s.IssueDeactivate(ctx, p, id, reason)
		case "revoke":
			// revoke takes the same shape as deactivate for the
			// remote-command purposes — cert revocation itself is
			// handled by the legacy Service.Revoke path but we still
			// publish so the agent can stop publishing immediately.
			cmd, err = s.issueCommand(ctx, p, issueOpts{
				kind:       CommandRevoke,
				action:     audit.ActionEndpointRevoked,
				endpointID: id,
				reason:     reason,
			})
		default:
			return nil, ErrUnknownOperation
		}
		r := BulkResult{EndpointID: id}
		if err != nil {
			r.Success = false
			r.Error = err.Error()
			failCount++
		} else {
			r.Success = true
			r.CommandID = cmd.ID
			successCount++
		}
		results = append(results, r)
	}

	if s.recorder != nil {
		if _, err := s.recorder.Append(ctx, audit.Entry{
			Actor:    p.UserID,
			TenantID: p.TenantID,
			Action:   audit.ActionEndpointCommandBulk,
			Target:   "endpoints:bulk",
			Details: map[string]any{
				"operation":   operation,
				"total":       len(endpointIDs),
				"success":     successCount,
				"failed":      failCount,
				"reason":      reason,
				"endpoint_id": endpointIDs,
			},
		}); err != nil {
			// Audit failure on the summary row is non-fatal to the
			// individual commands — each already has its own audit
			// entry. Log via the returned error so the handler can
			// surface it but don't invalidate per-endpoint results.
			return results, fmt.Errorf("endpoint: bulk audit: %w", err)
		}
	}

	return results, nil
}

// Acknowledge is called by the gateway (via /v1/internal/commands/{id}/ack)
// after the agent confirms receipt and/or completion. The ack is
// idempotent: duplicate acks from a re-delivered NATS message do not
// flip state backwards or write duplicate audit rows.
//
// newState must be one of acknowledged / completed / failed / timeout.
// The store enforces the CHECK constraint too, but we pre-validate for a
// better error message.
func (s *CommandService) Acknowledge(ctx context.Context, tenantID, commandID, newState, errorMsg string) error {
	switch CommandState(newState) {
	case CommandStateAcknowledged, CommandStateCompleted, CommandStateFailed, CommandStateTimeout:
	default:
		return fmt.Errorf("endpoint: invalid ack state %q", newState)
	}
	existing, err := s.store.GetByID(ctx, tenantID, commandID)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrEndpointNotFound
	}
	// Idempotency: if the command is already in a terminal state
	// (completed/failed/timeout) treat subsequent acks as no-ops.
	if existing.State == CommandStateCompleted ||
		existing.State == CommandStateFailed ||
		existing.State == CommandStateTimeout {
		return nil
	}
	if err := s.store.UpdateState(ctx, commandID, newState, errorMsg); err != nil {
		return err
	}
	if s.recorder != nil {
		if _, err := s.recorder.Append(ctx, audit.Entry{
			Actor:    "gateway",
			TenantID: tenantID,
			Action:   audit.ActionEndpointCommandAck,
			Target:   commandTarget(existing.EndpointID, commandID),
			Details: map[string]any{
				"new_state": newState,
				"error":     errorMsg,
			},
		}); err != nil {
			return fmt.Errorf("endpoint: ack audit: %w", err)
		}
	}
	return nil
}

// ListCommandsByEndpoint returns the most recent commands issued to an
// endpoint (most recent first), capped at limit. Used by the console
// endpoint detail page.
func (s *CommandService) ListCommandsByEndpoint(ctx context.Context, tenantID, endpointID string, limit int) ([]Command, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	// Re-use the tenant-scoped existence check so we return 404 (via
	// ErrEndpointNotFound) instead of an empty list for a missing or
	// cross-tenant endpoint.
	exists, err := s.store.EndpointExists(ctx, tenantID, endpointID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrEndpointNotFound
	}
	return s.store.ListByEndpoint(ctx, tenantID, endpointID, limit)
}
