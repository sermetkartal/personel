#!/usr/bin/env bash
# =============================================================================
# Personel Platform — PKI Bootstrap Script
# Offline Root CA ceremony wrapper using step-cli.
# Per pki-bootstrap.md §2 and §3.
#
# IMPORTANT: For a full air-gapped ceremony, follow pki-bootstrap.md exactly.
# This script automates the online parts (Vault PKI setup) and generates
# self-signed certs for development/staging. For production, the tenant CA
# must be signed by the offline Root CA on an air-gapped machine.
#
# TR: Üretim ortamı için kök CA imzalama, hava boşluklu makinede yapılmalıdır.
# EN: For production, root CA signing must be performed on an air-gapped machine.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"

set -a; source "${COMPOSE_DIR}/.env" 2>/dev/null || true; set +a

TLS_DIR="${TLS_DIR:-/etc/personel/tls}"
TENANT_ID="${PERSONEL_TENANT_ID:-default}"
VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
NON_INTERACTIVE=false
DEV_MODE=false

for arg in "$@"; do
  case "${arg}" in
    --tenant-id=*)     TENANT_ID="${arg#--tenant-id=}" ;;
    --tls-dir=*)       TLS_DIR="${arg#--tls-dir=}" ;;
    --non-interactive) NON_INTERACTIVE=true ;;
    --dev-mode)        DEV_MODE=true ;;
  esac
done

log() { echo "[ca-bootstrap] $*"; }
die() { echo "[ca-bootstrap] ERROR: $*" >&2; exit 1; }
warn() { echo "[ca-bootstrap] WARN: $*" >&2; }

mkdir -p "${TLS_DIR}"
chmod 750 "${TLS_DIR}"

# ---------------------------------------------------------------------------
if [[ "${DEV_MODE}" == "true" ]]; then
  log "=== DEV MODE: generating self-signed certs for local testing ==="
  warn "Dev-mode certs are NOT suitable for production."

  # Generate root CA
  if [[ ! -f "${TLS_DIR}/root_ca.crt" ]]; then
    openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-384 \
      -days 3650 -noenc \
      -keyout "${TLS_DIR}/root_ca.key" \
      -out "${TLS_DIR}/root_ca.crt" \
      -subj "/C=TR/O=Personel Dev/CN=Personel Dev Root CA"
    chmod 600 "${TLS_DIR}/root_ca.key"
    log "Generated dev Root CA"
  fi

  # Generate tenant CA signed by root
  for service in vault postgres clickhouse nats opensearch gateway; do
    if [[ ! -f "${TLS_DIR}/${service}.crt" ]]; then
      openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -noenc \
        -keyout "${TLS_DIR}/${service}.key" \
        -out "${TLS_DIR}/${service}.csr" \
        -subj "/C=TR/O=Personel Dev/CN=${service}.personel.internal"
      openssl x509 -req \
        -in "${TLS_DIR}/${service}.csr" \
        -CA "${TLS_DIR}/root_ca.crt" \
        -CAkey "${TLS_DIR}/root_ca.key" \
        -CAcreateserial \
        -days 90 \
        -extfile <(echo "subjectAltName=DNS:${service},DNS:${service}.personel.internal,DNS:localhost,IP:127.0.0.1") \
        -out "${TLS_DIR}/${service}.crt"
      cp "${TLS_DIR}/root_ca.crt" "${TLS_DIR}/tenant_ca.crt"
      rm -f "${TLS_DIR}/${service}.csr"
      chmod 600 "${TLS_DIR}/${service}.key"
      log "Generated dev cert for ${service}"
    fi
  done
  log "Dev TLS certs ready in ${TLS_DIR}"
  exit 0
fi

# ---------------------------------------------------------------------------
# Production: check step-cli is available
# ---------------------------------------------------------------------------
command -v step &>/dev/null || die "step-cli not found. Install from https://smallstep.com/docs/step-cli/installation/"

log "=== Personel PKI Bootstrap ==="
log "Tenant ID: ${TENANT_ID}"
log "TLS directory: ${TLS_DIR}"
log ""

