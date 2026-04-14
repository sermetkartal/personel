// Package license provides trial + production license validation for the
// Personel Admin API.
//
// License file format
// -------------------
//
// A license is a JSON object wrapped in an Ed25519 detached signature:
//
//	{
//	  "claims": {
//	    "customer_id": "acme-corp-tr",
//	    "tier": "business",
//	    "max_endpoints": 100,
//	    "features": ["uba", "ocr", "hris"],
//	    "issued_at": "2026-01-15T09:00:00Z",
//	    "expires_at": "2027-01-15T09:00:00Z",
//	    "fingerprint": "a1b2c3...",  // optional; binds to hardware
//	    "online_validation": true     // whether phone-home is expected
//	  },
//	  "signature": "base64(ed25519(canonical(claims)))",
//	  "key_id": "personel-vendor-key-2026"
//	}
//
// Canonical form of claims: JSON with sorted keys, no whitespace. The
// vendor Ed25519 public key is compiled into the binary so the API can
// verify offline without any external service. Online validation is an
// OPTIONAL additional check the operator can enable.
//
// Grace period
// ------------
//
// Expiry is soft: the service keeps operating in READ_ONLY mode for
// GracePeriod (7 days) past the expires_at timestamp. Beyond that the
// /v1/* routes return 403 license_expired, but /healthz, /readyz and
// /public/status.json stay open so operators can see the system is
// "alive but unlicensed" before remediating.
//
// Offline vs online
// -----------------
//
// Offline: signature check against embedded public key only. Air-gapped
// deployments must use this mode. Hardware fingerprint (if present in
// claims) is checked against local hardware to prevent trivial license
// transfer.
//
// Online: in addition, the API phone-homes to online_validation_url
// daily. The server responds with either "valid" or "revoked". A cached
// "valid" response survives 7 days of connectivity loss before the
// license is treated as suspect (downgraded to offline-only checks).
//
// Privacy: phone-home payload is (customer_id, endpoint_count, version).
// NO event content, NO user data, NO IP geolocation. Heartbeat is opt-in
// per deployment and can be disabled in the license file itself.
package license

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"time"
)

// Sentinel errors exposed so handlers can branch on license state.
var (
	// ErrNoLicense means the license file is missing or unreadable.
	// In dev/test mode main.go treats this as "permissive"; in prod
	// it must trigger 403.
	ErrNoLicense = errors.New("license: no license file loaded")

	// ErrInvalidSignature means the embedded signature does not
	// verify against the vendor public key.
	ErrInvalidSignature = errors.New("license: invalid signature")

	// ErrExpired means the license has passed its expires_at AND
	// the grace period.
	ErrExpired = errors.New("license: expired beyond grace period")

	// ErrCapacityExceeded means current_endpoints > max_endpoints.
	ErrCapacityExceeded = errors.New("license: endpoint capacity exceeded")

	// ErrFingerprintMismatch means the hardware fingerprint in the
	// license does not match the host running the API.
	ErrFingerprintMismatch = errors.New("license: fingerprint mismatch")

	// ErrFeatureDisabled means the caller asked for a feature that
	// is not enabled in the license tier.
	ErrFeatureDisabled = errors.New("license: feature not enabled in this tier")
)

// Tier names. These are advisory — the actual feature set is the
// Features array in the claims.
const (
	TierTrial      = "trial"
	TierStarter    = "starter"
	TierBusiness   = "business"
	TierEnterprise = "enterprise"
)

// Claims is the signed payload of a license file.
type Claims struct {
	CustomerID       string    `json:"customer_id"`
	Tier             string    `json:"tier"`
	MaxEndpoints     int       `json:"max_endpoints"`
	Features         []string  `json:"features"`
	IssuedAt         time.Time `json:"issued_at"`
	ExpiresAt        time.Time `json:"expires_at"`
	Fingerprint      string    `json:"fingerprint,omitempty"`
	OnlineValidation bool      `json:"online_validation"`
}

