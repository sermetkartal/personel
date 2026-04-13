// Command schema-check is the Faz 7 / roadmap item #79 compile-time
// guard for the Personel event schema registry.
//
// It parses proto/personel/v1/events.proto and common.proto with a
// minimal hand-rolled tokenizer (no new dependencies), extracts every
// `message X { ... field ... }` block, and compares the resulting
// message→field map against a baseline JSON file. If a message that
// exists in the baseline is missing required fields and the schema
// version has not been bumped, the command exits 1 and prints the
// offending messages.
//
// Usage:
//
//	go run ./apps/gateway/cmd/schema-check \
//	    -proto ./proto/personel/v1/events.proto,./proto/personel/v1/common.proto \
//	    -baseline ./proto/personel/v1/SCHEMA_BASELINE.json
//
// TODO Faz 16: wire to CI via .github/workflows/schema-check.yml
// (the workflow file is intentionally NOT in this change — Faz 16
// item #168 owns CI matrix work).
//
// Design notes:
//   - We do NOT use github.com/emicklei/proto — the project avoids adding
//     transitive deps for CI-only tools.
//   - The parser is deliberately loose: it only cares about message
//     declarations and their direct field names. Nested messages, enums,
//     oneofs, options, comments, and reserved ranges are tolerated but
//     unparsed.
//   - A field is "removed" if its name disappears from the message
//     block. Renames are detected as remove+add.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

type baselineMessage struct {
	RequiredFields []string `json:"required_fields"`
}

type baseline struct {
	SchemaVersion int                        `json:"schema_version"`
	GeneratedAt   string                     `json:"generated_at"`
	Note          string                     `json:"note"`
	Messages      map[string]baselineMessage `json:"messages"`
}

func main() {
	protoFlag := flag.String("proto", "proto/personel/v1/events.proto,proto/personel/v1/common.proto",
		"comma-separated list of .proto files to scan")
	baselineFlag := flag.String("baseline", "proto/personel/v1/SCHEMA_BASELINE.json",
		"path to the schema baseline JSON")
	verbose := flag.Bool("v", false, "verbose output")
	flag.Parse()

	// Load baseline.
	base, err := loadBaseline(*baselineFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "schema-check: load baseline: %v\n", err)
		os.Exit(2)
	}

	// Parse each proto file and accumulate messages.
	current := map[string]map[string]bool{}
	for _, path := range strings.Split(*protoFlag, ",") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "schema-check: read %s: %v\n", path, err)
			os.Exit(2)
		}
		parseProto(string(data), current)
	}

	if *verbose {
		for name, fields := range current {
			names := make([]string, 0, len(fields))
			for f := range fields {
				names = append(names, f)
			}
			sort.Strings(names)
			fmt.Fprintf(os.Stderr, "  parsed %s: %s\n", name, strings.Join(names, ", "))
		}
	}

	// Compare.
	var failures []string
	for msgName, bmsg := range base.Messages {
		fields, ok := current[msgName]
		if !ok {
			failures = append(failures,
				fmt.Sprintf("message %q present in baseline but missing from proto sources", msgName))
			continue
		}
		for _, required := range bmsg.RequiredFields {
			if !fields[required] {
				failures = append(failures,
					fmt.Sprintf("message %q: required field %q removed without schema_version bump",
						msgName, required))
			}
		}
	}

	if len(failures) > 0 {
		sort.Strings(failures)
		fmt.Fprintln(os.Stderr, "schema-check: FAIL")
		for _, f := range failures {
			fmt.Fprintln(os.Stderr, "  -", f)
		}
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "To fix: either restore the field or bump schema_version in")
		fmt.Fprintln(os.Stderr, "the baseline and document the breaking change in")
		fmt.Fprintln(os.Stderr, "docs/architecture/event-schema-registry.md §4.")
		os.Exit(1)
	}

	fmt.Printf("schema-check: OK (%d messages, baseline version %d)\n",
		len(base.Messages), base.SchemaVersion)
}

func loadBaseline(path string) (*baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &b, nil
}

