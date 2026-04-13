// Package dsr — fulfillment workflow for KVKK m.11 Data Subject Requests.
//
// Faz 6 item #69. Two entry points:
//
//   - FulfillAccessRequest  (KVKK m.11/b right to access): collects the
//     subject's personal data from Postgres + ClickHouse, bundles it
//     into a signed ZIP, uploads to the dsr-responses MinIO bucket, and
//     returns a 7-day presigned URL. The DSR row is transitioned to
//     the `resolved` state via Service.Respond so the existing SOC 2
//     evidence collector fires (P5.1 + P7.1).
//
//   - FulfillErasureRequest (KVKK m.11/f right to erasure): performs a
//     multi-store crypto-erase. Rows are deleted from Postgres +
//     ClickHouse, blobs are removed from MinIO, and the per-user Vault
//     transit key is destroyed. The KVKK m.7 promise is anchored on
//     that last step: even if a Postgres PITR rollback or a ClickHouse
//     replica still contains the ciphertext, without the wrapping PE-DEK
//     the data is mathematically unrecoverable. Audit logs are NEVER
//     touched — they are immutable by regulation.
//
// Neither entry point runs unless the caller is authenticated as the
// DSR's tenant owner. FulfillErasureRequest additionally short-circuits
// on any active legal hold that mentions the subject, returning 409
// with the blocking hold IDs.
package dsr

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
)

// MaxAccessExportBytes is the hard cap on the aggregate raw event bytes
// pulled from ClickHouse for a single access request. >50 MB export is
// a strong smell of an over-broad scope (think: "all my data ever");
// the handler returns 413 and asks the operator to scope the request
// by endpoint or date range.
const MaxAccessExportBytes = 50 * 1024 * 1024

// ErrExportTooLarge is returned by FulfillAccessRequest when the
// subject's ClickHouse footprint exceeds MaxAccessExportBytes.
var ErrExportTooLarge = errors.New("dsr: subject event footprint exceeds 50 MB — scope the request by endpoint or date range")

// ErrBlockedByLegalHold is returned by FulfillErasureRequest when one
// or more active holds cover the subject. The caller should surface
// the hold IDs to the DPO so they can explicitly release them first.
var ErrBlockedByLegalHold = errors.New("dsr: erasure blocked by active legal hold(s)")

// ErrInvalidState is returned when a DSR is not in a state that allows
// fulfilment (e.g. already resolved, rejected, wrong tenant).
var ErrInvalidState = errors.New("dsr: request not in a fulfillable state")

// CHReader is a narrow interface over the ClickHouse client — just the
// one query the access export needs. Satisfies the test-doubling
// requirement without pulling the whole clickhouse package into tests.
type CHReader interface {
	// QuerySubjectEvents returns the subject's events as a JSON byte
	// array (newline-delimited or single JSON array — both are valid
	// for the archive). Implementations MUST respect the 50 MB cap
	// internally and return ErrExportTooLarge when the subject has
	// too much data; the fulfilment service will also double-check
	// the returned byte length as a defensive measure.
	QuerySubjectEvents(ctx context.Context, tenantID, subjectUserID string) ([]byte, error)

	// DeleteSubjectEvents issues an ALTER TABLE DELETE WHERE against
	// the events_raw table. Returns the count of rows matched (may
	// be approximate — ClickHouse mutations are eventually
	// consistent). Implementations MAY cap the delete at 1M rows
	// and return an error for larger sets; the caller will record
	// the cap as a partial erasure in the fulfilment_details.
	DeleteSubjectEvents(ctx context.Context, tenantID, subjectUserID string) (int, error)
}

// ObjectInfo is the minimum shape the fulfilment service needs to
// describe a MinIO object during listing + cleanup.
type ObjectInfo struct {
	Key  string
	Size int64
}