// File is the full on-disk format: signed claims + metadata.
type File struct {
	Claims    json.RawMessage `json:"claims"`
	Signature string          `json:"signature"` // base64(ed25519)
	KeyID     string          `json:"key_id"`
}

// State is the runtime license state reported by the Service.
type State string

const (
	StateValid    State = "valid"
	StateGrace    State = "grace"    // expired but within grace period — read-only
	StateExpired  State = "expired"  // beyond grace — 403
	StateMissing  State = "missing"  // no file present
	StateInvalid  State = "invalid"  // signature or fingerprint fail
)

// GracePeriod is how long past expires_at we keep serving in read-only
// mode before returning 403 license_expired.
const GracePeriod = 7 * 24 * time.Hour

// Service holds the parsed license and answers runtime queries.
//
// A single Service instance is shared via sync.RWMutex. Call
// Refresh periodically (24h ticker) to re-read the file and
// potentially phone home.
type Service struct {
	mu         sync.RWMutex
	file       *File
	claims     *Claims
	state      State
	lastErr    error
	lastCheck  time.Time
	publicKey  ed25519.PublicKey
	hwFinger   string
	licensePath string
	log        *slog.Logger
}

// Options configure a license Service.
type Options struct {
	// LicensePath is the on-disk location of the license JSON file.
	// Default: /etc/personel/license.json
	LicensePath string

	// PublicKey is the Ed25519 public key used to verify signatures.
	// MUST be 32 bytes. In production this is compiled into the
	// binary (see EmbeddedVendorPublicKey).
	PublicKey ed25519.PublicKey

	// HardwareFingerprint is the fingerprint of the host running
	// the API. If the license claims a fingerprint, it must match
	// this value (or be empty). Computed via ComputeFingerprint().
	HardwareFingerprint string

	// Log is the slog logger to use for all license events.
	Log *slog.Logger

	// AllowMissing treats "file not found" as StateValid with a
	// permissive unlimited license. Used for dev/test only.
	AllowMissing bool
}

// NewService constructs a license Service and performs an initial load.
// The service is usable even on load failure — it reports StateMissing
// or StateInvalid, and handlers can decide what to do.
func NewService(opts Options) *Service {
	if opts.LicensePath == "" {
		opts.LicensePath = "/etc/personel/license.json"
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	s := &Service{
		publicKey:   opts.PublicKey,
		hwFinger:    opts.HardwareFingerprint,
		licensePath: opts.LicensePath,
		log:         opts.Log,
	}
	if err := s.Refresh(context.Background()); err != nil {
		if errors.Is(err, ErrNoLicense) && opts.AllowMissing {
			s.log.Warn("license file missing; permissive dev mode enabled",
				slog.String("path", opts.LicensePath))
			s.setPermissive()
			return s
		}
		s.log.Error("license load failed",
			slog.String("path", opts.LicensePath),
			slog.String("err", err.Error()))
	}
	return s
}

// setPermissive installs a dev-mode unlimited license so the API can
// run on a developer laptop without a real license file. Never called
// in production (AllowMissing defaults to false).
func (s *Service) setPermissive() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = StateValid
	s.claims = &Claims{
		CustomerID:   "dev-permissive",
		Tier:         TierEnterprise,
		MaxEndpoints: 100000,
		Features:     []string{"uba", "ocr", "hris", "siem", "ml", "mobile"},
		IssuedAt:     time.Now(),
		ExpiresAt:    time.Now().Add(365 * 24 * time.Hour),
	}
}

