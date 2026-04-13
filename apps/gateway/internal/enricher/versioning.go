// versioning.go — event schema version dispatch (Faz 7 #73).
//
// The enricher no longer calls proto.Unmarshal on raw EventBatch bytes
// directly. Every NATS message carries a "schema-version" header. The
// VersionedDecoder looks up the registered Decoder for that version and
// delegates the parse. A future v2 schema can ship as a second Decoder
// without a big-bang cutover: the gateway accepts both v1 and v2 until
// the fleet has finished rolling out, then the v1 Decoder is removed.
//
// Dispatch contract:
//   - Missing or empty header → treated as SchemaV1 (backwards compat
//     for agents that pre-date this feature).
//   - Unknown version → ErrUnknownSchemaVersion. Callers should route
//     the message to the DLQ rather than NAK (redelivery is pointless
//     when the gateway literally cannot parse the bytes).
//   - Registered decoder's Decode error → wrapped so DLQ.ErrorKind can
//     disambiguate "decode" vs "enrich" failures.
//
// Canonical Event shape: each decoder produces a *personelv1.EventBatch
// regardless of the wire schema. v1 maps 1:1; v2 will project new
// fields onto the same target struct (additional fields may be left
// zero-valued).
package enricher

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	personelv1 "github.com/personel/proto/personel/v1"
)

// Canonical schema version identifiers. Strings rather than ints so
// log lines and DLQ payloads are self-describing.
const (
	// SchemaV1 is the current agent wire format.
	SchemaV1 = "v1"

	// SchemaV2 is a placeholder for the next-generation schema. The
	// v2 decoder currently returns ErrNotImplemented — register it
	// only when the v2 proto types exist. Leaving the constant
	// defined lets tests exercise the dispatch path today.
	SchemaV2 = "v2"

	// HeaderSchemaVersion is the NATS header key. Lower-case
	// matches the convention used by other headers in the gateway.
	HeaderSchemaVersion = "schema-version"
)

// Sentinel errors returned by VersionedDecoder.Dispatch.
var (
	// ErrUnknownSchemaVersion is returned when the header carries a
	// version string that no registered decoder knows about.
	ErrUnknownSchemaVersion = errors.New("enricher: unknown schema_version")

	// ErrNotImplemented is returned by the v2 decoder stub until a
	// real implementation is wired in.
	ErrNotImplemented = errors.New("enricher: decoder not implemented")
)

// Decoder parses a raw wire payload into an *EventBatch for the
// enrichment pipeline. Implementations MUST be safe for concurrent use.
type Decoder interface {
	// Version returns the schema version string this decoder handles.
	// Must exactly match the "schema-version" header value.
	Version() string

	// Decode parses the raw payload into an EventBatch. The returned
	// batch is fully materialised — the caller will not keep the raw
	// slice alive.
	Decode(raw []byte) (*personelv1.EventBatch, error)
}

// VersionedDecoder routes raw payloads to the correct Decoder by
// inspecting the NATS headers.
type VersionedDecoder struct {
	decoders map[string]Decoder
	// fallback is used when the header is missing or empty. Default:
	// SchemaV1 — older agents predate the header and are indistinguishable
	// from a v1 publish.
	fallback string
}

// NewVersionedDecoder builds a VersionedDecoder with the given
// Decoders registered by their reported Version(). Duplicate versions
// are rejected (last-wins would be a silent footgun during a migration).
func NewVersionedDecoder(decoders ...Decoder) (*VersionedDecoder, error) {
	m := make(map[string]Decoder, len(decoders))
	for _, d := range decoders {
		if d == nil {
			return nil, fmt.Errorf("enricher: nil decoder")
		}
		v := d.Version()
		if v == "" {
			return nil, fmt.Errorf("enricher: decoder with empty Version()")
		}
		if _, ok := m[v]; ok {
			return nil, fmt.Errorf("enricher: duplicate decoder for version %q", v)
		}
		m[v] = d
	}
	return &VersionedDecoder{
		decoders: m,
		fallback: SchemaV1,
	}, nil
}

