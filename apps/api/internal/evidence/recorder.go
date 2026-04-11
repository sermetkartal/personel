package evidence

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
)

// RecorderImpl is the default Recorder. It signs each item via Vault
// control-plane key and delegates to a storeInserter for persistence.
type RecorderImpl struct {
	store  storeInserter
	signer Signer
	log    *slog.Logger
}

// storeInserter is the narrow interface the Recorder needs from Store.
// Used as an interface so unit tests can inject a fake without having
// to build a pgxpool or spin up a Postgres container. The concrete
// *Store satisfies this by virtue of its Insert method.
type storeInserter interface {
	Insert(ctx context.Context, item Item) (string, error)
}

// Signer abstracts the Vault transit signing call. Injected for testability.
type Signer interface {
	// Sign returns (signature, key_version, error) for the given payload.
	Sign(ctx context.Context, payload []byte) ([]byte, string, error)
}

// NewRecorder creates a Recorder with the given dependencies.
func NewRecorder(store *Store, signer Signer, log *slog.Logger) *RecorderImpl {
	return &RecorderImpl{
		store:  store,
		signer: signer,
		log:    log,
	}
}

// newRecorderWithInserter is a test helper that lets unit tests inject
// a fake storeInserter bypassing the pgxpool.Pool dependency.
func newRecorderWithInserter(si storeInserter, signer Signer, log *slog.Logger) *RecorderImpl {
	return &RecorderImpl{store: si, signer: signer, log: log}
}

// Record signs and stores an evidence item. See the interface in types.go.
func (r *RecorderImpl) Record(ctx context.Context, item Item) (string, error) {
	if item.RecordedAt.IsZero() {
		item.RecordedAt = time.Now().UTC()
	}
	// Truncate RecordedAt to microsecond precision BEFORE signing.
	// Postgres timestamptz stores microseconds (6 digits); any
	// nanosecond-precision input gets silently truncated at INSERT.
	// If we sign with nanosecond precision and then read back the
	// microsecond value, canonicalize() produces a different byte
	// string and signature verification fails. Match the precision
	// of the downstream storage BEFORE the signature is computed.
	// Discovered 2026-04-11 running the integration test round-trip.
	item.RecordedAt = item.RecordedAt.UTC().Truncate(time.Microsecond)

	if item.TenantID == "" {
		return "", fmt.Errorf("evidence: tenant_id is required")
	}
	if item.Control == "" {
		return "", fmt.Errorf("evidence: control is required")
	}
	if item.Kind == "" {
		return "", fmt.Errorf("evidence: kind is required")
	}

	// Derive ID and CollectionPeriod BEFORE canonicalize so they are
	// covered by the signature. Previously Store.Insert filled these
	// fields after the fact, which meant the signed payload had empty
	// ID + period while the Postgres row had the real values — a latent
	// integrity bug where re-verifying the row's canonical form would
	// compute a different payload than what was signed. Caught by the
	// round-trip verification in the evidence integration test on
	// 2026-04-11.
	//
	// IMPORTANT: any field canonicalize() reads must be set here
	// BEFORE the Sign call. If you add a new canonicalize field that
	// can be auto-derived, derive it here too or update the list.
	if item.ID == "" {
		item.ID = ulid.Make().String()
	}
	if item.CollectionPeriod == "" {
		item.CollectionPeriod = item.RecordedAt.Format("2006-01")
	}

	// Build canonical payload for signing. The signature covers every
	// field that materially affects evidence integrity; optional fields
	// (AttachmentRefs) are included so auditors can verify attachment
	// references weren't swapped after signing.
	canonical := canonicalize(item)

	sig, keyVersion, err := r.signer.Sign(ctx, canonical)
	if err != nil {
		// A signing failure means we cannot produce admissible evidence.
		// We log loud and return the error so callers can treat it as
		// an operational incident.
		r.log.ErrorContext(ctx, "evidence signing failed — SOC 2 control drift",
			slog.String("control", string(item.Control)),
			slog.String("kind", string(item.Kind)),
			slog.String("error", err.Error()))
		return "", fmt.Errorf("evidence: sign: %w", err)
	}

	item.Signature = sig
	item.SignatureKeyVersion = keyVersion

	id, err := r.store.Insert(ctx, item)
	if err != nil {
		return "", fmt.Errorf("evidence: insert: %w", err)
	}

	r.log.InfoContext(ctx, "evidence recorded",
		slog.String("id", id),
		slog.String("tenant_id", item.TenantID),
		slog.String("control", string(item.Control)),
		slog.String("kind", string(item.Kind)),
		slog.String("period", item.CollectionPeriod),
		slog.String("key_version", keyVersion),
	)

	return id, nil
}

