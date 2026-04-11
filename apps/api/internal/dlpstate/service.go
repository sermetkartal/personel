package dlpstate

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"

	vaultapi "github.com/hashicorp/vault/api"

	"github.com/personel/api/internal/audit"
)

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
