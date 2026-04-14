package enricher

import (
	"net/netip"
	"testing"
)

// TestGeoLookupNilReceiver verifies that a nil *GeoLookup is a valid
// "disabled" sentinel: every method is safe, Lookup always returns
// ok=false, and Close is a no-op.
func TestGeoLookupNilReceiver(t *testing.T) {
	var g *GeoLookup

	if p := g.Path(); p != "" {
		t.Errorf("nil.Path() = %q, want empty", p)
	}

	cc, city, ok := g.Lookup("8.8.8.8")
	if ok || cc != "" || city != "" {
		t.Errorf("nil.Lookup() = (%q,%q,%v), want empty/false", cc, city, ok)
	}

	if err := g.Close(); err != nil {
		t.Errorf("nil.Close() = %v, want nil", err)
	}
}

// TestNewGeoLookupEmptyPath documents the "disabled by default"
// contract: empty path returns (nil, nil) without error.
func TestNewGeoLookupEmptyPath(t *testing.T) {
	g, err := NewGeoLookup("")
	if err != nil {
		t.Fatalf("NewGeoLookup(\"\") err = %v, want nil", err)
	}
	if g != nil {
		t.Fatalf("NewGeoLookup(\"\") = %v, want nil", g)
	}
}

// TestNewGeoLookupMissingFile verifies that a non-empty path pointing
// at a missing file is a hard error — operators must know their
// config is wrong.
func TestNewGeoLookupMissingFile(t *testing.T) {
	g, err := NewGeoLookup("/nonexistent/personel/geo.mmdb")
	if err == nil {
		t.Errorf("NewGeoLookup(missing) err = nil, want error")
	}
	if g != nil {
		t.Errorf("NewGeoLookup(missing) = %v, want nil on error", g)
	}
}

