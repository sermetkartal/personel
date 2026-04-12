#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Environment Bootstrap
# TR: .env.example'daki her CHANGEME değeri için kriptografik olarak güvenli
#     rastgele değer üretir ve infra/compose/.env'e yazar.
# EN: Generates cryptographically secure random values for every CHANGEME
#     placeholder in .env.example and writes infra/compose/.env.
#
# Usage:
#   ./bootstrap-env.sh              — create .env (aborts if already exists)
#   ./bootstrap-env.sh --force      — overwrite existing .env
#   ./bootstrap-env.sh --dry-run    — print generated values without writing
#
# Security contract:
#   • Output file is chmod 600 (owner read/write only).
#   • Vault tokens / Secret IDs are NOT generated here.
#     They require the Shamir key ceremony (infra/runbooks/install.md §3).
#     Placeholders with "VAULT_TOKEN_PLACEHOLDER" are left as markers.
#   • SMTP credentials are intentionally left as CHANGEME_* — site-specific.
#   • This script is idempotent only in the --force direction.
#     Without --force it is strictly non-destructive.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"
EXAMPLE_FILE="${COMPOSE_DIR}/.env.example"
OUTPUT_FILE="${COMPOSE_DIR}/.env"

# ---------------------------------------------------------------------------
# Flag parsing
# ---------------------------------------------------------------------------
FORCE=false
DRY_RUN=false

for arg in "$@"; do
  case "${arg}" in
    --force)    FORCE=true ;;
    --dry-run)  DRY_RUN=true ;;
    --help|-h)
      sed -n '2,20p' "${BASH_SOURCE[0]}" | sed 's/^# \?//'
      exit 0
      ;;
    *)
      echo "[bootstrap-env] ERROR: Unknown flag: ${arg}" >&2
      exit 1
      ;;
  esac
done

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log()  { echo "[bootstrap-env] $*"; }
warn() { echo "[bootstrap-env] WARN: $*" >&2; }
die()  { echo "[bootstrap-env] ERROR: $*" >&2; exit 1; }

# rand_alnum <length>  — alphanumeric (A-Za-z0-9)
rand_alnum() {
  local len="${1:-32}"
  # LC_ALL=C ensures consistent character class on macOS and Linux
  LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c "${len}"
}

# rand_hex <bytes>  — lowercase hex, output length = bytes*2
rand_hex() {
  local bytes="${1:-32}"
  openssl rand -hex "${bytes}"
}

# rand_uuid — RFC 4122 v4 UUID without external tools
rand_uuid() {
  local hex
  hex="$(openssl rand -hex 16)"
  # Insert dashes: 8-4-4-4-12 and set version/variant bits
  printf '%s-%s-4%s-%x%s-%s\n' \
    "${hex:0:8}" \
    "${hex:8:4}" \
    "${hex:13:3}" \
    "$(( (0x${hex:16:1} & 0x3) | 0x8 ))" \
    "${hex:17:3}" \
    "${hex:20:12}"
}

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------
[[ -f "${EXAMPLE_FILE}" ]] || die ".env.example not found at: ${EXAMPLE_FILE}"

command -v openssl >/dev/null 2>&1 || die "'openssl' is required but not installed."

# ---------------------------------------------------------------------------
# Guard: refuse to overwrite without --force
# ---------------------------------------------------------------------------
if [[ -f "${OUTPUT_FILE}" ]] && [[ "${FORCE}" == "false" ]] && [[ "${DRY_RUN}" == "false" ]]; then
  warn "${OUTPUT_FILE} already exists."
  warn "Run with --force to overwrite or --dry-run to preview values."
  exit 0
fi

# ---------------------------------------------------------------------------
# Generate all secret values up front
# ---------------------------------------------------------------------------
log "Generating secrets..."