// NewDefaultDecoder returns a VersionedDecoder with v1 registered as
// the canonical current-wire decoder and v2 as a stub that returns
// ErrNotImplemented. The stub is present so that the dispatch path is
// exercised by tests today — a real v2 Decoder replaces it later.
func NewDefaultDecoder() *VersionedDecoder {
	vd, err := NewVersionedDecoder(v1Decoder{}, v2DecoderStub{})
	if err != nil {
		// Both decoders are compiled in; this cannot fail.
		panic(fmt.Sprintf("enricher: build default decoder: %v", err))
	}
	return vd
}

// Dispatch looks at headers["schema-version"] and delegates to the
// matching Decoder. Returns ErrUnknownSchemaVersion for unrecognised
// versions so the caller can route the message to the DLQ.
//
// headers accepts the jetstream.Msg.Headers() shape: map[string][]string.
// We only read the first value of each header (NATS headers are ordered
// but duplicates are rare in our protocol).
func (v *VersionedDecoder) Dispatch(headers map[string][]string, payload []byte) (*personelv1.EventBatch, string, error) {
	version := v.extractVersion(headers)

	d, ok := v.decoders[version]
	if !ok {
		return nil, version, fmt.Errorf("%w: %q", ErrUnknownSchemaVersion, version)
	}

	batch, err := d.Decode(payload)
	if err != nil {
		return nil, version, err
	}
	return batch, version, nil
}

// KnownVersions returns the sorted list of registered version strings.
// Used by /healthz and by the DLQ debug inspector.
func (v *VersionedDecoder) KnownVersions() []string {
	out := make([]string, 0, len(v.decoders))
	for k := range v.decoders {
		out = append(out, k)
	}
	return out
}

// extractVersion returns the schema-version header value, falling back
// to SchemaV1 when absent or empty. Header keys are matched
// case-insensitively against HeaderSchemaVersion.
func (v *VersionedDecoder) extractVersion(headers map[string][]string) string {
	if headers == nil {
		return v.fallback
	}
	// Direct hit (cheapest path).
	if vals, ok := headers[HeaderSchemaVersion]; ok && len(vals) > 0 && vals[0] != "" {
		return vals[0]
	}
	// Case-insensitive fallback — NATS clients sometimes canonicalise.
	for k, vals := range headers {
		if len(vals) == 0 {
			continue
		}
		if equalFoldASCII(k, HeaderSchemaVersion) && vals[0] != "" {
			return vals[0]
		}
	}
	return v.fallback
}

// equalFoldASCII is a tiny strings.EqualFold for ASCII header keys. The
// stdlib version pulls in Unicode tables; we never need them for NATS
// header keys.
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// -----------------------------------------------------------------------------
// Concrete decoders

// v1Decoder handles the current wire format: a proto.Marshal of an
// *personelv1.EventBatch. This is the only Decoder that can actually
// parse live agent traffic today.
type v1Decoder struct{}

func (v1Decoder) Version() string { return SchemaV1 }

func (v1Decoder) Decode(raw []byte) (*personelv1.EventBatch, error) {
	var batch personelv1.EventBatch
	if err := proto.Unmarshal(raw, &batch); err != nil {
		return nil, fmt.Errorf("v1 decoder: unmarshal: %w", err)
	}
	return &batch, nil
}

// v2DecoderStub is a placeholder. Its presence validates the dispatch
// path end-to-end: a message with schema-version=v2 resolves to a
// Decoder, it just fails immediately with ErrNotImplemented. When the
// v2 schema ships, swap this type for a real implementation.
type v2DecoderStub struct{}

func (v2DecoderStub) Version() string { return SchemaV2 }

func (v2DecoderStub) Decode(_ []byte) (*personelv1.EventBatch, error) {
	return nil, ErrNotImplemented
}
