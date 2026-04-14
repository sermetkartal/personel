#!/bin/bash
# maxmind-download.sh — Personel MaxMind GeoLite2 database updater.
#
# Runs via systemd timer (weekly). Pulls the latest GeoLite2 mmdb tarball
# from MaxMind's download endpoint, verifies its SHA256, extracts the
# single mmdb file, and atomically replaces the on-disk copy the
# enricher reads at startup.
#
# Credentials are read from environment variables supplied by the
# systemd EnvironmentFile (/etc/personel/maxmind.env). They are NEVER
# baked into the script or committed to the repository.
#
# Required env:
#   MAXMIND_ACCOUNT_ID   — numeric account id from MaxMind portal
#   MAXMIND_LICENSE_KEY  — license key from MaxMind portal
#
# Optional env:
#   MMDB_TARGET_DIR      — where to install the mmdb (default /var/lib/personel/geolite2)
#   MMDB_EDITION         — edition id (default GeoLite2-City)
#
# MaxMind GeoLite2 license terms (as of 2026-04): permits internal
# business use, prohibits redistribution and commercial resale.
# Personel uses the data purely for server-side enrichment of ingest
# events — no resale, no third-party redistribution.

set -euo pipefail

MMDB_TARGET_DIR="${MMDB_TARGET_DIR:-/var/lib/personel/geolite2}"
EDITION="${MMDB_EDITION:-GeoLite2-City}"

if [[ -z "${MAXMIND_ACCOUNT_ID:-}" || -z "${MAXMIND_LICENSE_KEY:-}" ]]; then
  echo "maxmind-download: MAXMIND_ACCOUNT_ID and MAXMIND_LICENSE_KEY must be set" >&2
  exit 2
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$MMDB_TARGET_DIR"

echo "maxmind-download: fetching ${EDITION} tarball…"
curl -fsSL \
  -u "${MAXMIND_ACCOUNT_ID}:${MAXMIND_LICENSE_KEY}" \
  "https://download.maxmind.com/app/geoip_download?edition_id=${EDITION}&suffix=tar.gz" \
  -o "$TMP/mmdb.tar.gz"

echo "maxmind-download: fetching ${EDITION} sha256…"
curl -fsSL \
  -u "${MAXMIND_ACCOUNT_ID}:${MAXMIND_LICENSE_KEY}" \
  "https://download.maxmind.com/app/geoip_download?edition_id=${EDITION}&suffix=tar.gz.sha256" \
  -o "$TMP/mmdb.sha256"

EXPECTED=$(awk '{print $1}' "$TMP/mmdb.sha256")
ACTUAL=$(sha256sum "$TMP/mmdb.tar.gz" | awk '{print $1}')
if [[ "$EXPECTED" != "$ACTUAL" ]]; then
  echo "maxmind-download: SHA256 mismatch — expected=$EXPECTED actual=$ACTUAL" >&2
  exit 1
fi
echo "maxmind-download: sha256 verified (${ACTUAL})"

tar -xzf "$TMP/mmdb.tar.gz" -C "$TMP"
MMDB_FILE=$(find "$TMP" -name "${EDITION}.mmdb" -print -quit)
if [[ -z "$MMDB_FILE" ]]; then
  echo "maxmind-download: ${EDITION}.mmdb not found in tarball" >&2
  exit 1
fi

# Atomic replace: write into a sibling .tmp and rename in place so a
# concurrent reader (the enricher) either sees the old copy or the new
# copy but never a truncated file.
install -m 0644 "$MMDB_FILE" "$MMDB_TARGET_DIR/${EDITION}.mmdb.tmp"
mv -f "$MMDB_TARGET_DIR/${EDITION}.mmdb.tmp" "$MMDB_TARGET_DIR/${EDITION}.mmdb"

echo "maxmind-download: ${EDITION}.mmdb updated at $MMDB_TARGET_DIR"