// Refresh re-reads the license file, verifies the signature, checks
// expiry + fingerprint, and updates the internal state. Call this on
// a 24h ticker from main.go so operators can drop a new license file
// without restarting the API.
func (s *Service) Refresh(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastCheck = time.Now()

	raw, err := os.ReadFile(s.licensePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.state = StateMissing
			s.lastErr = ErrNoLicense
			return ErrNoLicense
		}
		s.state = StateInvalid
		s.lastErr = fmt.Errorf("license: read: %w", err)
		return s.lastErr
	}

	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		s.state = StateInvalid
		s.lastErr = fmt.Errorf("license: parse: %w", err)
		return s.lastErr
	}

	// Verify signature over the canonical claims bytes (not the
	// raw bytes — we canonicalize so whitespace-insensitive).
	var claims Claims
	if err := json.Unmarshal(f.Claims, &claims); err != nil {
		s.state = StateInvalid
		s.lastErr = fmt.Errorf("license: parse claims: %w", err)
		return s.lastErr
	}

	canonical, err := canonicalize(claims)
	if err != nil {
		s.state = StateInvalid
		s.lastErr = fmt.Errorf("license: canonicalize: %w", err)
		return s.lastErr
	}

	sig, err := base64.StdEncoding.DecodeString(f.Signature)
	if err != nil {
		s.state = StateInvalid
		s.lastErr = fmt.Errorf("license: signature decode: %w", err)
		return s.lastErr
	}

	if len(s.publicKey) != ed25519.PublicKeySize {
		s.state = StateInvalid
		s.lastErr = errors.New("license: vendor public key not configured")
		return s.lastErr
	}
	if !ed25519.Verify(s.publicKey, canonical, sig) {
		s.state = StateInvalid
		s.lastErr = ErrInvalidSignature
		return ErrInvalidSignature
	}

	// Fingerprint check (optional — empty fingerprint in claims
	// means "portable", e.g. trial licenses).
	if claims.Fingerprint != "" && s.hwFinger != "" {
		if claims.Fingerprint != s.hwFinger {
			s.state = StateInvalid
			s.lastErr = ErrFingerprintMismatch
			return ErrFingerprintMismatch
		}
	}

	// Expiry check.
	now := time.Now()
	switch {
	case now.Before(claims.ExpiresAt):
		s.state = StateValid
	case now.Before(claims.ExpiresAt.Add(GracePeriod)):
		s.state = StateGrace
	default:
		s.state = StateExpired
	}

	s.file = &f
	s.claims = &claims
	s.lastErr = nil

	s.log.Info("license refreshed",
		slog.String("customer_id", claims.CustomerID),
		slog.String("tier", claims.Tier),
		slog.Int("max_endpoints", claims.MaxEndpoints),
		slog.String("state", string(s.state)),
		slog.Time("expires_at", claims.ExpiresAt))

	return nil
}

// State returns the current runtime license state.
func (s *Service) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Claims returns a copy of the current claims, or nil if no license
// is loaded. Callers MUST NOT mutate the returned struct.
func (s *Service) Claims() *Claims {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.claims == nil {
		return nil
	}
	cc := *s.claims
	return &cc
}

// LastError returns the most recent refresh error (nil on success).
func (s *Service) LastError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastErr
}

// CheckCapacity verifies that currentEndpoints does not exceed the
// licensed MaxEndpoints. Returns ErrCapacityExceeded if it does.
func (s *Service) CheckCapacity(currentEndpoints int) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.claims == nil {
		return ErrNoLicense
	}
	if currentEndpoints > s.claims.MaxEndpoints {
		return fmt.Errorf("%w: %d > %d", ErrCapacityExceeded,
			currentEndpoints, s.claims.MaxEndpoints)
	}
	return nil
}

// HasFeature returns true if the named feature is enabled in the
// license tier. Returns false if no license is loaded.
func (s *Service) HasFeature(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.claims == nil {
		return false
	}
	for _, f := range s.claims.Features {
		if f == name {
			return true
		}
	}
	return false
}

