// Package audit implements the hash-chained audit log recorder, verifier,
// and WORM (Write Once Read Many) sink for the Personel platform.
//
// This file implements the WORMSink, which writes daily audit chain
// checkpoints to a MinIO bucket configured with S3 Object Lock in
// Compliance Mode. Objects written to this bucket cannot be deleted or
// modified until the retention period expires, even by the MinIO root
// account. See docs/adr/0014-worm-audit-sink.md for the design rationale.
package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// WORMBucket is the name of the MinIO Object Lock bucket used as the WORM
// audit sink. This bucket must have been created with Object Lock enabled
// (see infra/compose/minio/worm-bucket-init.sh). The name is fixed — it is
// not configurable because changing the sink bucket name after deployment
// would silently break the tamper-evidence chain.
const WORMBucket = "audit-worm"

// wormRetentionYears is the minimum retention period imposed on every object
// written to the WORM bucket. 5 years matches KVKK Article 7 requirements for
// audit records subject to regulatory inquiry.
const wormRetentionYears = 5

// CheckpointRecord is the payload written to the WORM sink for each daily
// audit checkpoint. It mirrors audit.audit_checkpoint but is stored as
// self-describing JSON so it remains interpretable without the Postgres schema.
type CheckpointRecord struct {
	// SchemaVersion allows future tooling to detect and handle format changes.
	SchemaVersion int `json:"schema_version"`

	// TenantID identifies the tenant this checkpoint belongs to.
	TenantID string `json:"tenant_id"`

	// Day is the UTC date covered by this checkpoint (YYYY-MM-DD).
	Day string `json:"day"`

	// LastID is the highest audit_log.id included in this checkpoint.
	LastID int64 `json:"last_id"`

	// LastHash is the hex-encoded SHA-256 hash of the last audit record in
	// this checkpoint's coverage range. This is the value that the verifier
	// compares against the stored Postgres chain head.
	LastHash string `json:"last_hash"`

	// EntryCount is the number of audit records covered by this checkpoint.
	EntryCount int64 `json:"entry_count"`

	// VerifiedAt is the RFC3339 timestamp at which the verifier confirmed
	// the chain integrity before writing this checkpoint.
	VerifiedAt string `json:"verified_at"`

	// Verifier identifies the host and process that produced this checkpoint.
	Verifier string `json:"verifier"`

	// PrevCheckpointHash is the LastHash of the immediately preceding
	// checkpoint, forming a chain of checkpoints. An empty string indicates
	// this is the first checkpoint (genesis).
	PrevCheckpointHash string `json:"prev_checkpoint_hash,omitempty"`
}

// WORMSinkConfig holds the configuration for connecting to the MinIO WORM
// bucket. Values are sourced from environment variables by the API binary.
type WORMSinkConfig struct {
	// Endpoint is the MinIO server address, e.g. "minio:9000".
	Endpoint string

	// AccessKeyID is the access key for the audit-sink service account.
	// This account has PutObject + GetObject only; no DeleteObject.
	AccessKeyID string

	// SecretAccessKey is the secret key for the audit-sink service account.
	SecretAccessKey string

	// UseSSL controls whether the MinIO connection uses TLS. Production
	// deployments should set this to true.
	UseSSL bool
}

// WORMSink writes daily audit chain checkpoints to a MinIO Object Lock bucket.
// All objects are written with Compliance mode retention so they cannot be
// deleted or modified before the retention period expires.
type WORMSink struct {
	client *minio.Client
	logger *slog.Logger
}

// NewWORMSink creates a new WORMSink connected to the MinIO instance described
// by cfg. The caller must ensure the audit-worm bucket already exists and has
// Object Lock enabled (see infra/compose/minio/worm-bucket-init.sh).
func NewWORMSink(cfg WORMSinkConfig, logger *slog.Logger) (*WORMSink, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("audit/worm: endpoint is required")
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("audit/worm: access key credentials are required")
	}

	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("audit/worm: failed to create MinIO client: %w", err)
	}

	return &WORMSink{
		client: client,
		logger: logger,
	}, nil
}

