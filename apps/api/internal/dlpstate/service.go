package dlpstate

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"

	vaultapi "github.com/hashicorp/vault/api"

	"github.com/personel/api/internal/audit"
)

// TransitionRequest is the body for POST /v1/system/dlp-transition. It is
// called by infra/scripts/dlp-enable.sh and dlp-disable.sh after the out-of-
// API side effects (Vault Secret ID, container start/stop, form verification)
// have completed. The API owns the atomic state + audit + portal banner
// transition; the scripts own the orchestration.
type TransitionRequest struct {
	// Action is one of: "enable-complete", "enable-failed", "disable-complete".
	Action string `json:"action"`
	// ActorID is the human actor (DPO) driving the ceremony.
	ActorID string `json:"actor_id"`
	// DPOEmail is required for enable-complete; recorded in audit for traceability.
	DPOEmail string `json:"dpo_email,omitempty"`
	// FormHash is the sha256 of the signed opt-in form (enable-complete only).
	FormHash string `json:"form_hash,omitempty"`
	// EndpointsBootstrapped is the count from the bootstrap-keys call (enable-complete only).
	EndpointsBootstrapped int `json:"endpoints_bootstrapped,omitempty"`
	// Reason is required for disable-complete and enable-failed.
	Reason string `json:"reason,omitempty"`
}

// TransitionResponse is the body returned from the transition endpoint.
type TransitionResponse struct {
	NewState    DLPStateValue `json:"new_state"`
	AuditID     string        `json:"audit_id"`
	BannerState string        `json:"banner_state"` // "enabled" | "disabled"
}

// ContainerHealth is the health status of the DLP container as seen from the API.
type ContainerHealth string

const (
	ContainerHealthy    ContainerHealth = "healthy"
	ContainerUnhealthy  ContainerHealth = "unhealthy"
	ContainerNotStarted ContainerHealth = "not_started"
)

// DLPStatus is the combined view returned by GET /v1/system/dlp-state.
type DLPStatus struct {
	State               DLPStateValue   `json:"state"`
	EnabledAt           *string         `json:"enabled_at"`           // ISO8601 or null
	EnabledBy           *string         `json:"enabled_by"`           // actor_id or null
	CeremonyFormHash    *string         `json:"ceremony_form_hash"`   // sha256 or null
	ContainerHealth     ContainerHealth `json:"container_health"`
	VaultSecretIDPresent bool           `json:"vault_secret_id_present"`
	LastAuditEventID    string          `json:"last_audit_event_id"`
	Message             string          `json:"message"`
}

// BootstrapResult is the response payload for POST /v1/system/dlp-bootstrap-keys.
type BootstrapResult struct {
	TotalEndpoints int      `json:"total_endpoints"`
	Bootstrapped   int      `json:"bootstrapped"`
	AlreadyPresent int      `json:"already_present"`
	Failed         int      `json:"failed"`
	Failures       []string `json:"failures"`
}

// VaultBootstrapClient is the minimal Vault interface needed for PE-DEK generation.
// The dlp-bootstrap AppRole token must have transit/derive permissions on the tenant
// keys — NOT the admin-api AppRole.
//
// TODO(devops): Provision a dedicated "dlp-bootstrap" Vault AppRole with policy:
//
//	path "transit/datakey/wrapped/tenant/+/tmk" { capabilities = ["update"] }
//
// This role must NOT be the same as the long-lived "dlp-service" AppRole. Its
// Secret ID should be issued on each bootstrap call and revoked immediately after.
type VaultBootstrapClient interface {
	DeriveWrappedPEDEK(ctx context.Context, tenantID, endpointID string) (wrappedBytes []byte, keyVersion string, err error)
	SecretIDPresent(ctx context.Context) (bool, error)
}

// vaultClientAdapter wraps the real Vault API client for bootstrap operations.
// In stub/test mode both methods return safe defaults.
type vaultClientAdapter struct {
	raw      *vaultapi.Client
	stubMode bool
}

// DeriveWrappedPEDEK calls Vault transit datakey to generate a fresh PE-DEK
// wrapped with the DSEK derived from the tenant master key.
func (v *vaultClientAdapter) DeriveWrappedPEDEK(_ context.Context, tenantID, endpointID string) ([]byte, string, error) {
	if v.stubMode {
		// Return a deterministic stub for unit tests.
		stub := []byte(fmt.Sprintf("stub-wrapped-pe-dek:%s:%s", tenantID, endpointID))
		return stub, "v1", nil
	}
	path := fmt.Sprintf("transit/datakey/wrapped/tenant/%s/tmk", tenantID)
	secret, err := v.raw.Logical().Write(path, map[string]interface{}{
		"context": base64.StdEncoding.EncodeToString([]byte(endpointID)),
		"bits":    256,
	})
	if err != nil {
		return nil, "", fmt.Errorf("dlpstate: vault derive pe-dek: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return nil, "", fmt.Errorf("dlpstate: vault derive pe-dek: nil response")
	}
	ciphertext, ok := secret.Data["ciphertext"].(string)
	if !ok {
		return nil, "", fmt.Errorf("dlpstate: vault derive pe-dek: missing ciphertext")
	}
	wrapped, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		// Vault may return the ciphertext as a raw string; store as-is.
		wrapped = []byte(ciphertext)
	}
	keyVersion := "v1"
	if kv, ok := secret.Data["key_version"].(float64); ok {
		keyVersion = fmt.Sprintf("v%d", int(kv))
	}
	return wrapped, keyVersion, nil
}