if [[ "${NON_INTERACTIVE}" != "true" ]]; then
  echo "This will bootstrap the Vault PKI for tenant: ${TENANT_ID}"
  echo "Prerequisites:"
  echo "  1. Vault is running and unsealed"
  echo "  2. Root CA ceremony has been performed per pki-bootstrap.md §3"
  echo "  3. tenant_ca.crt and root_ca.crt are already in ${TLS_DIR}"
  echo ""
  read -r -p "Continue? [y/N]: " CONFIRM
  [[ "${CONFIRM}" =~ ^[Yy]$ ]] || exit 0
fi

# ---------------------------------------------------------------------------
# Verify root and tenant CA exist
# ---------------------------------------------------------------------------
if [[ ! -f "${TLS_DIR}/tenant_ca.crt" ]]; then
  warn "tenant_ca.crt not found."
  warn "For production: perform the air-gapped ceremony per pki-bootstrap.md §3"
  warn "For development: run with --dev-mode flag"

  if [[ "${NON_INTERACTIVE}" == "true" ]]; then
    log "Non-interactive mode: generating temporary self-signed tenant CA for bootstrap"
    openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
      -days 1095 -noenc \
      -keyout "${TLS_DIR}/tenant_ca.key" \
      -out "${TLS_DIR}/tenant_ca.crt" \
      -subj "/C=TR/O=Personel/CN=Personel Tenant CA ${TENANT_ID}"
    cp "${TLS_DIR}/tenant_ca.crt" "${TLS_DIR}/root_ca.crt"
    warn "TEMPORARY cert generated. Replace with ceremony-signed cert before pilot."
  else
    die "No tenant CA found. Perform root ceremony or use --dev-mode."
  fi
fi

# ---------------------------------------------------------------------------
# Generate service certs using Vault PKI (after vault-setup completes)
# ---------------------------------------------------------------------------
log "Vault PKI integration: service certs will be issued by vault-agent sidecar"
log "Static bootstrap certs for Vault itself..."

# Generate a bootstrap cert for Vault TLS (self-signed from tenant CA)
if [[ ! -f "${TLS_DIR}/vault.crt" ]]; then
  openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -noenc \
    -keyout "${TLS_DIR}/vault.key" \
    -out "${TLS_DIR}/vault.csr" \
    -subj "/C=TR/O=Personel/CN=vault.personel.internal"
  openssl x509 -req \
    -in "${TLS_DIR}/vault.csr" \
    -CA "${TLS_DIR}/tenant_ca.crt" \
    -CAkey "${TLS_DIR}/tenant_ca.key" \
    -CAcreateserial \
    -days 90 \
    -extfile <(echo "subjectAltName=DNS:vault,DNS:vault.personel.internal,DNS:localhost,IP:127.0.0.1") \
    -out "${TLS_DIR}/vault.crt"
  rm -f "${TLS_DIR}/vault.csr"
  chmod 600 "${TLS_DIR}/vault.key"
  log "Generated bootstrap cert for Vault"
fi

# Postgres bootstrap cert
if [[ ! -f "${TLS_DIR}/postgres.crt" ]]; then
  openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -noenc \
    -keyout "${TLS_DIR}/postgres.key" \
    -out "${TLS_DIR}/postgres.csr" \
    -subj "/C=TR/O=Personel/CN=postgres.personel.internal"
  openssl x509 -req \
    -in "${TLS_DIR}/postgres.csr" \
    -CA "${TLS_DIR}/tenant_ca.crt" \
    -CAkey "${TLS_DIR}/tenant_ca.key" \
    -CAcreateserial -days 90 \
    -extfile <(echo "subjectAltName=DNS:postgres,DNS:postgres.personel.internal") \
    -out "${TLS_DIR}/postgres.crt"
  rm -f "${TLS_DIR}/postgres.csr"
  chmod 600 "${TLS_DIR}/postgres.key"
  log "Generated bootstrap cert for Postgres"
fi

log "PKI bootstrap complete. All bootstrap certs in ${TLS_DIR}/"
log "TR: Üretim sertifikaları Vault PKI tarafından otomatik olarak yenilenecektir."
log "EN: Production certs will be auto-renewed by Vault PKI via vault-agent."
