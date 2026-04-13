// Package endpoint — certificate refresh endpoint.
//
// Faz 6 item #63. An enrolled agent whose cert is approaching its TTL
// can call POST /v1/endpoints/{id}/refresh-token (via the admin API,
// authenticated as an IT operator / manager / admin) to have the Admin
// API sign a fresh CSR without going through the full enrollment
// ceremony again. The old cert serial is best-effort revoked in Vault;
// the new serial replaces the old one in endpoints.cert_serial.
//
// This file is intentionally kept small and readable — the refresh
// path has strict rate limiting (one per ten minutes per endpoint) and
// every successful call is a deliberate PKI action, so the handler
// stays defensive at every step.
package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// refreshMinInterval is the per-endpoint rate limit. One successful
// refresh every ten minutes is enough headroom to cover pathological
// clock skew + a few agent retries while keeping an attacker who
// somehow captured a valid admin session from flooding the Vault PKI
// engine with sign requests.
const refreshMinInterval = 10 * time.Minute

// RefreshTokenRequest is the JSON body the admin console POSTs to
// /v1/endpoints/{id}/refresh-token. Only the CSR is required — all
// identity information (tenant, endpoint ID) flows through the URL
// path and the Principal.
type RefreshTokenRequest struct {
	CSRPEM string `json:"csr_pem"`
}

// RefreshTokenResponse is the successful response body. It mirrors
// the shape of AgentEnrollResponse for the cert-bearing fields; the
// admin console proxies it back to the operator unchanged.
type RefreshTokenResponse struct {
	EndpointID   string `json:"endpoint_id"`
	CertPEM      string `json:"cert_pem"`
	ChainPEM     string `json:"chain_pem"`
	SerialNumber string `json:"serial_number"`
	ExpiresAt    string `json:"expires_at"`
}

// RefreshResult is the service-layer return from RefreshToken. The
// handler converts it to the wire response.
type RefreshResult struct {
	EndpointID   string
	CertPEM      string
	ChainPEM     string
	SerialNumber string
	NotAfter     time.Time
}

// Sentinel errors for the refresh flow so the handler can map them to
// HTTP statuses without import cycles. Match the agent_enroll.go
// pattern.
var (
	errRefreshNotFound     = errors.New("endpoint refresh: not found")
	errRefreshNotActive    = errors.New("endpoint refresh: endpoint not active")
	errRefreshRateLimited  = errors.New("endpoint refresh: rate limited")
	errRefreshVaultFailure = errors.New("endpoint refresh: vault failure")
	errRefreshCSRInvalid   = errors.New("endpoint refresh: csr invalid")
)