GEN_TENANT_UUID="$(rand_uuid)"
GEN_POSTGRES_PASSWORD="$(rand_alnum 32)"
GEN_CLICKHOUSE_APP_PASSWORD="$(rand_alnum 32)"
GEN_CLICKHOUSE_ADMIN_PASSWORD="$(rand_alnum 32)"
GEN_KEYCLOAK_ADMIN_PASSWORD="$(rand_alnum 24)"
GEN_KEYCLOAK_DB_PASSWORD="$(rand_alnum 24)"
GEN_NATS_STORE_ENCRYPTION_KEY="$(rand_hex 32)"
GEN_MINIO_ROOT_PASSWORD="$(rand_alnum 32)"
GEN_MINIO_GW_ACCESS_KEY="gwsvc$(rand_alnum 12)"
GEN_MINIO_GW_SECRET_KEY="$(rand_alnum 40)"
GEN_MINIO_DLP_ACCESS_KEY="dlpsvc$(rand_alnum 12)"
GEN_MINIO_DLP_SECRET_KEY="$(rand_alnum 40)"
GEN_MINIO_BACKUP_ACCESS_KEY="bkpsvc$(rand_alnum 12)"
GEN_MINIO_BACKUP_SECRET_KEY="$(rand_alnum 40)"
GEN_MINIO_AUDIT_SINK_ACCESS_KEY="auditsvc$(rand_alnum 10)"
GEN_MINIO_AUDIT_SINK_SECRET_KEY="$(rand_alnum 40)"
GEN_OPENSEARCH_ADMIN_PASSWORD="$(rand_alnum 24)!1Aa"   # meets OpenSearch min policy
GEN_LIVEKIT_API_KEY="personel_lk_$(rand_alnum 12)"
GEN_LIVEKIT_API_SECRET="$(rand_alnum 48)"
GEN_NEXTAUTH_SECRET="$(rand_hex 16)"
GEN_GRAFANA_ADMIN_PASSWORD="$(rand_alnum 24)"
GEN_BACKUP_GPG_PASSPHRASE="$(openssl rand -base64 32 | tr -d '\n')"

# ---------------------------------------------------------------------------
# Dry-run mode: print and exit
# ---------------------------------------------------------------------------
if [[ "${DRY_RUN}" == "true" ]]; then
  log "--- DRY RUN (nothing written) ---"
  cat <<EOF
PERSONEL_TENANT_ID=${GEN_TENANT_UUID}
POSTGRES_PASSWORD=${GEN_POSTGRES_PASSWORD}
CLICKHOUSE_PASSWORD=${GEN_CLICKHOUSE_APP_PASSWORD}
CLICKHOUSE_ADMIN_PASSWORD=${GEN_CLICKHOUSE_ADMIN_PASSWORD}
KEYCLOAK_ADMIN_PASSWORD=${GEN_KEYCLOAK_ADMIN_PASSWORD}
KEYCLOAK_DB_PASSWORD=${GEN_KEYCLOAK_DB_PASSWORD}
NATS_STORE_ENCRYPTION_KEY=${GEN_NATS_STORE_ENCRYPTION_KEY}
MINIO_ROOT_PASSWORD=${GEN_MINIO_ROOT_PASSWORD}
MINIO_GATEWAY_ACCESS_KEY=${GEN_MINIO_GW_ACCESS_KEY}
MINIO_GATEWAY_SECRET_KEY=${GEN_MINIO_GW_SECRET_KEY}
MINIO_DLP_ACCESS_KEY=${GEN_MINIO_DLP_ACCESS_KEY}
MINIO_DLP_SECRET_KEY=${GEN_MINIO_DLP_SECRET_KEY}
MINIO_BACKUP_ACCESS_KEY=${GEN_MINIO_BACKUP_ACCESS_KEY}
MINIO_BACKUP_SECRET_KEY=${GEN_MINIO_BACKUP_SECRET_KEY}
MINIO_AUDIT_SINK_ACCESS_KEY=${GEN_MINIO_AUDIT_SINK_ACCESS_KEY}
MINIO_AUDIT_SINK_SECRET_KEY=${GEN_MINIO_AUDIT_SINK_SECRET_KEY}
OPENSEARCH_ADMIN_PASSWORD=${GEN_OPENSEARCH_ADMIN_PASSWORD}
LIVEKIT_API_KEY=${GEN_LIVEKIT_API_KEY}
LIVEKIT_API_SECRET=${GEN_LIVEKIT_API_SECRET}
NEXTAUTH_SECRET=${GEN_NEXTAUTH_SECRET}
GRAFANA_ADMIN_PASSWORD=${GEN_GRAFANA_ADMIN_PASSWORD}
BACKUP_GPG_PASSPHRASE=${GEN_BACKUP_GPG_PASSPHRASE}
EOF
  exit 0
