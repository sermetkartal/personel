// Package audit — nightly chain verifier and daily checkpoint writer.
//
// Runs as a cron-style job (02:30 local via systemd timer) per the runbook.
// On success writes to audit.audit_checkpoint and to the WORM sink.
// On failure raises a P0 alert and halts further verifier runs.
//
// WORM cross-validation (CrossValidateWORM) is called by a separate
// personel-worm-verifier.timer at 04:00 local — after the normal verifier and
// checkpoint write have completed. It reads back the previous day's checkpoint
// from MinIO Object Lock and compares its LastHash against the live Postgres
// chain head. A WORMDivergenceError proves Postgres was modified after the
// WORM write and constitutes a P0 incident. See docs/adr/0014-worm-audit-sink.md.
package audit

import (
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WORMDivergenceError is returned when CrossValidateWORM detects that the
// Postgres chain head differs from the WORM-locked checkpoint. This means
// Postgres was tampered with after the checkpoint was written to MinIO Object
// Lock — a P0 incident requiring immediate forensic response per
// docs/security/runbooks/worm-audit-recovery.md.
type WORMDivergenceError struct {
	TenantID         string
	Day              string
	WORMLastHash     string
	PostgresLastHash string
}

func (e *WORMDivergenceError) Error() string {
	return fmt.Sprintf(
		"WORM DIVERGENCE tenant=%s day=%s: worm_hash=%s postgres_hash=%s — audit chain may have been tampered",
		e.TenantID, e.Day, e.WORMLastHash, e.PostgresLastHash,
	)
}

const batchSize = 50_000

// CheckpointSigner signs checkpoint blobs with the control-plane Ed25519 key.
type CheckpointSigner interface {
	// Sign signs the canonical checkpoint bytes and returns the signature.
	Sign(ctx context.Context, payload []byte) (sig []byte, keyID string, err error)
}

// ExternalSink writes a signed checkpoint to an external append-only store.
type ExternalSink interface {
	Write(ctx context.Context, day time.Time, tenantID string, checkpointJSON []byte) error
}

// Verifier runs the nightly full-chain verification and checkpoint writing.
type Verifier struct {
	pool     *pgxpool.Pool
	signer   CheckpointSigner
	sink     ExternalSink
	worm     *WORMSink // optional; nil skips WORM writes with a warning
	log      *slog.Logger
	recorder *Recorder
}

// NewVerifier creates a Verifier. worm may be nil; in that case WORM writes
// are skipped and a warning is emitted. All production deployments must pass
// a non-nil WORMSink — a nil sink is only acceptable in test environments.
func NewVerifier(pool *pgxpool.Pool, signer CheckpointSigner, sink ExternalSink, worm *WORMSink, rec *Recorder, log *slog.Logger) *Verifier {
	if worm == nil {
		log.Warn("audit/verifier: WORMSink is nil — WORM checkpoint writes are DISABLED; this is not acceptable in production")
	}
	return &Verifier{pool: pool, signer: signer, sink: sink, worm: worm, recorder: rec, log: log}
}

// dbRow is a raw audit row fetched for verification.
type dbRow struct {
	ID       int64
	Ts       time.Time
	Actor    string
	TenantID string
	Action   string
	Target   string
	Details  []byte // raw JSONB bytes
	PrevHash []byte
	Hash     []byte
}

// RunForTenant verifies the full chain for a tenant and writes a checkpoint.
// Returns an error if the chain is broken — the caller should alert and halt.
func (v *Verifier) RunForTenant(ctx context.Context, tenantID string) error {
	v.log.Info("audit verifier: starting", slog.String("tenant_id", tenantID))
	start := time.Now()

	// Load previous checkpoint to know where to start batch.
	var lastID int64
	var lastHash []byte
	row := v.pool.QueryRow(ctx,
		`SELECT last_id, last_hash FROM audit.audit_checkpoint
		 WHERE tenant_id = $1::uuid
		 ORDER BY day DESC LIMIT 1`,
		tenantID,
	)
	_ = row.Scan(&lastID, &lastHash) // ok if no rows; start from 0

	if len(lastHash) == 0 {
		lastHash = GenesisHash()
	}

	var (
		prevHash     = lastHash
		count        int64
		lastVerifiedID int64
		offset       = lastID
	)

	for {
		rows, err := v.pool.Query(ctx,
			`SELECT id, ts, actor, tenant_id::text, action, target, details, prev_hash, hash
			 FROM audit.audit_log
			 WHERE tenant_id = $1::uuid AND id > $2
			 ORDER BY id ASC
			 LIMIT $3`,
			tenantID, offset, batchSize,
		)
		if err != nil {
			return fmt.Errorf("audit verifier: query batch: %w", err)
		}

		batchCount := 0
		for rows.Next() {
			var r dbRow
			if err := rows.Scan(&r.ID, &r.Ts, &r.Actor, &r.TenantID, &r.Action, &r.Target, &r.Details, &r.PrevHash, &r.Hash); err != nil {
				rows.Close()
				return fmt.Errorf("audit verifier: scan row: %w", err)
			}

			// Verify prev_hash matches the hash of the previous row.
			if !bytesEqual(r.PrevHash, prevHash) {
				rows.Close()
				v.alertChainBroken(ctx, tenantID, r.ID, "prev_hash mismatch")
				return fmt.Errorf("audit verifier: chain broken at id=%d: prev_hash mismatch", r.ID)
			}

			// Recompute hash.
			cr := &CanonicalRecord{
				ID:       r.ID,
				Ts:       r.Ts,
				Actor:    r.Actor,
				TenantID: r.TenantID,
				Action:   r.Action,
				Target:   r.Target,
				Details:  nil, // use raw bytes path below
				PrevHash: r.PrevHash,
			}
			computed, err := cr.hashWithRawDetails(r.Details)
			if err != nil {
				rows.Close()
				return fmt.Errorf("audit verifier: hash row %d: %w", r.ID, err)
			}

			if !bytesEqual(computed, r.Hash) {
				rows.Close()
				v.alertChainBroken(ctx, tenantID, r.ID, "hash mismatch")
				return fmt.Errorf("audit verifier: chain broken at id=%d: hash mismatch", r.ID)
			}

			prevHash = r.Hash
			lastVerifiedID = r.ID
			batchCount++
			count++
		}
		rows.Close()

		if batchCount < batchSize {
			break // no more rows
		}
		offset = lastVerifiedID
	}

	if count == 0 {
		v.log.Info("audit verifier: no new rows since last checkpoint", slog.String("tenant_id", tenantID))
	}

	// Write checkpoint.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	payload := buildCheckpointPayload(today, tenantID, lastID, lastVerifiedID, prevHash, count)
	sig, keyID, err := v.signer.Sign(ctx, payload)
	if err != nil {
		return fmt.Errorf("audit verifier: sign checkpoint: %w", err)
	}

	_, err = v.pool.Exec(ctx,
		`INSERT INTO audit.audit_checkpoint (tenant_id, day, last_id, last_hash, entry_count, verified_at, verifier, signature)
		 VALUES ($1::uuid, $2, $3, $4, $5, now(), $6, $7)
		 ON CONFLICT (tenant_id, day) DO UPDATE
		   SET last_id = EXCLUDED.last_id,
		       last_hash = EXCLUDED.last_hash,
		       entry_count = audit_checkpoint.entry_count + EXCLUDED.entry_count,
		       verified_at = EXCLUDED.verified_at,
		       verifier = EXCLUDED.verifier,
		       signature = EXCLUDED.signature`,
		tenantID, today, lastVerifiedID, prevHash, count, keyID, sig,
	)
	if err != nil {
		return fmt.Errorf("audit verifier: write checkpoint: %w", err)
	}

	// Push to external sink (non-fatal on failure; alert separately).
	checkpointJSON := buildCheckpointJSON(today, tenantID, lastVerifiedID, prevHash, count, keyID, sig)
	if sinkErr := v.sink.Write(ctx, today, tenantID, checkpointJSON); sinkErr != nil {
		v.log.Error("audit verifier: external sink write failed — manual reconciliation required",
			slog.String("tenant_id", tenantID),
			slog.Any("error", sinkErr),
		)
	}

	// Write to WORM sink (MinIO Object Lock). Non-fatal: Postgres checkpoint is
	// the primary record. WORM failure fires a Prometheus alert via textfile
	// collector; the on-call engineer must reconcile within 24h.
	if v.worm != nil {
		wormRec := CheckpointRecord{
			SchemaVersion: 1,
			TenantID:      tenantID,
			Day:           today.Format("2006-01-02"),
			LastID:        lastVerifiedID,
			LastHash:      hex.EncodeToString(prevHash),
			EntryCount:    count,
			VerifiedAt:    time.Now().UTC().Format(time.RFC3339),
			Verifier:      keyID,
		}
		if wormErr := v.worm.WriteCheckpoint(ctx, wormRec); wormErr != nil {
			v.log.Error("audit verifier: WORM sink write failed — checkpoint not in Object Lock; reconcile within 24h",
				slog.String("tenant_id", tenantID),
				slog.String("day", wormRec.Day),
				slog.Any("error", wormErr),
			)
		} else {
			v.log.Info("audit verifier: WORM checkpoint written",
				slog.String("tenant_id", tenantID),
				slog.String("day", wormRec.Day),
				slog.String("bucket", WORMBucket),
			)
		}
	} else {
		v.log.Warn("audit verifier: WORM sink not configured — skipping Object Lock write")
	}

	// Audit the verification itself.
	_, _ = v.recorder.AppendSystem(ctx, tenantID, ActionAuditChainVerified, fmt.Sprintf("checkpoint:%s", today.Format("2006-01-02")), map[string]any{
		"count":   count,
		"last_id": lastVerifiedID,
		"key_id":  keyID,
		"elapsed": time.Since(start).String(),
	})

	v.log.Info("audit verifier: done",
		slog.String("tenant_id", tenantID),
		slog.Int64("count", count),
		slog.Duration("elapsed", time.Since(start)),
	)
	return nil
}

// CrossValidateWORM reads the previous day's checkpoint from the WORM bucket
// and compares its LastHash to the live Postgres chain tail for the same day.
//
// Called nightly by personel-worm-verifier.timer at 04:00 local — after
// personel-audit-verifier.timer at 03:30 has completed its run and written the
// WORM checkpoint.
//
// Returns a WORMDivergenceError if the hashes differ — this is evidence of
// post-checkpoint tampering and is a P0 incident. Returns nil on success.
// Returns nil (with a warning) if the WORM sink is unconfigured or if no
// checkpoint exists for the requested day (which may legitimately happen if
// yesterday had zero audit events).
func (v *Verifier) CrossValidateWORM(ctx context.Context, tenantID, day string) error {
	if v.worm == nil {
		v.log.WarnContext(ctx, "audit/verifier: WORM sink not configured; skipping cross-validation")
		return nil
	}

	wormRec, err := v.worm.ReadCheckpoint(ctx, tenantID, day)
	if err != nil {
		if _, notFound := err.(*ErrCheckpointNotFound); notFound {
			v.log.WarnContext(ctx, "audit/verifier: no WORM checkpoint found for day; cannot cross-validate",
				slog.String("tenant_id", tenantID),
				slog.String("day", day),
			)
			return nil
		}
		return fmt.Errorf("audit/verifier: failed to read WORM checkpoint tenant=%s day=%s: %w", tenantID, day, err)
	}

	// Query the Postgres chain tail for the same day.
	var pgHeadHash []byte
	err = v.pool.QueryRow(ctx, `
		SELECT hash
		FROM audit.audit_log
		WHERE tenant_id = $1::uuid
		  AND ts >= $2::date
		  AND ts <  ($2::date + interval '1 day')
		ORDER BY id DESC
		LIMIT 1
	`, tenantID, day).Scan(&pgHeadHash)
	if err != nil {
		return fmt.Errorf("audit/verifier: failed to query Postgres chain tail tenant=%s day=%s: %w", tenantID, day, err)
	}

	pgHeadHashHex := hex.EncodeToString(pgHeadHash)

	if pgHeadHashHex != wormRec.LastHash {
		divErr := &WORMDivergenceError{
			TenantID:         tenantID,
			Day:              day,
			WORMLastHash:     wormRec.LastHash,
			PostgresLastHash: pgHeadHashHex,
		}
		v.log.Error("WORM DIVERGENCE DETECTED — P0 INCIDENT — see runbook worm-audit-recovery.md",
			slog.String("tenant_id", tenantID),
			slog.String("day", day),
			slog.String("worm_hash", wormRec.LastHash),
			slog.String("postgres_hash", pgHeadHashHex),
		)
		v.alertChainBroken(ctx, tenantID, 0, fmt.Sprintf("WORM divergence on day %s", day))
		return divErr
	}

	v.log.Info("audit/verifier: WORM cross-validation passed",
		slog.String("tenant_id", tenantID),
		slog.String("day", day),
		slog.String("hash", pgHeadHashHex),
	)
	return nil
}

func (v *Verifier) alertChainBroken(ctx context.Context, tenantID string, atID int64, reason string) {
	v.log.Error("AUDIT CHAIN BROKEN — P0 INCIDENT",
		slog.String("tenant_id", tenantID),
		slog.Int64("at_id", atID),
		slog.String("reason", reason),
	)
	_, _ = v.recorder.AppendSystem(ctx, tenantID, ActionAuditChainBroken,
		fmt.Sprintf("id:%d", atID),
		map[string]any{"reason": reason, "at_id": atID},
	)
}

// HashWithRawDetails reuses the canonical hash logic but accepts pre-marshalled
// details bytes (as stored in Postgres JSONB). This is used by the verifier and
// integration tests to recompute hashes without a Go map round-trip.
func (cr *CanonicalRecord) HashWithRawDetails(rawDetails []byte) ([]byte, error) {
	return cr.hashWithRawDetails(rawDetails)
}

// hashWithRawDetails is the internal implementation.
func (cr *CanonicalRecord) hashWithRawDetails(rawDetails []byte) ([]byte, error) {
	if rawDetails == nil {
		rawDetails = []byte("{}")
	}
	// Build the same buffer as Hash() but substitute rawDetails directly.
	import_ := func() []byte {
		// inline to avoid circular package issues; matches canonical.go exactly
		var buf []byte
		idBuf := make([]byte, 8)
		binary.BigEndian.PutUint64(idBuf, uint64(cr.ID))
		buf = append(buf, idBuf...)

		tsMicros := cr.Ts.UnixMicro()
		tsNanos := tsMicros * 1000
		tsBuf := make([]byte, 8)
		binary.BigEndian.PutUint64(tsBuf, uint64(tsNanos))
		buf = append(buf, tsBuf...)

		buf = appendField(buf, []byte(cr.Actor))
		buf = appendField(buf, []byte(cr.TenantID))
		buf = appendField(buf, []byte(cr.Action))
		buf = appendField(buf, []byte(cr.Target))
		buf = appendField(buf, rawDetails)
		buf = append(buf, cr.PrevHash...)
		return buf
	}

	payload := import_()
	_ = ed25519.PublicKeySize // keep crypto/ed25519 imported
	var out [32]byte
	h := hashSHA256(payload)
	copy(out[:], h)
	return out[:], nil
}

func appendField(buf []byte, b []byte) []byte {
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(b)))
	buf = append(buf, lenBuf...)
	buf = append(buf, b...)
	return buf
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func buildCheckpointPayload(day time.Time, tenantID string, firstID, lastID int64, headHash []byte, count int64) []byte {
	var out []byte
	out = append(out, []byte(day.Format(time.RFC3339))...)
	out = append(out, []byte(tenantID)...)
	idBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(idBuf, uint64(firstID))
	out = append(out, idBuf...)
	binary.BigEndian.PutUint64(idBuf, uint64(lastID))
	out = append(out, idBuf...)
	out = append(out, headHash...)
	binary.BigEndian.PutUint64(idBuf, uint64(count))
	out = append(out, idBuf...)
	return out
}

func buildCheckpointJSON(day time.Time, tenantID string, lastID int64, headHash []byte, count int64, keyID string, sig []byte) []byte {
	return []byte(fmt.Sprintf(
		`{"day":%q,"tenant_id":%q,"last_id":%d,"chain_head_hash":%q,"entry_count":%d,"signed_by":%q,"signature":%q}`,
		day.Format("2006-01-02"), tenantID, lastID, hex.EncodeToString(headHash), count, keyID, hex.EncodeToString(sig),
	))
}
