// Package enricher — geo.go implements server-side MaxMind GeoLite2
// lookup for inbound network events. The enricher consults this module
// during Enrich() and, when a public IP is available in the payload,
// attaches geo_country_code + geo_city_name to the payload JSON that
// eventually lands in ClickHouse.
//
// Design notes:
//
//   - The mmdb file is NOT shipped with the repo. MaxMind's GeoLite2
//     license permits internal business use but prohibits
//     redistribution, so each pilot must download the database via
//     `infra/scripts/maxmind-download.sh`, which runs weekly under
//     systemd (see `infra/systemd/personel-maxmind-download.*`).
//
//   - The reader is opened once at enricher boot. Concurrent readers
//     are safe per the maxminddb-golang/v2 library guarantees; we
//     still wrap the reader in an RWMutex so a future hot-reload path
//     can replace it without restarting the process.
//
//   - Private, loopback, link-local, unspecified, and multicast
//     addresses are skipped: geo lookup on RFC1918 yields meaningless
//     results and wastes a pointer chase. The upstream mmdb actually
//     contains no records for those ranges, but checking here lets us
//     fail fast without touching the reader.
//
//   - When the lookup file is missing at boot (or the configured path
//     is empty), NewGeoLookup returns a (nil, nil) sentinel. The
//     consumer pipeline accepts a nil *GeoLookup and simply skips
//     enrichment — this is deliberate: customers running without a
//     MaxMind license should still get a working enricher.
package enricher

import (
	"fmt"
	"net/netip"
	"sync"

	"github.com/oschwald/maxminddb-golang/v2"
)

// GeoLookup is a thread-safe wrapper around a maxminddb Reader.
// The zero value is NOT usable — always construct via NewGeoLookup.
// A nil *GeoLookup is treated as "geo lookup disabled" and the Lookup
// method is safe to call on nil (returns ok=false).
type GeoLookup struct {
	mu     sync.RWMutex
	reader *maxminddb.Reader
	path   string
}

// GeoRecord is the minimal subset of the MaxMind GeoLite2-City schema
// the enricher consumes. Adding fields here is a schema expansion;
// existing records remain decodable because maxminddb tolerates
// unknown tags on either side.
type GeoRecord struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
}

// NewGeoLookup opens the mmdb file at path and returns a ready
// GeoLookup. If path is empty, NewGeoLookup returns (nil, nil) —
// callers should treat that as "geo enrichment disabled". If path is
// non-empty but the open fails, the error is returned so the enricher
// operator sees a hard failure (misconfiguration is loud).
func NewGeoLookup(path string) (*GeoLookup, error) {
	if path == "" {
		return nil, nil
	}
	r, err := maxminddb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("geoip: open %s: %w", path, err)
	}
	return &GeoLookup{reader: r, path: path}, nil
}

// Path returns the file path the reader was opened with, or "" when
// the lookup is disabled. Intended for log lines at boot time.
func (g *GeoLookup) Path() string {
	if g == nil {
		return ""
	}
	return g.path
}

// Lookup resolves ipStr to a country code and city name. Returns
// ok=false when:
//   - the receiver is nil (lookup disabled)
//   - ipStr is empty or unparseable
//   - the address is private / loopback / link-local / unspecified / multicast
//   - the mmdb has no record for the address
//   - the decode fails (should never happen in practice)
//
// The English city name ("en") is returned when present. Country code
// is the ISO 3166-1 alpha-2 code (e.g. "TR", "DE"). Both values are
// empty strings when the reader has no data for the given IP — the
// caller should check ok, not the strings.
func (g *GeoLookup) Lookup(ipStr string) (countryCode, city string, ok bool) {
	if g == nil {
		return "", "", false
	}
	if ipStr == "" {
		return "", "", false
	}
	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		return "", "", false
	}
	if !isPublicRoutable(addr) {
		return "", "", false
	}

	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.reader == nil {
		return "", "", false
	}

	var rec GeoRecord
	if err := g.reader.Lookup(addr).Decode(&rec); err != nil {
		return "", "", false
	}
	if rec.Country.ISOCode == "" {
		return "", "", false
	}
	city = rec.City.Names["en"]
	return rec.Country.ISOCode, city, true
}

// Close releases the underlying mmdb reader. Safe to call on nil.
func (g *GeoLookup) Close() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.reader == nil {
		return nil
	}
	err := g.reader.Close()
	g.reader = nil
	return err
}

// isPublicRoutable returns true only for addresses that can meaningfully
// be geolocated. Any reserved / private / unspecified range short-
// circuits the lookup and saves an mmdb page fault.
func isPublicRoutable(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	if addr.IsUnspecified() {
		return false
	}
	if addr.IsLoopback() {
		return false
	}
	if addr.IsPrivate() {
		return false
	}
	if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return false
	}
	if addr.IsMulticast() {
		return false
	}
	// Carrier-grade NAT (100.64.0.0/10, RFC6598) — unroutable globally.
	if addr.Is4() {
		b := addr.As4()
		if b[0] == 100 && (b[1]&0xc0) == 64 {
			return false
		}
		// 0.0.0.0/8 reserved.
		if b[0] == 0 {
			return false
		}
	}
	return true
}