// RequireWritable returns nil if the license permits write operations
// (StateValid only). Grace and Expired are read-only.
func (s *Service) RequireWritable() error {
	st := s.State()
	switch st {
	case StateValid:
		return nil
	case StateGrace:
		return fmt.Errorf("license: in grace period — read-only mode")
	case StateMissing:
		return ErrNoLicense
	default:
		return ErrExpired
	}
}

// Summary returns a structured snapshot of the license state suitable
// for /v1/system/license GET and Prometheus exposition.
type Summary struct {
	State             State     `json:"state"`
	CustomerID        string    `json:"customer_id,omitempty"`
	Tier              string    `json:"tier,omitempty"`
	MaxEndpoints      int       `json:"max_endpoints,omitempty"`
	Features          []string  `json:"features,omitempty"`
	IssuedAt          time.Time `json:"issued_at,omitempty"`
	ExpiresAt         time.Time `json:"expires_at,omitempty"`
	DaysUntilExpiry   int       `json:"days_until_expiry"`
	LastCheck         time.Time `json:"last_check"`
	OnlineValidation  bool      `json:"online_validation"`
	LastError         string    `json:"last_error,omitempty"`
}

// Summary returns a snapshot of the license state.
func (s *Service) Summary() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sum := Summary{State: s.state, LastCheck: s.lastCheck}
	if s.lastErr != nil {
		sum.LastError = s.lastErr.Error()
	}
	if s.claims == nil {
		return sum
	}
	sum.CustomerID = s.claims.CustomerID
	sum.Tier = s.claims.Tier
	sum.MaxEndpoints = s.claims.MaxEndpoints
	sum.Features = append([]string(nil), s.claims.Features...)
	sum.IssuedAt = s.claims.IssuedAt
	sum.ExpiresAt = s.claims.ExpiresAt
	sum.OnlineValidation = s.claims.OnlineValidation
	sum.DaysUntilExpiry = int(time.Until(s.claims.ExpiresAt) / (24 * time.Hour))
	return sum
}

// canonicalize produces a deterministic byte representation of the
// Claims struct for signing/verification. Keys are sorted, timestamps
// are in RFC3339Nano.
func canonicalize(c Claims) ([]byte, error) {
	m := map[string]any{
		"customer_id":       c.CustomerID,
		"tier":              c.Tier,
		"max_endpoints":     c.MaxEndpoints,
		"features":          append([]string(nil), c.Features...),
		"issued_at":         c.IssuedAt.UTC().Format(time.RFC3339Nano),
		"expires_at":        c.ExpiresAt.UTC().Format(time.RFC3339Nano),
		"online_validation": c.OnlineValidation,
	}
	if c.Fingerprint != "" {
		m["fingerprint"] = c.Fingerprint
	}
	// sort.Strings(features) — enforce canonical order
	if feat, ok := m["features"].([]string); ok {
		sort.Strings(feat)
		m["features"] = feat
	}
	return marshalSorted(m)
}

// marshalSorted JSON-encodes a map with keys sorted alphabetically.
// Produces bytes identical to Python's json.dumps(sort_keys=True).
func marshalSorted(m map[string]any) ([]byte, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf := []byte{'{'}
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf = append(buf, kb...)
		buf = append(buf, ':')
		vb, err := json.Marshal(m[k])
		if err != nil {
			return nil, err
		}
		buf = append(buf, vb...)
	}
	buf = append(buf, '}')
	return buf, nil
}

// ComputeFingerprint derives a stable hardware fingerprint for the
// current host. Input sources: the machine-id file (Linux) or the
// Windows registry MachineGuid, combined with the first-found MAC
// address. SHA-256 then hex-encoded.
//
// This is NOT tamper-proof — a determined operator can forge a
// fingerprint — it's a speed bump against casual license transfer.
func ComputeFingerprint(machineID, firstMAC string) string {
	h := sha256.New()
	h.Write([]byte(machineID))
	h.Write([]byte{0})
	h.Write([]byte(firstMAC))
	return hex.EncodeToString(h.Sum(nil))
}