// WriteCheckpoint serialises rec as JSON and writes it to the WORM bucket as a
// Compliance-mode locked object. The object key is:
//
//	checkpoints/{tenant_id}/{YYYY-MM-DD}.json
//
// This key scheme makes it easy to list checkpoints for a given tenant and to
// retrieve a specific day's checkpoint for forensic comparison.
//
// The object is written with a RetainUntilDate of today + wormRetentionYears.
// Once written, neither the audit-sink account nor the MinIO root account can
// delete or overwrite this object before that date.
//
// WriteCheckpoint is idempotent with respect to key naming: if a checkpoint
// for this tenant+day already exists (e.g. due to a retry), the function
// returns a WORMConflictError rather than silently overwriting. Callers should
// treat this as a non-fatal warning and log the conflict for manual review.
func (s *WORMSink) WriteCheckpoint(ctx context.Context, rec CheckpointRecord) error {
	payload, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("audit/worm: failed to marshal checkpoint: %w", err)
	}

	objectKey := fmt.Sprintf("checkpoints/%s/%s.json", rec.TenantID, rec.Day)
	retainUntil := time.Now().UTC().AddDate(wormRetentionYears, 0, 1) // +1 day buffer

	opts := minio.PutObjectOptions{
		ContentType: "application/json",
		RetainUntilDate: retainUntil,
		Mode: minio.Compliance,
	}

	_, err = s.client.PutObject(
		ctx,
		WORMBucket,
		objectKey,
		bytes.NewReader(payload),
		int64(len(payload)),
		opts,
	)
	if err != nil {
		// Check for "already exists" or "locked" errors from MinIO. MinIO returns
		// a 409 Conflict when an object with Compliance retention already exists
		// and the caller attempts to overwrite it.
		if minio.ToErrorResponse(err).Code == "ObjectLocked" ||
			minio.ToErrorResponse(err).StatusCode == 409 {
			s.logger.WarnContext(ctx, "audit/worm: checkpoint object already locked; skipping overwrite",
				slog.String("key", objectKey),
				slog.String("tenant_id", rec.TenantID),
				slog.String("day", rec.Day),
			)
			return &WORMConflictError{Key: objectKey}
		}
		return fmt.Errorf("audit/worm: PutObject failed for key %q: %w", objectKey, err)
	}

	s.logger.InfoContext(ctx, "audit/worm: checkpoint written",
		slog.String("key", objectKey),
		slog.String("tenant_id", rec.TenantID),
		slog.String("day", rec.Day),
		slog.Int64("last_id", rec.LastID),
		slog.Int64("entry_count", rec.EntryCount),
		slog.Time("retain_until", retainUntil),
	)

	return nil
}

// ReadCheckpoint retrieves a previously written checkpoint for the given
// tenant and day. Returns ErrCheckpointNotFound if no checkpoint exists.
//
// This is used by the verifier to compare the WORM-stored checkpoint against
// the current Postgres chain state. A missing WORM checkpoint does not
// automatically indicate tampering — it may mean the checkpoint has not been
// written yet (e.g. today's checkpoint before 03:30 local time). The caller
// must apply context when interpreting a not-found result.
func (s *WORMSink) ReadCheckpoint(ctx context.Context, tenantID, day string) (*CheckpointRecord, error) {
	objectKey := fmt.Sprintf("checkpoints/%s/%s.json", tenantID, day)

	obj, err := s.client.GetObject(ctx, WORMBucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, &ErrCheckpointNotFound{TenantID: tenantID, Day: day}
		}
		return nil, fmt.Errorf("audit/worm: GetObject failed for key %q: %w", objectKey, err)
	}
	defer obj.Close()

	var rec CheckpointRecord
	if err := json.NewDecoder(obj).Decode(&rec); err != nil {
		return nil, fmt.Errorf("audit/worm: failed to decode checkpoint at key %q: %w", objectKey, err)
	}

	return &rec, nil
}

// ListCheckpoints returns all checkpoint records for the given tenant, in
// chronological order. Used by forensic tooling and the compliance-auditor
// evidence pack generator.
func (s *WORMSink) ListCheckpoints(ctx context.Context, tenantID string) ([]CheckpointRecord, error) {
	prefix := fmt.Sprintf("checkpoints/%s/", tenantID)
	var records []CheckpointRecord

	for obj := range s.client.ListObjects(ctx, WORMBucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: false,
	}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("audit/worm: ListObjects error: %w", obj.Err)
		}

		rec, err := s.ReadCheckpoint(ctx, tenantID, checkpointDayFromKey(obj.Key))
		if err != nil {
			s.logger.WarnContext(ctx, "audit/worm: failed to read checkpoint during list; skipping",
				slog.String("key", obj.Key),
				slog.String("error", err.Error()),
			)
			continue
		}
		records = append(records, *rec)
	}

	return records, nil
}

// checkpointDayFromKey extracts the YYYY-MM-DD date from a key of the form
// "checkpoints/{tenant_id}/{YYYY-MM-DD}.json".
func checkpointDayFromKey(key string) string {
	// Find the last "/" and strip ".json"
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '/' {
			name := key[i+1:]
			if len(name) > 5 && name[len(name)-5:] == ".json" {
				return name[:len(name)-5]
			}
			return name
		}
	}
	return key
}

// WORMConflictError is returned when WriteCheckpoint encounters an object that
// is already Compliance-locked and cannot be overwritten.
type WORMConflictError struct {
	Key string
}

func (e *WORMConflictError) Error() string {
	return fmt.Sprintf("audit/worm: object %q is already compliance-locked", e.Key)
}

// ErrCheckpointNotFound is returned by ReadCheckpoint when no checkpoint
// object exists for the given tenant+day combination.
type ErrCheckpointNotFound struct {
	TenantID string
	Day      string
}

func (e *ErrCheckpointNotFound) Error() string {
	return fmt.Sprintf("audit/worm: checkpoint not found for tenant %q day %q", e.TenantID, e.Day)
}

