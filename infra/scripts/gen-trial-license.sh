#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Trial License Generator
# TR: Trial lisans dosyası üretir. Vendor Ed25519 private key'i ile imzalar.
# EN: Generates a trial license file. Signs with the vendor Ed25519 private key.
#
# Usage:
#   ./gen-trial-license.sh --customer-id acme-corp --max-endpoints 50 \
#       --days 30 --output /etc/personel/license.json
#
# Environment:
#   PERSONEL_VENDOR_PRIVATE_KEY   — path to Ed25519 priv key PEM (required)
#   PERSONEL_VENDOR_KEY_ID        — key identifier string (optional, default "personel-vendor-2026")
#
# Vendor key layout:
#   /opt/personel/keys/vendor-ed25519.pem        — PRIVATE key (keep offline)
#   /opt/personel/keys/vendor-ed25519.pub.pem    — PUBLIC key (compiled into API binary)
#
# This script is intended to be run on a SECURE host owned by Personel,
# NOT on the customer's production server. The output license.json is
# delivered to the customer out of band (USB / signed email / secure portal).
# =============================================================================
set -euo pipefail

CUSTOMER_ID=""
MAX_ENDPOINTS=50
DAYS=30
TIER="trial"
FEATURES="uba,ocr"
OUTPUT="/tmp/license.json"
FINGERPRINT=""
ONLINE_VALIDATION=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --customer-id)    CUSTOMER_ID="$2"; shift 2 ;;
        --max-endpoints)  MAX_ENDPOINTS="$2"; shift 2 ;;
        --days)           DAYS="$2"; shift 2 ;;
        --tier)           TIER="$2"; shift 2 ;;
        --features)       FEATURES="$2"; shift 2 ;;
        --output)         OUTPUT="$2"; shift 2 ;;
        --fingerprint)    FINGERPRINT="$2"; shift 2 ;;
        --online)         ONLINE_VALIDATION=true; shift ;;
        --help|-h)
            sed -n '2,24p' "$0"
            exit 0 ;;
        *) echo "Unknown option: $1" >&2; exit 1 ;;
    esac
done

if [[ -z "$CUSTOMER_ID" ]]; then
    echo "ERROR: --customer-id is required" >&2
    exit 1
fi

VENDOR_KEY="${PERSONEL_VENDOR_PRIVATE_KEY:-/opt/personel/keys/vendor-ed25519.pem}"
KEY_ID="${PERSONEL_VENDOR_KEY_ID:-personel-vendor-2026}"

if [[ ! -f "$VENDOR_KEY" ]]; then
    echo "ERROR: vendor private key not found at $VENDOR_KEY" >&2
    echo "       Set PERSONEL_VENDOR_PRIVATE_KEY env var." >&2
    exit 1
fi

# Compute timestamps
ISSUED_AT=$(date -u +"%Y-%m-%dT%H:%M:%S.000000000Z")
EXPIRES_AT=$(date -u -d "+${DAYS} days" +"%Y-%m-%dT%H:%M:%S.000000000Z" 2>/dev/null || \
             date -u -v+${DAYS}d +"%Y-%m-%dT%H:%M:%S.000000000Z")

# Convert features CSV to JSON array, sorted alphabetically
FEATURES_JSON=$(echo "$FEATURES" | tr ',' '\n' | sort | awk 'BEGIN{printf "["} {if(NR>1)printf ","; printf "\"%s\"", $0} END{printf "]"}')

# Build canonical claims (must match Go canonicalize() exactly)
if [[ -n "$FINGERPRINT" ]]; then
    CANONICAL=$(cat <<EOF
{"customer_id":"${CUSTOMER_ID}","expires_at":"${EXPIRES_AT}","features":${FEATURES_JSON},"fingerprint":"${FINGERPRINT}","issued_at":"${ISSUED_AT}","max_endpoints":${MAX_ENDPOINTS},"online_validation":${ONLINE_VALIDATION},"tier":"${TIER}"}
EOF
)
else
    CANONICAL=$(cat <<EOF
{"customer_id":"${CUSTOMER_ID}","expires_at":"${EXPIRES_AT}","features":${FEATURES_JSON},"issued_at":"${ISSUED_AT}","max_endpoints":${MAX_ENDPOINTS},"online_validation":${ONLINE_VALIDATION},"tier":"${TIER}"}
EOF
)
fi

# Sign with openssl (requires Ed25519 support: openssl 1.1.1+)
CANONICAL_STRIPPED=$(echo -n "$CANONICAL")
SIG_BASE64=$(echo -n "$CANONICAL_STRIPPED" | openssl pkeyutl -sign -inkey "$VENDOR_KEY" -rawin 2>/dev/null | base64 -w 0)

if [[ -z "$SIG_BASE64" ]]; then
    echo "ERROR: openssl signing failed — ensure vendor key is Ed25519 PEM" >&2
    exit 1
fi

# Build the full license file
mkdir -p "$(dirname "$OUTPUT")"
cat > "$OUTPUT" <<EOF
{
  "claims": ${CANONICAL_STRIPPED},
  "signature": "${SIG_BASE64}",
  "key_id": "${KEY_ID}"
}
EOF

chmod 0600 "$OUTPUT"

echo "==========================================================="
echo " Trial License Generated"
echo "==========================================================="
echo " Customer       : ${CUSTOMER_ID}"
echo " Tier           : ${TIER}"
echo " Max endpoints  : ${MAX_ENDPOINTS}"
echo " Features       : ${FEATURES}"
echo " Issued         : ${ISSUED_AT}"
echo " Expires        : ${EXPIRES_AT}  (${DAYS} days)"
echo " Fingerprint    : ${FINGERPRINT:-<unbound>}"
echo " Online         : ${ONLINE_VALIDATION}"
echo " Key ID         : ${KEY_ID}"
echo " Output         : ${OUTPUT}"
echo "==========================================================="
echo ""
echo "Delivery: send ${OUTPUT} to customer via secure channel."
echo "Customer drops it at /etc/personel/license.json on the API host."