// RefreshToken issues a new PKI leaf for an already-enrolled endpoint
// without consuming an enrollment token. The caller must be a human
// operator — this is gated by HTTP-level RBAC on
// /v1/endpoints/{id}/refresh-token, not by an enrollment Secret ID.
//
// Preconditions:
//   - Endpoint exists AND belongs to the caller's tenant AND is_active
//   - It has been at least refreshMinInterval since the last refresh
//   - The submitted CSR parses and its signature verifies
//
// On success: the new cert is signed, the old cert is best-effort
// revoked (errors logged but not propagated — CRL is best-effort),
// the refresh counters are bumped, and an audit entry is recorded.
func (s *Service) RefreshToken(ctx context.Context, p *auth.Principal, endpointID, csrPEM, actorIP string) (*RefreshResult, error) {
	if p == nil || p.TenantID == "" {
		return nil, errRefreshNotFound
	}
	if endpointID == "" {
		return nil, errRefreshNotFound
	}

	// 1. Parse + verify the CSR upfront so a garbage request never
	//    reaches Vault.
	csr, _, err := parseAndVerifyCSR(csrPEM)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errRefreshCSRInvalid, err)
	}
	if csr == nil {
		return nil, errRefreshCSRInvalid
	}

	// 2. Load the endpoint row inside the caller's tenant. A row from a
	//    different tenant must surface as NOT FOUND (not FORBIDDEN) so
	//    an attacker cannot enumerate endpoint IDs across tenants.
	store := s.refreshStore()
	snap, err := store.LoadForRefresh(ctx, p.TenantID, endpointID)
	if err != nil {
		return nil, err
	}
	if snap == nil {
		return nil, errRefreshNotFound
	}
	if !snap.IsActive {
		return nil, errRefreshNotActive
	}

	// 3. Rate limit check. last_refresh_at is set transactionally at the
	//    end of a successful refresh, so a concurrent second request
	//    that races this check will either see the pivot and 429 OR win
	//    the race, sign a second cert, and update last_refresh_at. The
	//    window is a soft bound — the audit log is the authoritative
	//    record. For tighter guarantees a row-level advisory lock could
	//    be added but is not strictly required for the threat model.
	now := time.Now().UTC()
	if snap.LastRefreshAt != nil {
		since := now.Sub(snap.LastRefreshAt.UTC())
		if since < refreshMinInterval {
			return nil, errRefreshRateLimited
		}
	}

	// 4. Log into Vault with the agent-enrollment AppRole and sign the
	//    new CSR. We reuse the same AppRole scope as first-time enroll
	//    because the policy is already scoped tightly to
	//    pki/sign/agent-cert and nothing else.
	pki := s.vaultPKI()
	roleID, err := pki.GetEnrollmentRoleID(ctx)
	if err != nil {
		s.log.Error("endpoint refresh: get enrollment role id",
			"error", err.Error(),
			"endpoint_id", endpointID,
		)
		return nil, errRefreshVaultFailure
	}
	secretID, err := pki.IssueEnrollmentSecretID(ctx)
	if err != nil {
		s.log.Error("endpoint refresh: issue secret id",
			"error", err.Error(),
			"endpoint_id", endpointID,
		)
		return nil, errRefreshVaultFailure
	}
	signClient, err := pki.LoginWithAppRole(ctx, roleID, secretID)
	if err != nil {
		s.log.Error("endpoint refresh: approle login",
			"error", err.Error(),
			"endpoint_id", endpointID,
		)
		return nil, errRefreshVaultFailure
	}
	issued, err := pki.SignAgentCSR(ctx, signClient, csrPEM, snap.Hostname+".personel.internal", "720h")
	if err != nil {
		s.log.Error("endpoint refresh: pki sign",
			"error", err.Error(),
			"endpoint_id", endpointID,
		)
		return nil, errRefreshVaultFailure
	}

	// 5. Update the endpoint row with the new serial + bump the refresh
	//    counters. Happens BEFORE the revoke call so the DB is the
	//    source of truth for which serial is currently authoritative.
	newSerial := formatSerialHex(issued.SerialNumber)
	if err := store.MarkRefreshed(ctx, endpointID, newSerial, now); err != nil {
		return nil, fmt.Errorf("endpoint refresh: mark refreshed: %w", err)
	}

	// 6. Best-effort revoke of the old serial. A Vault outage here
	//    MUST NOT fail the refresh — the new cert is already issued
	//    and the DB already flipped. Log loudly so an on-call engineer
	//    can follow up manually. KVKK note: the cert_pem we just
	//    signed is NOT logged; only the serial hex.
	oldSerial := snap.CertSerial
	if oldSerial != "" {
		if err := pki.RevokeCert(ctx, oldSerial); err != nil {
			s.log.Warn("endpoint refresh: revoke old cert failed (CRL is best-effort)",
				"error", err.Error(),
				"endpoint_id", endpointID,
				"old_serial", oldSerial,
				"new_serial", newSerial,
			)
		}
	}

	// 7. Audit. Deliberately include BOTH serials so an investigator
	//    can reconstruct the rotation trail, but nothing else
	//    cert-bearing — no PEM, no public key material. The actor is
	//    the human operator behind the OIDC session.
	_, _ = s.auditAppend(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: p.TenantID,
		Action:   audit.ActionEndpointTokenRefreshed,
		Target:   "endpoint:" + endpointID,
		Details: map[string]any{
			"hostname":   snap.Hostname,
			"old_serial": oldSerial,
			"new_serial": newSerial,
			"expires_at": issued.NotAfter.UTC().Format(time.RFC3339),
			"actor_ip":   actorIP,
			"tenant_id":  p.TenantID,
		},
	})

	return &RefreshResult{
		EndpointID:   endpointID,
		CertPEM:      issued.CertificatePEM,
		ChainPEM:     issued.CAChainPEM,
		SerialNumber: newSerial,
		NotAfter:     issued.NotAfter,
	}, nil
}

// refreshStore returns the refresh-path store. The default is a thin
// adapter over the concrete *pgxpool.Pool; unit tests override this
// via SetRefreshStoreForTesting.
func (s *Service) refreshStore() refreshStore {
	if s.refreshStoreOverride != nil {
		return s.refreshStoreOverride
	}
	return &pgxRefreshStore{pool: s.pool}
}

// SetRefreshStoreForTesting installs a fake refresh store. ONLY for
// use in *_test.go — the production path should never touch this
// setter. Kept in the same file as the production path so the seam is
// obvious.
func (s *Service) SetRefreshStoreForTesting(store refreshStore) {
	s.refreshStoreOverride = store
}