// -----------------------------------------------------------------------------
// Evidence blob storage (Phase 3.0 — SOC 2 Type II evidence locker)
//
// The same WORM bucket and Compliance mode retention is reused as the
// tamper-evident substrate for SOC 2 evidence items. See ADR 0023 for
// rationale: evidence and audit checkpoints share one integrity boundary
// rather than operating separate buckets. Key scheme keeps them disjoint:
//
//	checkpoints/{tenant}/{YYYY-MM-DD}.json   -- daily audit chain checkpoints
//	evidence/{tenant}/{YYYY-MM}/{id}.bin     -- canonical signed evidence items
//
// The evidence writer (evidence.Store) depends on audit.WORMSink through the
// EvidenceWORM interface defined in the evidence package so the two packages
// stay loosely coupled.
// -----------------------------------------------------------------------------

// EvidenceObjectKey returns the deterministic WORM bucket key for a given
// evidence item. Exported so the evidence package can reference it without
// duplicating the key scheme — the scheme is the integrity contract.
func EvidenceObjectKey(tenantID, collectionPeriod, id string) string {
	return fmt.Sprintf("evidence/%s/%s/%s.bin", tenantID, collectionPeriod, id)
}

// PutEvidence writes signed canonical evidence bytes to the WORM bucket with
// Compliance mode retention. Returns the object key on success.
//
// The retention period is wormRetentionYears (5 years) to match KVKK Article 7
// and SOC 2 Type II observation window requirements. Once written, not even
// the MinIO root account can delete the object before the retention expires.
//
// If an object with the same key already exists and is Compliance-locked,
// returns a WORMConflictError. The caller should treat this as an idempotent
// retry signal — the original canonical bytes were already persisted, so
// skipping the second write is safe as long as the caller verifies the
// existing object matches.
func (s *WORMSink) PutEvidence(ctx context.Context, tenantID, collectionPeriod, id string, canonical []byte) (string, error) {
	if tenantID == "" || collectionPeriod == "" || id == "" {
		return "", fmt.Errorf("audit/worm: PutEvidence requires tenant_id, collection_period, id")
	}
	if len(canonical) == 0 {
		return "", fmt.Errorf("audit/worm: PutEvidence refuses empty canonical payload")
	}

	key := EvidenceObjectKey(tenantID, collectionPeriod, id)
	retainUntil := time.Now().UTC().AddDate(wormRetentionYears, 0, 1)

	opts := minio.PutObjectOptions{
		ContentType:     "application/octet-stream",
		RetainUntilDate: retainUntil,
		Mode:            minio.Compliance,
	}

	_, err := s.client.PutObject(
		ctx,
		WORMBucket,
		key,
		bytes.NewReader(canonical),
		int64(len(canonical)),
		opts,
	)
	if err != nil {
		if minio.ToErrorResponse(err).Code == "ObjectLocked" ||
			minio.ToErrorResponse(err).StatusCode == 409 {
			s.logger.WarnContext(ctx, "audit/worm: evidence object already locked; skipping",
				slog.String("key", key),
				slog.String("tenant_id", tenantID),
				slog.String("id", id),
			)
			return key, &WORMConflictError{Key: key}
		}
		return "", fmt.Errorf("audit/worm: PutEvidence %q: %w", key, err)
	}

	s.logger.InfoContext(ctx, "audit/worm: evidence written",
		slog.String("key", key),
		slog.String("tenant_id", tenantID),
		slog.String("collection_period", collectionPeriod),
		slog.String("id", id),
		slog.Int("bytes", len(canonical)),
		slog.Time("retain_until", retainUntil),
	)

	return key, nil
}

// GetEvidence retrieves the canonical signed bytes of a previously written
// evidence item. Used by the DPO evidence pack builder and the verification
// path that cross-checks Postgres metadata against WORM.
//
// Returns an ErrEvidenceNotFound if no object exists at the computed key.
func (s *WORMSink) GetEvidence(ctx context.Context, tenantID, collectionPeriod, id string) ([]byte, error) {
	key := EvidenceObjectKey(tenantID, collectionPeriod, id)

	obj, err := s.client.GetObject(ctx, WORMBucket, key, minio.GetObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, &ErrEvidenceNotFound{Key: key}
		}
		return nil, fmt.Errorf("audit/worm: GetEvidence %q: %w", key, err)
	}
	defer obj.Close()

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(obj); err != nil {
		return nil, fmt.Errorf("audit/worm: GetEvidence read %q: %w", key, err)
	}
	return buf.Bytes(), nil
}

// ErrEvidenceNotFound is returned by GetEvidence when no object exists at
// the computed WORM key. A missing evidence object after a successful
// Postgres INSERT indicates tampering and must be treated as a SOC 2 CC7.1
// incident.
type ErrEvidenceNotFound struct {
	Key string
}

func (e *ErrEvidenceNotFound) Error() string {
	return fmt.Sprintf("audit/worm: evidence object not found at key %q", e.Key)
}