// SecretIDPresent checks whether the dlp-service AppRole has an active Secret ID
// by attempting a non-destructive lookup. Returns false on any Vault error so
// the state endpoint degrades gracefully when Vault is unreachable.
func (v *vaultClientAdapter) SecretIDPresent(_ context.Context) (bool, error) {
	if v.stubMode {
		return false, nil
	}
	secret, err := v.raw.Logical().List("auth/approle/role/dlp-service/secret-id")
	if err != nil {
		return false, nil //nolint:nilerr // degrade gracefully
	}
	if secret == nil || secret.Data == nil {
		return false, nil
	}
	keys, _ := secret.Data["keys"].([]interface{})
	return len(keys) > 0, nil
}

// NewVaultBootstrapClient wraps a raw Vault client. Pass nil raw to enable stub mode.
func NewVaultBootstrapClient(raw *vaultapi.Client) VaultBootstrapClient {
	if raw == nil {
		return &vaultClientAdapter{stubMode: true}
	}
	return &vaultClientAdapter{raw: raw}
}

// Service orchestrates DLP state reads and PE-DEK bootstrapping.
type Service struct {
	store    *Store
	vault    VaultBootstrapClient
	recorder *audit.Recorder
	log      *slog.Logger
}

// NewService creates the DLP state service.
func NewService(store *Store, vault VaultBootstrapClient, rec *audit.Recorder, log *slog.Logger) *Service {
	return &Service{store: store, vault: vault, recorder: rec, log: log}
}

// GetStatus computes the current DLP status by joining the DB row with a live
// Vault Secret ID presence check. Container health is always "not_started" in
// Phase 1 because the API does not have access to the Docker socket; the console
// displays this as "DLP container durumu bilinmiyor".
func (s *Service) GetStatus(ctx context.Context) (*DLPStatus, error) {
	row, err := s.store.GetState(ctx)
	if err != nil {
		return nil, fmt.Errorf("dlpstate: get status: %w", err)
	}

	secretIDPresent, _ := s.vault.SecretIDPresent(ctx)

	var enabledAt, enabledBy, ceremonyHash *string
	if row.EnabledAt != nil {
		t := row.EnabledAt.Format("2006-01-02T15:04:05Z")
		enabledAt = &t
	}
	enabledBy = row.EnabledBy
	ceremonyHash = row.CeremonyFormHash

	lastAuditEventID := ""
	if row.LastAuditEventID != nil {
		lastAuditEventID = *row.LastAuditEventID
	}

	return &DLPStatus{
		State:                row.State,
		EnabledAt:            enabledAt,
		EnabledBy:            enabledBy,
		CeremonyFormHash:     ceremonyHash,
		ContainerHealth:      ContainerNotStarted,
		VaultSecretIDPresent: secretIDPresent,
		LastAuditEventID:     lastAuditEventID,
		Message:              row.Message,
	}, nil
}

