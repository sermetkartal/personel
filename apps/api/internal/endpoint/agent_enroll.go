// Package endpoint — unauthenticated agent enrollment endpoint.
//
// POST /v1/agent-enroll is reached by the Windows agent installer's
// enroll.exe helper. It is intentionally NOT behind the OIDC bearer
// auth middleware: the agent has no Keycloak identity yet, only the
// single-use AppRole credential bundled in the opaque enrollment token
// the human operator handed it. Authorization is anchored on three
// independent gates:
//
//  1. The Vault Secret ID must still be valid (single-use; Vault enforces).
//  2. The matching enrollment_tokens row must exist, be unused and unexpired.
//  3. The presented CSR must be cryptographically valid (CheckSignature).
//
// Once those pass, the API logs into Vault using a fresh client (NOT the
// admin API's own root token) and asks the PKI engine to sign the CSR.
// The leaf certificate plus the CA chain are returned to the agent, the
// enrollment_tokens row is marked used, and an audit event is recorded.
package endpoint

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/httpx"
)

// AgentEnrollRequest is the JSON body posted by the Windows agent
// installer to /v1/agent-enroll. All fields are required except
// agent_version which is informational.
type AgentEnrollRequest struct {
	RoleID        string `json:"role_id"`
	SecretID      string `json:"secret_id"`
	CSRPEM        string `json:"csr_pem"`
	HWFingerprint string `json:"hw_fingerprint"`
	Hostname      string `json:"hostname"`
	OSVersion     string `json:"os_version"`
	AgentVersion  string `json:"agent_version"`
}

// AgentEnrollResponse is the JSON body returned to the agent on
// successful enrollment.
type AgentEnrollResponse struct {
	EndpointID    string `json:"endpoint_id"`
	TenantID      string `json:"tenant_id"`
	CertPEM       string `json:"cert_pem"`
	ChainPEM      string `json:"chain_pem"`
	GatewayURL    string `json:"gateway_url"`
	SPKIPinSHA256 string `json:"spki_pin_sha256"`
	SerialNumber  string `json:"serial_number"`
	NotAfter      string `json:"not_after"`
}

// agentEnrollResult is the in-process service-layer return type. The
// handler converts it to the wire AgentEnrollResponse.
type agentEnrollResult struct {
	EndpointID    string
	TenantID      string
	CertPEM       string
	ChainPEM      string
	GatewayURL    string
	SPKIPinSHA256 string
	SerialNumber  string
	NotAfter      time.Time
}

// validateRequest performs lightweight schema + length validation. CSR
// signature validity is verified separately so the audit log can
// distinguish "garbage input" from "bad CSR".
func (r *AgentEnrollRequest) validate() error {
	if r == nil {
		return errors.New("nil body")
	}
	if r.RoleID == "" {
		return errors.New("role_id is required")
	}
	if r.SecretID == "" {
		return errors.New("secret_id is required")
	}
	if r.CSRPEM == "" {
		return errors.New("csr_pem is required")
	}
	if r.Hostname == "" {
		return errors.New("hostname is required")
	}
	if len(r.Hostname) > 253 {
		return errors.New("hostname too long")
	}
	if len(r.CSRPEM) > 64*1024 {
		return errors.New("csr_pem too large")
	}
	if len(r.HWFingerprint) > 256 {
		return errors.New("hw_fingerprint too long")
	}
	if len(r.OSVersion) > 256 {
		return errors.New("os_version too long")
	}
	if len(r.AgentVersion) > 64 {
		return errors.New("agent_version too long")
	}
	return nil
}

