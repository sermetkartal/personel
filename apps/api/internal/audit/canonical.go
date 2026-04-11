// Package audit — canonical encoder for hash input.
// The encoding defined here MUST match the audit.compute_hash Postgres function
// byte-for-byte. Any change here requires a simultaneous schema migration and
// must be approved by security-engineer AND compliance-auditor.
package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"sort"
	"time"
)

// CanonicalRecord contains the fields that are hashed for a single audit row.
type CanonicalRecord struct {
	ID       int64
	Ts       time.Time
	Actor    string
	TenantID string // UUID string
	Action   string
	Target   string
	Details  map[string]any
	PrevHash []byte // 32 bytes; genesis row uses 32×0x00
}

// Hash computes:
//
//	SHA-256(
//	    id_be64 ||
//	    ts_unix_nanos_be64 ||
//	    len(actor)_be32 || actor_bytes ||
//	    len(tenant_id)_be32 || tenant_id_bytes ||
//	    len(action)_be32 || action_bytes ||
//	    len(target)_be32 || target_bytes ||
//	    len(details_canon)_be32 || details_canon_bytes ||
//	    prev_hash_32
//	)
//
// ts is converted to Unix nanoseconds with microsecond precision (multiply
// Unix microseconds by 1000) to match the Postgres extraction approach.
func (r *CanonicalRecord) Hash() ([]byte, error) {
	detailsBytes, err := canonDetails(r.Details)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer

	// id_be64
	idBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(idBuf, uint64(r.ID))
	buf.Write(idBuf)

	// ts_unix_nanos_be64 — microsecond precision × 1000
	tsMicros := r.Ts.UnixMicro()
	tsNanos := tsMicros * 1000
	tsBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(tsBuf, uint64(tsNanos))
	buf.Write(tsBuf)

	// len(actor)_be32 || actor
	writeField(&buf, []byte(r.Actor))

	// len(tenant_id)_be32 || tenant_id
	writeField(&buf, []byte(r.TenantID))

	// len(action)_be32 || action
	writeField(&buf, []byte(r.Action))

	// len(target)_be32 || target
	writeField(&buf, []byte(r.Target))

	// len(details_canon)_be32 || details_canon
	writeField(&buf, detailsBytes)

	// prev_hash_32
	if len(r.PrevHash) != 32 {
		return nil, ErrInvalidPrevHashLen
	}
	buf.Write(r.PrevHash)

	sum := sha256.Sum256(buf.Bytes())
	return sum[:], nil
}

// ErrInvalidPrevHashLen is returned when PrevHash is not 32 bytes.
var ErrInvalidPrevHashLen = &auditError{"prev_hash must be exactly 32 bytes"}

type auditError struct{ msg string }

func (e *auditError) Error() string { return e.msg }

// canonDetails returns the canonical JSON encoding: keys sorted
// lexicographically, no whitespace, UTF-8. Matching the Postgres
// audit.canon_details(jsonb) function.
func canonDetails(d map[string]any) ([]byte, error) {
	if d == nil {
		return []byte("{}"), nil
	}
	// Sort keys
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	ordered := make(map[string]any, len(d))
	for _, k := range keys {
		ordered[k] = d[k]
	}
	// json.Marshal on a map does NOT guarantee key order in older Go; we
	// use the sorted approach of building an ordered slice of k/v pairs.
	// For nested maps the sort is not recursive; details should be flat.
	return json.Marshal(ordered)
}

func writeField(buf *bytes.Buffer, b []byte) {
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(b)))
	buf.Write(lenBuf)
	buf.Write(b)
}

// GenesisHash returns the 32-byte genesis (all-zero) prev_hash.
func GenesisHash() []byte {
	return make([]byte, 32)
}