// Transition performs an atomic DLP state transition: update dlp_state row,
// write the corresponding audit entry, and return the new state. The caller
// is the dlp-enable.sh or dlp-disable.sh script authenticated as dlp-admin.
//
// This endpoint is the single source of truth for state transitions. The
// portal banner is derived from dlp_state.state, so updating the state
// automatically updates the banner on the next portal request.
//
// ADR 0013 amendment items: A1 (transition semantics), A3 (failure handling
// via enable-failed action), A4 (disable does NOT destroy ciphertext).
func (s *Service) Transition(ctx context.Context, req TransitionRequest) (*TransitionResponse, error) {
	if req.ActorID == "" {
		return nil, fmt.Errorf("dlpstate: transition: actor_id is required")
	}

	var (
		newState    DLPStateValue
		enabledAt   *time.Time
		enabledBy   *string
		formHash    *string
		auditAction audit.Action
		message     string
		bannerState string
	)

	switch req.Action {
	case "enable-complete":
		if req.FormHash == "" || req.DPOEmail == "" {
			return nil, fmt.Errorf("dlpstate: enable-complete requires form_hash and dpo_email")
		}
		now := time.Now().UTC()
		newState = StateEnabled
		enabledAt = &now
		enabledBy = &req.ActorID
		formHash = &req.FormHash
		auditAction = audit.ActionDLPEnabled
		message = fmt.Sprintf("DLP %s tarihinde %s tarafından aktif edildi.", now.Format("2006-01-02"), req.ActorID)
		bannerState = "enabled"

	case "disable-complete":
		if req.Reason == "" {
			return nil, fmt.Errorf("dlpstate: disable-complete requires reason")
		}
		newState = StateDisabled
		auditAction = audit.ActionDLPDisabled
		message = "DLP devre dışı — mevcut şifreli içerik TTL ile doğal olarak silinir (ADR 0013 A4)."
		bannerState = "disabled"

	case "enable-failed":
		if req.Reason == "" {
			return nil, fmt.Errorf("dlpstate: enable-failed requires reason")
		}
		newState = StateDisabled
		auditAction = audit.ActionDLPEnableFailed
		message = fmt.Sprintf("DLP aktivasyonu başarısız: %s — durum disabled'e döndürüldü.", req.Reason)
		bannerState = "disabled"

	default:
		return nil, fmt.Errorf("dlpstate: unknown action %q", req.Action)
	}

	// Write the audit entry first so the last_audit_event_id points at an
	// existing row.
	auditID, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    req.ActorID,
		TenantID: "", // DLP transitions are tenant-wide
		Action:   auditAction,
		Target:   "system:dlp",
		Details: map[string]any{
			"action":                  req.Action,
			"dpo_email":               req.DPOEmail,
			"form_hash":               req.FormHash,
			"endpoints_bootstrapped":  req.EndpointsBootstrapped,
			"reason":                  req.Reason,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("dlpstate: transition: audit append: %w", err)
	}

	auditIDStr := fmt.Sprintf("%d", auditID)
	if err := s.store.UpdateState(ctx, newState, enabledAt, enabledBy, formHash, &auditIDStr, message); err != nil {
		return nil, fmt.Errorf("dlpstate: transition: update state: %w", err)
	}

	s.log.InfoContext(ctx, "dlp state transitioned",
		slog.String("action", req.Action),
		slog.String("new_state", string(newState)),
		slog.String("actor", req.ActorID),
		slog.String("audit_id", auditIDStr),
	)

	return &TransitionResponse{
		NewState:    newState,
		AuditID:     auditIDStr,
		BannerState: bannerState,
	}, nil
}

// BootstrapPEDEKs iterates all active endpoints for tenantID, generates a
// fresh wrapped PE-DEK for each that does not yet have one, and stores it in
// keystroke_keys. The result contains per-endpoint counts and any failures.
//
// The operation is idempotent: endpoints that already have a key are counted
// in AlreadyPresent and skipped without error.
func (s *Service) BootstrapPEDEKs(ctx context.Context, tenantID, actorID string) (*BootstrapResult, error) {
	endpoints, err := s.store.ListEndpoints(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("dlpstate: bootstrap: list endpoints: %w", err)
	}

	result := &BootstrapResult{
		TotalEndpoints: len(endpoints),
		Failures:       []string{},
	}

	for _, endpointID := range endpoints {
		exists, err := s.store.KeyExists(ctx, endpointID)
		if err != nil {
			result.Failed++
			result.Failures = append(result.Failures, fmt.Sprintf("%s: key-exists-check: %v", endpointID, err))
			continue
		}
		if exists {
			result.AlreadyPresent++
			continue
		}

		wrappedPEDEK, keyVersion, err := s.vault.DeriveWrappedPEDEK(ctx, tenantID, endpointID)
		if err != nil {
			result.Failed++
			result.Failures = append(result.Failures, fmt.Sprintf("%s: vault-derive: %v", endpointID, err))
			continue
		}

		if err := s.store.InsertKey(ctx, endpointID, tenantID, keyVersion, wrappedPEDEK); err != nil {
			result.Failed++
			result.Failures = append(result.Failures, fmt.Sprintf("%s: insert-key: %v", endpointID, err))
			continue
		}

		// Per-endpoint audit entry (ADR 0013 A2).
		_, _ = s.recorder.Append(ctx, audit.Entry{
			Actor:    actorID,
			TenantID: tenantID,
			Action:   audit.ActionDLPPEDEKBootstrapped,
			Target:   fmt.Sprintf("endpoint:%s", endpointID),
			Details:  map[string]any{"key_version": keyVersion},
		})

		result.Bootstrapped++
	}

	// Summary audit entry.
	_, _ = s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionDLPPEDEKBootstrapBatch,
		Target:   fmt.Sprintf("tenant:%s", tenantID),
		Details: map[string]any{
			"total":           result.TotalEndpoints,
			"bootstrapped":    result.Bootstrapped,
			"already_present": result.AlreadyPresent,
			"failed":          result.Failed,
		},
	})

	return result, nil
}