// parseProto is a minimal proto3 parser. It finds top-level
// `message <Name> { ... }` blocks and extracts field names. It is
// intentionally simple and tolerant of malformed input.
//
// Recognised field form: `[repeated] <type> <name> = <tag>;`
// Ignored: `oneof`, `reserved`, `option`, `//` comments.
func parseProto(src string, out map[string]map[string]bool) {
	src = stripComments(src)
	n := len(src)
	i := 0
	for i < n {
		// Skip whitespace.
		for i < n && isSpace(src[i]) {
			i++
		}
		if i >= n {
			break
		}
		// Look for "message <Name> {".
		if !hasWord(src, i, "message") {
			// Advance one rune-ish and continue.
			i++
			continue
		}
		i += len("message")
		// Skip whitespace.
		for i < n && isSpace(src[i]) {
			i++
		}
		// Read name.
		nameStart := i
		for i < n && isIdent(src[i]) {
			i++
		}
		name := src[nameStart:i]
		if name == "" {
			continue
		}
		// Skip to '{'.
		for i < n && src[i] != '{' {
			i++
		}
		if i >= n {
			return
		}
		i++ // consume '{'
		// Read until matching '}'.
		body, consumed := readBracedBody(src, i)
		i += consumed
		fields := parseFields(body)
		if len(fields) == 0 {
			continue
		}
		if _, ok := out[name]; !ok {
			out[name] = map[string]bool{}
		}
		for _, f := range fields {
			out[name][f] = true
		}
	}
}

// readBracedBody reads from `src[start:]` until the matching closing
// brace is found, handling nested {}. Returns the body (exclusive of
// the closing brace) and the number of bytes consumed from start.
func readBracedBody(src string, start int) (string, int) {
	depth := 1
	i := start
	for i < len(src) && depth > 0 {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start:i], (i - start) + 1
			}
		}
		i++
	}
	return src[start:i], i - start
}

// parseFields extracts simple proto3 field declarations from a message
// body. It handles:
//
//	type name = tag;
//	repeated type name = tag;
//	oneof foo { type name = tag; ... }
//
// and intentionally ignores options, reserved ranges, nested messages,
// and enums.
func parseFields(body string) []string {
	var out []string
	body = stripComments(body)
	// Split on ';' — each statement ends with one.
	for _, stmt := range strings.Split(body, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		// Skip nested message/enum bodies and oneof braces — they
		// leave residue like "oneof payload { ... }" which we handle
		// by recursing for oneof blocks.
		if strings.HasPrefix(stmt, "message ") ||
			strings.HasPrefix(stmt, "enum ") ||
			strings.HasPrefix(stmt, "reserved") ||
			strings.HasPrefix(stmt, "option ") ||
			strings.HasPrefix(stmt, "extensions ") {
			continue
		}
		// oneof: `oneof name { type fld = N <closer consumed>`
		// Our ';' split breaks the oneof into multiple pseudo-stmts,
		// the first of which starts with "oneof name {". Strip that
		// prefix and re-process.
		if strings.HasPrefix(stmt, "oneof ") {
			if idx := strings.Index(stmt, "{"); idx != -1 {
				stmt = stmt[idx+1:]
			}
		}
		// Strip trailing '}' closers.
		stmt = strings.TrimRight(stmt, "}")
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		// Must contain '=' to be a field.
		eq := strings.Index(stmt, "=")
		if eq < 0 {
			continue
		}
		lhs := strings.TrimSpace(stmt[:eq])
		// Drop leading "repeated ", "optional ", "required " (proto2), "map<...>" handling.
		lhs = strings.TrimPrefix(lhs, "repeated ")
		lhs = strings.TrimPrefix(lhs, "optional ")
		lhs = strings.TrimPrefix(lhs, "required ")
		// For map<K, V> name, the type ends at '>'.
		if strings.HasPrefix(lhs, "map<") {
			gt := strings.Index(lhs, ">")
			if gt < 0 {
				continue
			}
			lhs = strings.TrimSpace(lhs[gt+1:])
		}
		// lhs should now be "<type> <name>", possibly with a package
		// prefix on the type ("google.protobuf.Timestamp name").
		// Take the last whitespace-separated token as the field name.
		fields := strings.Fields(lhs)
		if len(fields) < 2 {
			continue
		}
		name := fields[len(fields)-1]
		// Must be a valid identifier.
		if !isValidIdent(name) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// stripComments removes `// ...` line comments and `/* ... */` block comments.
func stripComments(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '/' {
			// line comment
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func hasWord(s string, i int, w string) bool {
	if i+len(w) > len(s) {
		return false
	}
	if s[i:i+len(w)] != w {
		return false
	}
	if i+len(w) < len(s) && isIdent(s[i+len(w)]) {
		return false
	}
	if i > 0 && isIdent(s[i-1]) {
		return false
	}
	return true
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
func isIdent(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func isValidIdent(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !isIdent(name[i]) {
			return false
		}
	}
	// Disallow starting with a digit.
	if name[0] >= '0' && name[0] <= '9' {
		return false
	}
	return true
}