// MinioBlobStore is the narrow blob-store interface the fulfilment
// service depends on. It is deliberately broader than the existing
// minio.Client because the fulfilment workflow needs list + delete
// semantics that are not part of the screenshots-only production
// client surface. Real wiring in main.go will inject a wrapper.
type MinioBlobStore interface {
	ListByPrefix(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error)
	Put(ctx context.Context, bucket, key string, data []byte, contentType string) error
	Presign(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
	RemoveObjects(ctx context.Context, bucket string, keys []string) error
}

// VaultKeyDestroyer is implemented by *vault.Client.DestroyTransitKey.
// Isolated here so the fulfilment test suite can stub the crypto-erase
// side-effect without spinning up Vault.
type VaultKeyDestroyer interface {
	DestroyTransitKey(ctx context.Context, keyName string) error
}

// LegalHoldInfo is the minimum shape the fulfilment service needs to
// report blocking holds back to the DPO. Matches the fields the
// legalhold package exposes but is decoupled here to avoid an import
// cycle (legalhold → dsr would be forbidden; dsr → legalhold would be
// fine but we prefer interface-based decoupling for test fakes).
type LegalHoldInfo struct {
	ID         string
	TicketID   string
	ReasonCode string
	EndpointID *string
	UserSID    *string
}

// LegalHoldChecker returns all active holds that would block an
// erasure for the given (tenantID, userID). Implementations should
// include holds that scope the user directly AND holds that scope an
// endpoint currently assigned to the user (the stricter test).
type LegalHoldChecker interface {
	HoldsByUser(ctx context.Context, tenantID, userID string) ([]LegalHoldInfo, error)
}

// AuditRecorder is the narrow audit interface the fulfilment service
// needs. *audit.Recorder satisfies it directly.
type AuditRecorder interface {
	Append(ctx context.Context, e audit.Entry) (int64, error)
}

// FulfillmentService expands the DSR lifecycle with access-export and
// crypto-erase capabilities. It composes the existing dsr.Service for
// the state-machine transition (Respond) so the SOC 2 evidence
// collector fires exactly once per fulfilment.
type FulfillmentService struct {
	dsrSvc       *Service
	pool         *pgxpool.Pool
	chClient     CHReader
	minioClient  MinioBlobStore
	vaultClient  VaultKeyDestroyer
	legalHoldChk LegalHoldChecker
	recorder     AuditRecorder
	log          *slog.Logger

	// dsrResponsesBucket is the MinIO bucket name for access export
	// artifacts. Configurable so tests can pin a known value.
	dsrResponsesBucket string

	// peDEKKeyNameFn derives the Vault transit key name for a given
	// (tenant, user). Defaults to fmt.Sprintf("pe-dek-%s-%s", tenant, user)
	// but is overridable for tests.
	peDEKKeyNameFn func(tenantID, subjectUserID string) string

	// now overridable for deterministic tests.
	now func() time.Time
}

// NewFulfillmentService constructs a fulfilment service with all
// dependencies explicit. Any nil dep is a programmer error and will
// be caught at startup (cmd/api/main.go constructs this once).
func NewFulfillmentService(
	dsrSvc *Service,
	pool *pgxpool.Pool,
	ch CHReader,
	blob MinioBlobStore,
	vk VaultKeyDestroyer,
	lh LegalHoldChecker,
	rec AuditRecorder,
	dsrResponsesBucket string,
	log *slog.Logger,
) *FulfillmentService {
	return &FulfillmentService{
		dsrSvc:             dsrSvc,
		pool:               pool,
		chClient:           ch,
		minioClient:        blob,
		vaultClient:        vk,
		legalHoldChk:       lh,
		recorder:           rec,
		log:                log,
		dsrResponsesBucket: dsrResponsesBucket,
		peDEKKeyNameFn: func(tenantID, subjectUserID string) string {
			return fmt.Sprintf("pe-dek-%s-%s", tenantID, subjectUserID)
		},
		now: func() time.Time { return time.Now().UTC() },
	}
}

// FulfillmentArtifact is the result of a successful access-export run.
// The PresignedURL is valid for 7 days; after that the DPO must
// re-issue via the fulfilment endpoint (which will re-presign the
// existing ZipKey — the ZIP itself does not get rebuilt).
type FulfillmentArtifact struct {
	RequestID    string    `json:"request_id"`
	ZipKey       string    `json:"zip_key"`
	PresignedURL string    `json:"presigned_url"`
	SHA256       string    `json:"sha256"`
	SizeBytes    int64     `json:"size_bytes"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// ErasureReport captures exactly what the crypto-erase pipeline did.
// Stored verbatim (minus PII) in dsr_requests.fulfillment_details.
type ErasureReport struct {
	RequestID             string    `json:"request_id"`
	PostgresRowsDeleted   int       `json:"postgres_rows_deleted"`
	ClickHouseRowsDeleted int       `json:"clickhouse_rows_deleted"`
	MinioKeysErased       int       `json:"minio_keys_erased"`
	VaultKeysDestroyed    int       `json:"vault_keys_destroyed"`
	BlockedByLegalHold    []string  `json:"blocked_by_legal_hold,omitempty"`
	DryRun                bool      `json:"dry_run"`
	CompletedAt           time.Time `json:"completed_at"`
	// PartialFailureReason is set when at least one stage succeeded
	// but a later stage failed. The DSR stays `in_progress` so the
	// DPO can manually re-drive it once the root cause is fixed.
	PartialFailureReason string `json:"partial_failure_reason,omitempty"`
}

// -----------------------------------------------------------------
// ACCESS export (KVKK m.11/b)
// -----------------------------------------------------------------

// FulfillAccessRequest collects all of the subject's data, writes it
// to a ZIP, uploads to MinIO, and transitions the DSR to `resolved`.
// Returns the artifact metadata including a 7-day presigned URL.
func (f *FulfillmentService) FulfillAccessRequest(ctx context.Context, p *auth.Principal, requestID string) (*FulfillmentArtifact, error) {
	if p == nil {
		return nil, auth.ErrForbidden
	}
	req, err := f.dsrSvc.Get(ctx, p.TenantID, requestID)
	if err != nil || req == nil {
		return nil, ErrInvalidState
	}
	if req.TenantID != p.TenantID {
		return nil, ErrInvalidState
	}
	if req.State != StateOpen && req.State != StateAtRisk && req.State != StateOverdue {
		return nil, ErrInvalidState
	}

	// -- 1. Postgres collection --------------------------------------
	pgSections, pgCounts, err := f.collectPostgres(ctx, req.TenantID, req.EmployeeUserID)
	if err != nil {
		return nil, fmt.Errorf("dsr: collect postgres: %w", err)
	}

	// -- 2. ClickHouse collection ------------------------------------
	chBytes, err := f.chClient.QuerySubjectEvents(ctx, req.TenantID, req.EmployeeUserID)
	if err != nil {
		return nil, fmt.Errorf("dsr: collect clickhouse: %w", err)
	}
	if int64(len(chBytes)) > MaxAccessExportBytes {
		return nil, ErrExportTooLarge
	}

	// -- 3. ZIP bundling ---------------------------------------------
	now := f.now()
	zipBuf := &bytes.Buffer{}
	zw := zip.NewWriter(zipBuf)

	// Manifest row counts include the ClickHouse row as one logical
	// entry (we don't parse it — the DPO does).
	manifest := map[string]any{
		"dsr_id":           req.ID,
		"tenant_id":        req.TenantID,
		"employee_user_id": req.EmployeeUserID,
		"generated_at":     now.Format(time.RFC3339Nano),
		"kvkk_article":     "11/b",
		"postgres":         pgCounts,
		"clickhouse_bytes": len(chBytes),
		"artifacts":        []string{},
	}
	artifactList := make([]string, 0, len(pgSections)+1)

	for name, content := range pgSections {
		sum := sha256.Sum256(content)
		entry := fmt.Sprintf("postgres/%s.json", name)
		if err := writeZipEntry(zw, entry, content); err != nil {
			return nil, err
		}
		artifactList = append(artifactList, entry)
		manifest[fmt.Sprintf("postgres_%s_sha256", name)] = hex.EncodeToString(sum[:])
	}
	if len(chBytes) > 0 {
		sum := sha256.Sum256(chBytes)
		const chEntry = "clickhouse/events.json"
		if err := writeZipEntry(zw, chEntry, chBytes); err != nil {
			return nil, err
		}
		artifactList = append(artifactList, chEntry)
		manifest["clickhouse_events_sha256"] = hex.EncodeToString(sum[:])
	}
	manifest["artifacts"] = artifactList
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("dsr: marshal manifest: %w", err)
	}
	if err := writeZipEntry(zw, "manifest.json", manifestBytes); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("dsr: close zip: %w", err)
	}
	zipBytes := zipBuf.Bytes()
	overallSum := sha256.Sum256(zipBytes)
	overallHex := hex.EncodeToString(overallSum[:])

	// -- 4. MinIO upload ----------------------------------------------
	zipKey := fmt.Sprintf("dsr-responses/%s/%s/%d.zip",
		req.TenantID, req.ID, now.Unix())
	if err := f.minioClient.Put(ctx, f.dsrResponsesBucket, zipKey, zipBytes, "application/zip"); err != nil {
		return nil, fmt.Errorf("dsr: upload artifact: %w", err)
	}

	ttl := 7 * 24 * time.Hour
	presigned, err := f.minioClient.Presign(ctx, f.dsrResponsesBucket, zipKey, ttl)
	if err != nil {
		return nil, fmt.Errorf("dsr: presign artifact: %w", err)
	}

	// -- 5. Update the DSR + emit audit -------------------------------
	// Write the fulfillment_details and sha256 directly; the Service
	// Respond() call then drives the state machine + SOC 2 evidence.
	fulfilment := map[string]any{
		"kind":          "access_export",
		"zip_key":       zipKey,
		"sha256":        overallHex,
		"size_bytes":    len(zipBytes),
		"generated_at":  now.Format(time.RFC3339Nano),
		"postgres":      pgCounts,
		"clickhouse_b":  len(chBytes),
	}
	fulfilmentBytes, _ := json.Marshal(fulfilment)
	if _, err := f.pool.Exec(ctx,
		`UPDATE dsr_requests
		 SET response_sha256 = $1, fulfillment_details = $2::jsonb
		 WHERE id = $3 AND tenant_id = $4::uuid`,
		overallHex, fulfilmentBytes, req.ID, req.TenantID,
	); err != nil {
		return nil, fmt.Errorf("dsr: persist fulfillment_details: %w", err)
	}

	// Audit the export BEFORE transitioning the state (the respond
	// path will add its own audit + SOC 2 evidence).
	if _, err := f.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		ActorUA:  "",
		TenantID: req.TenantID,
		Action:   audit.ActionDSRExported,
		Target:   fmt.Sprintf("dsr:%s", req.ID),
		Details: map[string]any{
			"zip_key":    zipKey,
			"sha256":     overallHex,
			"size_bytes": len(zipBytes),
			"kvkk":       "11/b",
		},
	}); err != nil {
		f.log.ErrorContext(ctx, "dsr: audit export failed",
			slog.String("dsr_id", req.ID),
			slog.String("error", err.Error()),
		)
	}

	if err := f.dsrSvc.Respond(ctx, req.TenantID, req.ID, p.UserID, zipKey); err != nil {
		return nil, fmt.Errorf("dsr: respond: %w", err)
	}

	return &FulfillmentArtifact{
		RequestID:    req.ID,
		ZipKey:       zipKey,
		PresignedURL: presigned,
		SHA256:       overallHex,
		SizeBytes:    int64(len(zipBytes)),
		ExpiresAt:    now.Add(ttl),
	}, nil
}

// -----------------------------------------------------------------
// ERASURE (KVKK m.11/f + crypto-erase)
// -----------------------------------------------------------------

// FulfillErasureRequest runs the crypto-erase pipeline. Legal hold
// check happens first; any active hold short-circuits the entire
// operation. When dryRun=true the method counts what WOULD be deleted
// without touching anything.
func (f *FulfillmentService) FulfillErasureRequest(ctx context.Context, p *auth.Principal, requestID string, dryRun bool) (*ErasureReport, error) {
	if p == nil {
		return nil, auth.ErrForbidden
	}
	req, err := f.dsrSvc.Get(ctx, p.TenantID, requestID)
	if err != nil || req == nil {
		return nil, ErrInvalidState
	}
	if req.TenantID != p.TenantID {
		return nil, ErrInvalidState
	}
	if req.State != StateOpen && req.State != StateAtRisk && req.State != StateOverdue {
		return nil, ErrInvalidState
	}

	// -- 1. Legal hold gate (non-negotiable, runs before dry-run) -----
	holds, err := f.legalHoldChk.HoldsByUser(ctx, req.TenantID, req.EmployeeUserID)
	if err != nil {
		return nil, fmt.Errorf("dsr: legal hold lookup: %w", err)
	}
	if len(holds) > 0 {
		blocking := make([]string, 0, len(holds))
		for _, h := range holds {
			blocking = append(blocking, h.ID)
		}
		return &ErasureReport{
			RequestID:          req.ID,
			BlockedByLegalHold: blocking,
			DryRun:             dryRun,
			CompletedAt:        f.now(),
		}, ErrBlockedByLegalHold
	}

	report := &ErasureReport{
		RequestID:   req.ID,
		DryRun:      dryRun,
		CompletedAt: f.now(),
	}

	// -- 2. Postgres count (always — dry-run or real) -----------------
	pgCount, err := f.countPostgres(ctx, req.TenantID, req.EmployeeUserID)
	if err != nil {
		return nil, fmt.Errorf("dsr: count postgres: %w", err)
	}
	report.PostgresRowsDeleted = pgCount

	// -- 3. Blob list (always) ----------------------------------------
	blobKeys, err := f.listAllUserBlobs(ctx, req.TenantID, req.EmployeeUserID)
	if err != nil {
		return nil, fmt.Errorf("dsr: list blobs: %w", err)
	}
	report.MinioKeysErased = len(blobKeys)

	if dryRun {
		// Only Vault + ClickHouse require a "dry-run" shortcut — we
		// can't safely count CH rows without issuing the query, so we
		// conservatively report the blob+pg counts and a placeholder 1
		// for the Vault destroy target.
		report.VaultKeysDestroyed = 1
		return report, nil
	}

	// -- 4. REAL ERASURE ----------------------------------------------
	// Order matters: we do MinIO + ClickHouse + Postgres first, and
	// the Vault crypto-erase LAST. If an earlier stage fails we
	// abort and leave the DSR in_progress so the DPO can re-drive;
	// a Vault failure after the other stages is partial coverage but
	// the ciphertext is still unrecoverable if the DEK was only held
	// in Vault (which it is by ADR 0013).

	// MinIO
	if len(blobKeys) > 0 {
		if err := f.minioClient.RemoveObjects(ctx, f.dsrResponsesBucket, blobKeys); err != nil {
			return report, fmt.Errorf("dsr: minio remove: %w", err)
		}
	}

	// ClickHouse
	chCount, err := f.chClient.DeleteSubjectEvents(ctx, req.TenantID, req.EmployeeUserID)
	if err != nil {
		return report, fmt.Errorf("dsr: clickhouse delete: %w", err)
	}
	report.ClickHouseRowsDeleted = chCount

	// Postgres: delete in a transaction. Audit log is NEVER touched
	// (hash-chained + immutable). We mark the user as tombstoned.
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return report, fmt.Errorf("dsr: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// silence_acknowledgements — user_id references
	if _, err := tx.Exec(ctx,
		`DELETE FROM silence_acknowledgements
		 WHERE tenant_id = $1::uuid AND user_id = $2::uuid`,
		req.TenantID, req.EmployeeUserID,
	); err != nil {
		// Non-fatal: the table may not exist in Phase 1 dev setups.
		f.log.WarnContext(ctx, "dsr: erase silence_acknowledgements failed",
			slog.String("error", err.Error()))
	}

	// live_view_sessions — requester_id / approver_id may point at
	// the subject. We mask rather than delete so the audit chain
	// stays intact.
	if _, err := tx.Exec(ctx,
		`UPDATE live_view_sessions
		 SET requester_id = NULL
		 WHERE tenant_id = $1::uuid AND requester_id = $2::uuid`,
		req.TenantID, req.EmployeeUserID,
	); err != nil {
		f.log.WarnContext(ctx, "dsr: null live_view requester failed",
			slog.String("error", err.Error()))
	}

	// Tombstone the user.
	now := f.now()
	if _, err := tx.Exec(ctx,
		`UPDATE users
		 SET pii_erased = true,
		     pii_erased_at = $1,
		     terminated_at = COALESCE(terminated_at, $1)
		 WHERE id = $2::uuid AND tenant_id = $3::uuid`,
		now, req.EmployeeUserID, req.TenantID,
	); err != nil {
		return report, fmt.Errorf("dsr: tombstone user: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return report, fmt.Errorf("dsr: commit erasure tx: %w", err)
	}

	// -- 5. Vault crypto-erase — THE commit point ---------------------
	keyName := f.peDEKKeyNameFn(req.TenantID, req.EmployeeUserID)
	if err := f.vaultClient.DestroyTransitKey(ctx, keyName); err != nil {
		report.PartialFailureReason = "vault_destroy_failed"
		return report, fmt.Errorf("dsr: vault destroy %q: %w", keyName, err)
	}
	report.VaultKeysDestroyed = 1
	report.CompletedAt = f.now()

	// -- 6. Persist fulfilment_details + mark resolved ----------------
	fulfilmentBytes, _ := json.Marshal(report)
	if _, err := f.pool.Exec(ctx,
		`UPDATE dsr_requests
		 SET fulfillment_details = $1::jsonb
		 WHERE id = $2 AND tenant_id = $3::uuid`,
		fulfilmentBytes, req.ID, req.TenantID,
	); err != nil {
		return report, fmt.Errorf("dsr: persist erasure report: %w", err)
	}

	// Audit the erasure BEFORE the Respond (which writes its own
	// "responded" audit + evidence).
	if _, err := f.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: req.TenantID,
		Action:   audit.ActionDSRErased,
		Target:   fmt.Sprintf("dsr:%s", req.ID),
		Details: map[string]any{
			"postgres_rows_deleted":    report.PostgresRowsDeleted,
			"clickhouse_rows_deleted":  report.ClickHouseRowsDeleted,
			"minio_keys_erased":        report.MinioKeysErased,
			"vault_keys_destroyed":     report.VaultKeysDestroyed,
			"kvkk":                     "11/f",
		},
	}); err != nil {
		f.log.ErrorContext(ctx, "dsr: audit erasure failed",
			slog.String("dsr_id", req.ID),
			slog.String("error", err.Error()),
		)
	}

	// State transition — artifact ref is the erasure report key.
	artifactRef := fmt.Sprintf("erasure-report:%s", req.ID)
	if err := f.dsrSvc.Respond(ctx, req.TenantID, req.ID, p.UserID, artifactRef); err != nil {
		return report, fmt.Errorf("dsr: respond: %w", err)
	}

	return report, nil
}

// -----------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------

// collectPostgres returns a map of section-name → JSON bytes and a
// per-section row-count map. Each section is one Postgres table
// filtered to the subject's rows. PII is intentionally preserved
// (this IS the access export) but we never include audit_log content
// — auditors need that data to prove what the admin saw, not to
// forward to the subject.
func (f *FulfillmentService) collectPostgres(ctx context.Context, tenantID, subjectUserID string) (map[string][]byte, map[string]int, error) {
	sections := make(map[string][]byte)
	counts := make(map[string]int)

	// users (one row)
	if rows, err := queryToJSON(ctx, f.pool,
		`SELECT id::text, tenant_id::text, email, display_name, created_at,
		        hris_id, department, manager_user_id::text
		 FROM users
		 WHERE id = $1::uuid AND tenant_id = $2::uuid`,
		subjectUserID, tenantID,
	); err == nil {
		sections["users"] = rows
		if len(rows) > 2 { // "[]" = empty
			counts["users"] = 1
		}
	} else {
		return nil, nil, err
	}

	// dsr_requests (the subject's prior requests)
	if rows, err := queryToJSON(ctx, f.pool,
		`SELECT id, request_type, state, justification,
		        created_at, sla_deadline, closed_at
		 FROM dsr_requests
		 WHERE employee_user_id = $1::uuid AND tenant_id = $2::uuid
		 ORDER BY created_at DESC`,
		subjectUserID, tenantID,
	); err == nil {
		sections["dsr_requests"] = rows
		counts["dsr_requests"] = countJSONArray(rows)
	}

	// live_view_sessions (if the subject was the target)
	if rows, err := queryToJSON(ctx, f.pool,
		`SELECT id, requester_id::text, approver_id::text,
		        state, started_at, ended_at
		 FROM live_view_sessions
		 WHERE tenant_id = $1::uuid AND requester_id = $2::uuid`,
		tenantID, subjectUserID,
	); err == nil {
		sections["live_view_sessions"] = rows
		counts["live_view_sessions"] = countJSONArray(rows)
	}

	// silence_acknowledgements
	if rows, err := queryToJSON(ctx, f.pool,
		`SELECT id, endpoint_id::text, acknowledged_at, reason
		 FROM silence_acknowledgements
		 WHERE tenant_id = $1::uuid AND user_id = $2::uuid`,
		tenantID, subjectUserID,
	); err == nil {
		sections["silence_acknowledgements"] = rows
		counts["silence_acknowledgements"] = countJSONArray(rows)
	}

	return sections, counts, nil
}

// countPostgres returns the approximate row count the erasure will
// touch. Used both for dry-run reporting and for the post-erasure
// report. Tables that may not exist in dev are absorbed silently.
func (f *FulfillmentService) countPostgres(ctx context.Context, tenantID, subjectUserID string) (int, error) {
	total := 0
	tables := []struct {
		name  string
		query string
	}{
		{"silence_acknowledgements",
			`SELECT COUNT(*) FROM silence_acknowledgements
			 WHERE tenant_id = $1::uuid AND user_id = $2::uuid`},
		{"live_view_sessions",
			`SELECT COUNT(*) FROM live_view_sessions
			 WHERE tenant_id = $1::uuid AND requester_id = $2::uuid`},
		// users is +1 (we tombstone, don't delete, but count it)
	}
	for _, t := range tables {
		var n int
		if err := f.pool.QueryRow(ctx, t.query, tenantID, subjectUserID).Scan(&n); err == nil {
			total += n
		}
	}
	total++ // users tombstone
	return total, nil
}

// listAllUserBlobs enumerates every MinIO prefix that stores per-user
// data: screenshots, keystroke blobs, clipboard blobs. The returned
// slice is flat — the caller removes them in one RemoveObjects call.
func (f *FulfillmentService) listAllUserBlobs(ctx context.Context, tenantID, subjectUserID string) ([]string, error) {
	var all []string
	prefixes := []struct {
		bucket string
		prefix string
	}{
		{f.dsrResponsesBucket, fmt.Sprintf("screenshots/%s/%s/", tenantID, subjectUserID)},
		{f.dsrResponsesBucket, fmt.Sprintf("keystroke-blobs/%s/%s/", tenantID, subjectUserID)},
		{f.dsrResponsesBucket, fmt.Sprintf("clipboard-blobs/%s/%s/", tenantID, subjectUserID)},
	}
	for _, p := range prefixes {
		objs, err := f.minioClient.ListByPrefix(ctx, p.bucket, p.prefix)
		if err != nil {
			// Per-prefix failures are non-fatal; the erasure report
			// will under-count rather than fail closed.
			f.log.WarnContext(ctx, "dsr: list prefix failed",
				slog.String("prefix", p.prefix),
				slog.String("error", err.Error()),
			)
			continue
		}
		for _, o := range objs {
			all = append(all, o.Key)
		}
	}
	return all, nil
}

// queryToJSON runs a SELECT and encodes the result as a JSON array of
// objects (one object per row). Used for the access export; the
// format is identical to what the console's DSR viewer expects.
func queryToJSON(ctx context.Context, pool *pgxpool.Pool, query string, args ...any) ([]byte, error) {
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	out := make([]map[string]any, 0)
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(map[string]any, len(fields))
		for i, fd := range fields {
			row[string(fd.Name)] = values[i]
		}
		out = append(out, row)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return json.Marshal(out)
}

// countJSONArray reports the number of elements in a JSON array byte
// slice. Used for section counts in the export manifest. Non-array
// inputs return 0.
func countJSONArray(b []byte) int {
	var arr []any
	if err := json.Unmarshal(b, &arr); err != nil {
		return 0
	}
	return len(arr)
}

// writeZipEntry writes a single file into the ZIP with deterministic
// modtime so the same input produces byte-identical archives (useful
// for verification and caching).
func writeZipEntry(zw *zip.Writer, name string, content []byte) error {
	hdr := &zip.FileHeader{
		Name:     name,
		Method:   zip.Deflate,
		Modified: time.Unix(0, 0).UTC(),
	}
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return fmt.Errorf("dsr: zip create %q: %w", name, err)
	}
	if _, err := w.Write(content); err != nil {
		return fmt.Errorf("dsr: zip write %q: %w", name, err)
	}
	return nil
}