fi

# ---------------------------------------------------------------------------
# Build .env by substituting placeholders in .env.example
# ---------------------------------------------------------------------------
log "Writing ${OUTPUT_FILE} ..."

# We process line by line so we can add a generated-by header and handle
# each CHANGEME independently without a fragile sed chain.

{
  printf '# =============================================================================\n'
  printf '# AUTO-GENERATED by bootstrap-env.sh on %s\n' "$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  printf '# DO NOT COMMIT THIS FILE (it is in .gitignore).\n'
  printf '# Vault token / Secret-ID values require Shamir ceremony — see install.md §3.\n'
  printf '# =============================================================================\n\n'

  while IFS= read -r line; do
    # Pass comments and blanks through unchanged
    if [[ "${line}" =~ ^[[:space:]]*# ]] || [[ -z "${line}" ]]; then
      printf '%s\n' "${line}"
      continue
    fi

    # Extract key= portion
    key="${line%%=*}"
    value_raw="${line#*=}"

    case "${key}" in
      PERSONEL_TENANT_ID)
        printf '%s=%s\n' "${key}" "${GEN_TENANT_UUID}" ;;

      POSTGRES_PASSWORD)
        printf '%s=%s\n' "${key}" "${GEN_POSTGRES_PASSWORD}" ;;

      # Vault-managed dynamic secrets — placeholder comment inserted
      POSTGRES_APP_PASSWORD|POSTGRES_GATEWAY_PASSWORD|POSTGRES_DLP_RO_PASSWORD)
        printf '# VAULT_DYNAMIC_SECRET: managed by Vault database secrets engine\n'
        printf '%s=VAULT_DYNAMIC_SECRET_PENDING_CEREMONY\n' "${key}" ;;

      CLICKHOUSE_PASSWORD)
        printf '%s=%s\n' "${key}" "${GEN_CLICKHOUSE_APP_PASSWORD}" ;;

      CLICKHOUSE_ADMIN_PASSWORD)
        printf '%s=%s\n' "${key}" "${GEN_CLICKHOUSE_ADMIN_PASSWORD}" ;;

      NATS_STORE_ENCRYPTION_KEY)
        printf '%s=%s\n' "${key}" "${GEN_NATS_STORE_ENCRYPTION_KEY}" ;;

      # NATS operator JWT is set by bootstrap-nats.sh; leave placeholder
      NATS_OPERATOR_JWT)
        printf '# Set by bootstrap-nats.sh after nsc operator creation\n'
        printf '%s=PENDING_NATS_BOOTSTRAP\n' "${key}" ;;

      # NATS creds paths — set by bootstrap-nats.sh
      NATS_GATEWAY_CREDS|NATS_DLP_CREDS|NATS_API_CREDS|NATS_AUDIT_CREDS)
        printf '# Path written by bootstrap-nats.sh\n'
        printf '%s=PENDING_NATS_BOOTSTRAP\n' "${key}" ;;

      MINIO_ROOT_PASSWORD)
        printf '%s=%s\n' "${key}" "${GEN_MINIO_ROOT_PASSWORD}" ;;

      MINIO_GATEWAY_ACCESS_KEY)
        printf '%s=%s\n' "${key}" "${GEN_MINIO_GW_ACCESS_KEY}" ;;

      MINIO_GATEWAY_SECRET_KEY)
        printf '%s=%s\n' "${key}" "${GEN_MINIO_GW_SECRET_KEY}" ;;

      MINIO_DLP_ACCESS_KEY)
        printf '%s=%s\n' "${key}" "${GEN_MINIO_DLP_ACCESS_KEY}" ;;

      MINIO_DLP_SECRET_KEY)
        printf '%s=%s\n' "${key}" "${GEN_MINIO_DLP_SECRET_KEY}" ;;

      MINIO_BACKUP_ACCESS_KEY)
        printf '%s=%s\n' "${key}" "${GEN_MINIO_BACKUP_ACCESS_KEY}" ;;

      MINIO_BACKUP_SECRET_KEY)
        printf '%s=%s\n' "${key}" "${GEN_MINIO_BACKUP_SECRET_KEY}" ;;

      MINIO_AUDIT_SINK_ACCESS_KEY)
        printf '%s=%s\n' "${key}" "${GEN_MINIO_AUDIT_SINK_ACCESS_KEY}" ;;

      MINIO_AUDIT_SINK_SECRET_KEY)
        printf '%s=%s\n' "${key}" "${GEN_MINIO_AUDIT_SINK_SECRET_KEY}" ;;

      OPENSEARCH_ADMIN_PASSWORD)
        printf '%s=%s\n' "${key}" "${GEN_OPENSEARCH_ADMIN_PASSWORD}" ;;

      KEYCLOAK_ADMIN_PASSWORD)
        printf '%s=%s\n' "${key}" "${GEN_KEYCLOAK_ADMIN_PASSWORD}" ;;

      KEYCLOAK_DB_PASSWORD)
        printf '%s=%s\n' "${key}" "${GEN_KEYCLOAK_DB_PASSWORD}" ;;

      LIVEKIT_API_KEY)
        printf '%s=%s\n' "${key}" "${GEN_LIVEKIT_API_KEY}" ;;

      LIVEKIT_API_SECRET)
        printf '%s=%s\n' "${key}" "${GEN_LIVEKIT_API_SECRET}" ;;

      NEXTAUTH_SECRET)
        printf '%s=%s\n' "${key}" "${GEN_NEXTAUTH_SECRET}" ;;

      GRAFANA_ADMIN_PASSWORD)
        printf '%s=%s\n' "${key}" "${GEN_GRAFANA_ADMIN_PASSWORD}" ;;

      BACKUP_GPG_PASSPHRASE)
        printf '%s=%s\n' "${key}" "${GEN_BACKUP_GPG_PASSPHRASE}" ;;

      # Vault tokens / Secret IDs — require Shamir ceremony
      VAULT_ROLE_ID_GATEWAY|VAULT_ROLE_ID_API|VAULT_ROLE_ID_DLP|DLP_VAULT_ROLE_ID)
        printf '# VAULT_CEREMONY_REQUIRED: set during install.md §3 (Shamir unseal + AppRole provision)\n'
        printf '%s=VAULT_TOKEN_PLACEHOLDER\n' "${key}" ;;

      # SMTP / external addresses — must be filled manually
      KEYCLOAK_SMTP_HOST|KEYCLOAK_SMTP_USER|KEYCLOAK_SMTP_PASSWORD|KEYCLOAK_SMTP_FROM|\
      DPO_EMAIL|DPO_SECONDARY_EMAIL)
        printf '# MANUAL: fill in this value for your site\n'
        printf '%s=%s\n' "${key}" "${value_raw}" ;;

      # GPG / age keys — optional, site-specific
      BACKUP_GPG_RECIPIENT|BACKUP_AGE_RECIPIENT)
        printf '# OPTIONAL: set GPG fingerprint or age public key for asymmetric backup encryption\n'
        printf '%s=%s\n' "${key}" "${value_raw}" ;;

      # Everything else: pass through as-is
      *)
        printf '%s=%s\n' "${key}" "${value_raw}" ;;
    esac
  done < "${EXAMPLE_FILE}"
} > "${OUTPUT_FILE}"

# Secure the file
chmod 600 "${OUTPUT_FILE}"

log "Done. ${OUTPUT_FILE} written (chmod 600)."
log ""
log "NEXT STEPS:"
log "  1. Edit ${OUTPUT_FILE} — fill in MANUAL fields (SMTP, domain names, etc.)"
log "  2. Run: infra/scripts/bootstrap-nats.sh     — generates NATS operator JWT + creds"
log "  3. Run: infra/scripts/bootstrap-keycloak.sh — provisions Keycloak realm secrets"
log "  4. Follow infra/runbooks/install.md §3       — Shamir ceremony fills VAULT_TOKEN_PLACEHOLDER entries"
log "  5. Run: sudo infra/install.sh               — full stack install"
log ""
warn "BREAK-GLASS: store ${OUTPUT_FILE} contents in a sealed physical envelope."
warn "POSTGRES_PASSWORD and MINIO_ROOT_PASSWORD are master credentials."