// pgxRefreshStore adapts *pgxpool.Pool to the refreshStore interface.
type pgxRefreshStore struct {
	pool *pgxpool.Pool
}

// LoadForRefresh reads the subset of endpoints columns the refresh
// path needs in a single round trip. Returns (nil, nil) on
// pgx.ErrNoRows so the service can map it to errRefreshNotFound.
// tenant_id is part of the WHERE clause to enforce tenant isolation.
func (p *pgxRefreshStore) LoadForRefresh(ctx context.Context, tenantID, endpointID string) (*refreshSnapshot, error) {
	var snap refreshSnapshot
	var serial *string
	err := p.pool.QueryRow(ctx,
		`SELECT tenant_id::text, hostname, cert_serial, is_active, last_refresh_at
		   FROM endpoints
		  WHERE id = $1::uuid AND tenant_id = $2::uuid`,
		endpointID, tenantID,
	).Scan(&snap.TenantID, &snap.Hostname, &serial, &snap.IsActive, &snap.LastRefreshAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("endpoint refresh: load: %w", err)
	}
	if serial != nil {
		snap.CertSerial = *serial
	}
	return &snap, nil
}

// MarkRefreshed writes the new serial + bumps the refresh counters.
func (p *pgxRefreshStore) MarkRefreshed(ctx context.Context, endpointID, newSerial string, now time.Time) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE endpoints
		    SET cert_serial = $2,
		        last_refresh_at = $3,
		        refresh_count = refresh_count + 1
		  WHERE id = $1::uuid`,
		endpointID, newSerial, now,
	)
	if err != nil {
		return fmt.Errorf("endpoint refresh: update: %w", err)
	}
	return nil
}

// RefreshTokenHandler is the http.HandlerFunc for
// POST /v1/endpoints/{id}/refresh-token. RBAC is enforced at route
// mount time in httpserver/server.go — admin / it_manager / it_operator.
func RefreshTokenHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthenticated", "err.unauthenticated")
			return
		}
		endpointID := chi.URLParam(r, "endpointID")
		if endpointID == "" {
			httpx.WriteError(w, r, http.StatusNotFound,
				httpx.ProblemTypeNotFound, "Not Found", "err.not_found")
			return
		}

		// Cap body so a malicious client can't OOM us.
		r.Body = http.MaxBytesReader(w, r.Body, 128*1024)

		var req RefreshTokenRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid Request Body", "err.validation")
			return
		}
		if req.CSRPEM == "" || len(req.CSRPEM) > 64*1024 {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Validation Error", "err.validation")
			return
		}

		result, err := svc.RefreshToken(r.Context(), p, endpointID, req.CSRPEM, clientIP(r))
		if err != nil {
			switch {
			case errors.Is(err, errRefreshNotFound):
				// Explicitly 404 for cross-tenant enumeration attempts
				// (the service returns errRefreshNotFound when the row
				// is in a different tenant, not a 403 — see docstring
				// on LoadForRefresh).
				httpx.WriteError(w, r, http.StatusNotFound,
					httpx.ProblemTypeNotFound, "Endpoint Not Found", "err.not_found")
			case errors.Is(err, errRefreshNotActive):
				httpx.WriteError(w, r, http.StatusConflict,
					httpx.ProblemTypeConflict, "Endpoint Revoked", "err.conflict")
			case errors.Is(err, errRefreshRateLimited):
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(refreshMinInterval.Seconds())))
				httpx.WriteError(w, r, http.StatusTooManyRequests,
					httpx.ProblemTypeRateLimit, "Refresh Rate Limited", "err.rate_limited")
			case errors.Is(err, errRefreshCSRInvalid):
				httpx.WriteError(w, r, http.StatusBadRequest,
					httpx.ProblemTypeValidation, "Invalid CSR", "err.validation")
			case errors.Is(err, errRefreshVaultFailure):
				httpx.WriteError(w, r, http.StatusServiceUnavailable,
					httpx.ProblemTypeInternal, "Upstream Vault Failure", "err.internal")
			default:
				svc.log.Error("endpoint refresh: internal failure",
					"error", err.Error(),
					"endpoint_id", endpointID,
				)
				httpx.WriteError(w, r, http.StatusInternalServerError,
					httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			}
			return
		}

		httpx.WriteJSON(w, http.StatusOK, &RefreshTokenResponse{
			EndpointID:   result.EndpointID,
			CertPEM:      result.CertPEM,
			ChainPEM:     result.ChainPEM,
			SerialNumber: result.SerialNumber,
			ExpiresAt:    result.NotAfter.UTC().Format(time.RFC3339),
		})
	}
}