// parseAndVerifyCSR decodes a PEM-encoded CSR and verifies its signature.
// Returns the parsed CSR + the SPKI pin (base64 SHA-256 over the
// SubjectPublicKeyInfo bytes). The pin is what the agent will see in
// future cert renewals to detect mid-stream key swaps.
func parseAndVerifyCSR(pemBytes string) (*x509.CertificateRequest, string, error) {
	block, _ := pem.Decode([]byte(pemBytes))
	if block == nil {
		return nil, "", errors.New("csr: not a PEM block")
	}
	if block.Type != "CERTIFICATE REQUEST" && block.Type != "NEW CERTIFICATE REQUEST" {
		return nil, "", fmt.Errorf("csr: unexpected PEM type %q", block.Type)
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, "", fmt.Errorf("csr: parse: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, "", fmt.Errorf("csr: signature: %w", err)
	}
	sum := sha256.Sum256(csr.RawSubjectPublicKeyInfo)
	return csr, base64.StdEncoding.EncodeToString(sum[:]), nil
}

// AgentEnroll runs the full enrollment ceremony. spkiPin must be the
// pre-computed SPKI pin from parseAndVerifyCSR — the handler validates
// the CSR before reaching this method, so we pass the pin through
// rather than re-parsing.
func (s *Service) AgentEnroll(ctx context.Context, req *AgentEnrollRequest, spkiPin, actorIP string) (*agentEnrollResult, error) {
	// 1. Look up the enrollment_tokens row by secret_id. We need the
	//    tenant_id binding before doing any Vault round trip — if the
	//    token isn't ours we want to fail fast and not consume Vault
	//    quota.
	var (
		tenantID  string
		usedAt    *time.Time
		expiresAt time.Time
		dbRoleID  string
	)
	err := s.pool.QueryRow(ctx,
		`SELECT tenant_id::text, vault_role_id, used_at, expires_at
		   FROM enrollment_tokens
		  WHERE vault_secret_id = $1`,
		req.SecretID,
	).Scan(&tenantID, &dbRoleID, &usedAt, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errEnrollUnauthorized
		}
		return nil, fmt.Errorf("agent enroll: lookup token: %w", err)
	}
	if usedAt != nil {
		return nil, errEnrollUnauthorized
	}
	if time.Now().UTC().After(expiresAt) {
		return nil, errEnrollUnauthorized
	}
	// Defence in depth: the role_id presented by the agent must match
	// what we issued. Vault would reject a mismatched pair anyway but
	// failing here saves a round trip and tightens the audit story.
	if !strings.EqualFold(dbRoleID, req.RoleID) {
		return nil, errEnrollUnauthorized
	}

	// 2. Vault AppRole login on a fresh client.
	signClient, err := s.vaultClient.LoginWithAppRole(ctx, req.RoleID, req.SecretID)
	if err != nil {
		s.log.Error("agent enroll: vault login failed",
			"error", err.Error(),
			"hostname", req.Hostname,
			"tenant_id", tenantID,
		)
		return nil, errEnrollVaultFailure
	}

	// 3. Sign the CSR.
	commonName := req.Hostname + ".personel.internal"
	issued, err := s.vaultClient.SignAgentCSR(ctx, signClient, req.CSRPEM, commonName, "720h")
	if err != nil {
		s.log.Error("agent enroll: pki sign failed",
			"error", err.Error(),
			"hostname", req.Hostname,
			"tenant_id", tenantID,
		)
		return nil, errEnrollVaultFailure
	}
	// We do not explicitly revoke the enrollment AppRole token here:
	// Vault's auth/approle/role/agent-enrollment is configured with
	// token_ttl=15m so the lease ages out shortly anyway, and the
	// agent-enroll policy only allows pki/sign/agent-cert which is
	// scoped enough that an aged-out lease is acceptable.

	// 4. Insert endpoint + mark token used inside a transaction so a
	//    crash between the two writes can't leak a half-enrolled
	//    endpoint.
	var endpointID string
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("agent enroll: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// hw_fingerprint comes in as a hex string ("sha256-hex"); decode
	// to raw bytes so the BYTEA column stores the canonical 32-byte
	// digest instead of the ASCII representation.
	var hwBytes []byte
	if req.HWFingerprint != "" {
		// Strip an optional "sha256:" or "sha256-" prefix if the agent
		// added one — keep the raw hex.
		hexStr := req.HWFingerprint
		if strings.HasPrefix(hexStr, "sha256:") {
			hexStr = hexStr[len("sha256:"):]
		} else if strings.HasPrefix(hexStr, "sha256-") {
			hexStr = hexStr[len("sha256-"):]
		}
		if b, err := decodeHex(hexStr); err == nil {
			hwBytes = b
		}
	}
	err = tx.QueryRow(ctx,
		`INSERT INTO endpoints (
			tenant_id, hostname, os_version, agent_version,
			hardware_fingerprint, cert_serial, enrolled_at, is_active
		 ) VALUES (
			$1::uuid, $2, NULLIF($3,''), NULLIF($4,''),
			$5, $6, now(), true
		 ) RETURNING id::text`,
		tenantID,
		req.Hostname,
		req.OSVersion,
		req.AgentVersion,
		hwBytes,
		issued.SerialNumber,
	).Scan(&endpointID)
	if err != nil {
		return nil, fmt.Errorf("agent enroll: insert endpoint: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE enrollment_tokens
		    SET used_at = now(),
		        used_by_endpoint = $2::uuid
		  WHERE vault_secret_id = $1
		    AND used_at IS NULL`,
		req.SecretID,
		endpointID,
	)
	if err != nil {
		return nil, fmt.Errorf("agent enroll: mark token used: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("agent enroll: commit: %w", err)
	}

	// 5. Audit. Actor identity is anonymous-ish: the agent has no
	//    Keycloak user, so we tag the actor with the role_id prefix
	//    so an investigator can correlate it back to the issuance row.
	rolePrefix := req.RoleID
	if len(rolePrefix) > 8 {
		rolePrefix = rolePrefix[:8]
	}
	_, _ = s.recorder.Append(ctx, audit.Entry{
		Actor:    "enrollment-token:" + rolePrefix,
		TenantID: tenantID,
		Action:   audit.ActionEndpointEnrolled,
		Target:   "endpoint:" + endpointID,
		Details: map[string]any{
			"hostname":       req.Hostname,
			"serial":         issued.SerialNumber,
			"tenant_id":      tenantID,
			"agent_version":  req.AgentVersion,
			"os_version":     req.OSVersion,
			"hw_fingerprint": req.HWFingerprint,
			"actor_ip":       actorIP,
		},
	})

	return &agentEnrollResult{
		EndpointID:    endpointID,
		TenantID:      tenantID,
		CertPEM:       issued.CertificatePEM,
		ChainPEM:      issued.CAChainPEM,
		GatewayURL:    s.gatewayURL,
		SPKIPinSHA256: spkiPin,
		SerialNumber:  formatSerialHex(issued.SerialNumber),
		NotAfter:      issued.NotAfter,
	}, nil
}

// decodeHex is a thin wrapper around encoding/hex.DecodeString that
// also tolerates an empty string by returning (nil, nil). The handler
// uses it for the hardware_fingerprint round trip.
func decodeHex(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	return hex.DecodeString(s)
}

// formatSerialHex normalises Vault's "53:7b:..." colon-separated serial
// to lowercase contiguous hex. Agents store the cert serial as a single
// hex blob and we want consistent comparison semantics on both sides.
func formatSerialHex(s string) string {
	if s == "" {
		return ""
	}
	cleaned := strings.ToLower(strings.ReplaceAll(s, ":", ""))
	// Validate it round-trips through hex.DecodeString — if Vault ever
	// emits a non-hex serial we'd rather return the raw value than
	// silently corrupt it.
	if _, err := hex.DecodeString(cleaned); err != nil {
		return s
	}
	return cleaned
}

// Sentinel errors used by the handler to map service-layer failures to
// HTTP status codes.
var (
	errEnrollUnauthorized = errors.New("agent enroll: unauthorized")
	errEnrollVaultFailure = errors.New("agent enroll: vault failure")
)

// AgentEnrollHandler returns an http.HandlerFunc for POST
// /v1/agent-enroll. It is mounted on the unauthenticated public router
// — auth is enforced by the enrollment-token + CSR signature verification
// inside Service.AgentEnroll.
func AgentEnrollHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Cap the body so a malicious client can't OOM us.
		r.Body = http.MaxBytesReader(w, r.Body, 128*1024)

		var req AgentEnrollRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid Request Body", "err.validation")
			return
		}
		if err := req.validate(); err != nil {
			svc.log.Info("agent enroll: validation rejected",
				"error", err.Error(),
				"hostname", req.Hostname,
			)
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Validation Error", "err.validation")
			return
		}
		_, spkiPin, err := parseAndVerifyCSR(req.CSRPEM)
		if err != nil {
			svc.log.Info("agent enroll: csr rejected",
				"error", err.Error(),
				"hostname", req.Hostname,
			)
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid CSR", "err.validation")
			return
		}

		actorIP := clientIP(r)
		result, err := svc.AgentEnroll(r.Context(), &req, spkiPin, actorIP)
		if err != nil {
			switch {
			case errors.Is(err, errEnrollUnauthorized):
				httpx.WriteError(w, r, http.StatusUnauthorized,
					httpx.ProblemTypeAuth, "Enrollment Token Invalid", "err.unauthenticated")
			case errors.Is(err, errEnrollVaultFailure):
				httpx.WriteError(w, r, http.StatusBadGateway,
					httpx.ProblemTypeInternal, "Upstream Vault Failure", "err.internal")
			default:
				svc.log.Error("agent enroll: internal failure",
					"error", err.Error(),
					"hostname", req.Hostname,
				)
				httpx.WriteError(w, r, http.StatusInternalServerError,
					httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			}
			return
		}

		httpx.WriteJSON(w, http.StatusOK, &AgentEnrollResponse{
			EndpointID:    result.EndpointID,
			TenantID:      result.TenantID,
			CertPEM:       result.CertPEM,
			ChainPEM:      result.ChainPEM,
			GatewayURL:    result.GatewayURL,
			SPKIPinSHA256: result.SPKIPinSHA256,
			SerialNumber:  result.SerialNumber,
			NotAfter:      result.NotAfter.UTC().Format(time.RFC3339),
		})
	}
}

// clientIP extracts the best-effort client IP for audit purposes. The
// chi RealIP middleware (mounted globally in httpserver) already
// rewrites RemoteAddr from X-Forwarded-For when present, so we just
// take it directly.
func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i > 0 {
		return addr[:i]
	}
	return addr
}