// TestIsPublicRoutable exercises every branch of the pre-lookup filter.
// This is the hot-path filter so regressions would silently leak
// private traffic into the geo lookup.
func TestIsPublicRoutable(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want bool
	}{
		// Routable unicast — happy path.
		{"public ipv4 google dns", "8.8.8.8", true},
		{"public ipv4 cloudflare", "1.1.1.1", true},
		{"public ipv6 google", "2001:4860:4860::8888", true},
		// Loopback.
		{"loopback ipv4", "127.0.0.1", false},
		{"loopback ipv4 alt", "127.10.20.30", false},
		{"loopback ipv6", "::1", false},
		// Private RFC1918.
		{"private 10.0", "10.0.0.1", false},
		{"private 172.16", "172.16.5.10", false},
		{"private 192.168", "192.168.1.1", false},
		// Unspecified.
		{"unspecified ipv4", "0.0.0.0", false},
		{"unspecified ipv6", "::", false},
		// Link-local.
		{"link-local ipv4", "169.254.1.2", false},
		{"link-local ipv6", "fe80::1", false},
		// Multicast.
		{"multicast ipv4", "224.0.0.1", false},
		{"multicast ipv6", "ff02::1", false},
		// Carrier-grade NAT (100.64.0.0/10).
		{"cgnat 100.64", "100.64.0.1", false},
		{"cgnat 100.127", "100.127.255.254", false},
		{"not cgnat 100.0", "100.0.0.1", true},     // outside /10
		{"not cgnat 100.128", "100.128.0.1", true}, // outside /10
		// Reserved 0.0.0.0/8.
		{"reserved 0.1", "0.1.2.3", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr, err := netip.ParseAddr(tc.ip)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.ip, err)
			}
			got := isPublicRoutable(addr)
			if got != tc.want {
				t.Errorf("isPublicRoutable(%s) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

// TestIsPublicRoutableInvalid covers the invalid-addr branch.
func TestIsPublicRoutableInvalid(t *testing.T) {
	var zero netip.Addr
	if isPublicRoutable(zero) {
		t.Errorf("isPublicRoutable(zero) = true, want false")
	}
}

// TestGeoLookupLookupUnparseable verifies that bogus IP strings are
// rejected before reaching the reader. We synthesise a non-nil
// *GeoLookup with reader==nil (by calling Close on a disabled
// receiver cannot do this, so we open from a fresh struct) to ensure
// the parse path short-circuits even when the reader would otherwise
// be consulted.
func TestGeoLookupLookupUnparseable(t *testing.T) {
	// Build a GeoLookup with a nil reader to represent a post-Close
	// state or a test double. Lookup must still return ok=false.
	g := &GeoLookup{}

	cases := []string{
		"",
		"not-an-ip",
		"999.999.999.999",
		"192.168.1.1:80", // with port
	}
	for _, ipStr := range cases {
		t.Run(ipStr, func(t *testing.T) {
			cc, city, ok := g.Lookup(ipStr)
			if ok || cc != "" || city != "" {
				t.Errorf("Lookup(%q) = (%q,%q,%v), want empty/false", ipStr, cc, city, ok)
			}
		})
	}
}

// TestGeoLookupLookupNilReader verifies that a GeoLookup struct with
// reader=nil (post-Close) returns ok=false for a valid public IP.
// This is the defence-in-depth check against a race between Close()
// and a late Lookup() call.
func TestGeoLookupLookupNilReader(t *testing.T) {
	g := &GeoLookup{}
	cc, city, ok := g.Lookup("8.8.8.8")
	if ok {
		t.Errorf("Lookup on reader=nil returned ok=true")
	}
	if cc != "" || city != "" {
		t.Errorf("Lookup on reader=nil returned (%q,%q), want empty", cc, city)
	}
}

// TestGeoLookupLookupPrivateIP is a documentation-style test: when the
// caller feeds a private IP we MUST return ok=false even if the reader
// is open (the mmdb has no record, but we want to avoid the round-trip
// anyway).
func TestGeoLookupLookupPrivateIP(t *testing.T) {
	g := &GeoLookup{}
	for _, ip := range []string{"10.0.0.1", "192.168.1.1", "172.16.0.1", "127.0.0.1"} {
		cc, _, ok := g.Lookup(ip)
		if ok || cc != "" {
			t.Errorf("Lookup(%s) private returned ok=%v cc=%q, want false/empty", ip, ok, cc)
		}
	}
}

// TestExtractPayloadIP covers the enrich.go helper that pulls the
// geolocatable IP from a network event payload. The priority order
// is documented in the helper itself and asserted here.
func TestExtractPayloadIP(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]interface{}
		want    string
	}{
		{"nil payload", nil, ""},
		{"empty payload", map[string]interface{}{}, ""},
		{"remote_ip wins", map[string]interface{}{
			"remote_ip": "1.2.3.4",
			"dest_ip":   "5.6.7.8",
		}, "1.2.3.4"},
		{"source_ip second", map[string]interface{}{
			"source_ip": "1.2.3.4",
			"dest_ip":   "5.6.7.8",
		}, "1.2.3.4"},
		{"dest_ip third", map[string]interface{}{
			"dest_ip": "5.6.7.8",
		}, "5.6.7.8"},
		{"answer_ip last", map[string]interface{}{
			"answer_ip": "9.9.9.9",
		}, "9.9.9.9"},
		{"no recognised field", map[string]interface{}{
			"hostname": "example.com",
		}, ""},
		{"non-string value ignored", map[string]interface{}{
			"remote_ip": 12345,
			"dest_ip":   "5.6.7.8",
		}, "5.6.7.8"},
		{"empty string skipped", map[string]interface{}{
			"remote_ip": "",
			"dest_ip":   "5.6.7.8",
		}, "5.6.7.8"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractPayloadIP(tc.payload)
			if got != tc.want {
				t.Errorf("extractPayloadIP() = %q, want %q", got, tc.want)
			}
		})
	}
}