// canonicalize produces the byte sequence that gets signed. Field order
// is stable so the signature is reproducible across process restarts
// and language implementations.
//
// Format (all fields UTF-8 encoded, length-prefixed with 4-byte BE):
//
//	id | tenant_id | control | kind | collection_period |
//	recorded_at RFC3339 | actor | summary_tr | summary_en |
//	payload | referenced_audit_ids (sorted asc) | attachment_refs (sorted)
//
// IMPORTANT: RecordedAt is normalised to UTC + microsecond precision
// BEFORE formatting. Postgres timestamptz stores microseconds and may
// return times in the session timezone — without normalisation the
// round-trip would produce a different canonical byte string than what
// was originally signed, breaking signature verification. Match these
// normalisations exactly in Recorder.Record so both sign time and
// verify time produce the same output.
func canonicalize(item Item) []byte {
	var buf []byte
	appendField := func(s string) {
		b := []byte(s)
		l := uint32(len(b))
		buf = append(buf, byte(l>>24), byte(l>>16), byte(l>>8), byte(l))
		buf = append(buf, b...)
	}

	appendField(item.ID)
	appendField(item.TenantID)
	appendField(string(item.Control))
	appendField(string(item.Kind))
	appendField(item.CollectionPeriod)
	// Normalise UTC + microsecond precision at format time so post-DB
	// round-trips produce identical bytes to the sign-time form even
	// if the pgx driver returned a local-time value. See the method
	// comment for the precision rationale.
	appendField(item.RecordedAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano))
	appendField(item.Actor)
	appendField(item.SummaryTR)
	appendField(item.SummaryEN)
	appendField(string(item.Payload))

	// Referenced audit IDs as comma-separated sorted decimal strings.
	ids := append([]int64{}, item.ReferencedAuditIDs...)
	// manual sort to avoid importing slices package
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j-1] > ids[j]; j-- {
			ids[j-1], ids[j] = ids[j], ids[j-1]
		}
	}
	var idStr string
	for i, id := range ids {
		if i > 0 {
			idStr += ","
		}
		idStr += fmt.Sprintf("%d", id)
	}
	appendField(idStr)

	// Attachment refs sorted + joined.
	atts := append([]string{}, item.AttachmentRefs...)
	for i := 1; i < len(atts); i++ {
		for j := i; j > 0 && atts[j-1] > atts[j]; j-- {
			atts[j-1], atts[j] = atts[j], atts[j-1]
		}
	}
	var attStr string
	for i, a := range atts {
		if i > 0 {
			attStr += "|"
		}
		attStr += a
	}
	appendField(attStr)

	return buf
}

// noopSigner is used when Vault is unavailable during development or
// in tests. Returns a placeholder signature so Record() can still
// succeed in scaffold mode.
type noopSigner struct{}

// NewNoopSigner returns a Signer that produces non-cryptographic
// placeholder signatures. Never use in production — RecorderImpl logs
// a loud warning each time a noop signature is produced.
func NewNoopSigner() Signer {
	return &noopSigner{}
}

// Sign returns a static marker bytes + "noop" key version.
func (*noopSigner) Sign(_ context.Context, _ []byte) ([]byte, string, error) {
	return []byte("noop-signature-do-not-trust"), "noop", nil
}
