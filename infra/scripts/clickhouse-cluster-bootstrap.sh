#!/usr/bin/env bash
# =============================================================================
# Personel Platform — ClickHouse Cluster Bootstrap (vm3 + vm5)
# Phase 5 Wave 2, Roadmap #44
# =============================================================================
#
# Prepares both hosts for a 2-node ClickHouse + 2-node Keeper cluster:
#   1. Pre-flight: verifies single-node CH is stopped, vm5 reachable,
#      Vault PKI is available, local TLS dir is writable.
#   2. Issues TLS server certs for clickhouse-01, clickhouse-02,
#      keeper-01, keeper-02 from the Vault PKI `server-cert` role.
#   3. Generates an interserver replication password + SHA-256 hash.
#   4. Emits an scp bundle for the operator to copy the right files to
#      vm5.
#   5. Prints the operator run sequence (start order, validation).
#
# The script is IDEMPOTENT: rerunning reissues certs only if they're
# missing or within 14 days of expiry.
#
# Usage:
#   ./clickhouse-cluster-bootstrap.sh            # full pre-flight + cert issue
#   ./clickhouse-cluster-bootstrap.sh --check    # pre-flight only, no changes
#   ./clickhouse-cluster-bootstrap.sh --force    # reissue certs unconditionally
# =============================================================================
set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_DIR="${REPO_ROOT}/compose"
TLS_DIR="${PERSONEL_TLS_DIR:-/etc/personel/tls}"
STAGING_DIR="${CLICKHOUSE_CLUSTER_STAGING_DIR:-/tmp/personel-ch-cluster-staging}"

VM3_IP="${VM3_IP:-192.168.5.44}"
VM5_IP="${VM5_IP:-192.168.5.32}"
VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
VAULT_PKI_MOUNT="${VAULT_PKI_MOUNT:-pki_int}"
VAULT_PKI_ROLE="${VAULT_PKI_ROLE:-server-cert}"

CHECK_ONLY=false
FORCE=false
for arg in "$@"; do
    case "${arg}" in
        --check) CHECK_ONLY=true ;;
        --force) FORCE=true ;;
        -h|--help)
            sed -n '2,28p' "${BASH_SOURCE[0]}"
            exit 0
            ;;
        *)
            echo "Unknown argument: ${arg}" >&2
            exit 2
            ;;
    esac
done

# -------------------------------------------------------------------------
# Logging helpers
# -------------------------------------------------------------------------
log()    { printf '[cluster-bootstrap] %s\n' "$*"; }
ok()     { printf '\033[0;32m[OK]\033[0m     %s\n' "$*"; }
warn()   { printf '\033[0;33m[WARN]\033[0m   %s\n' "$*" >&2; }
err()    { printf '\033[0;31m[ERROR]\033[0m  %s\n' "$*" >&2; }
section(){ printf '\n===== %s =====\n' "$*"; }

fatal() {
    err "$*"
    exit 1
}

# -------------------------------------------------------------------------
# Pre-flight checks
# -------------------------------------------------------------------------
preflight() {
    section "Pre-flight"

    # Required commands
    local cmd
    for cmd in vault jq openssl docker ssh scp; do
        if ! command -v "${cmd}" >/dev/null 2>&1; then
            fatal "required command not found: ${cmd}"
        fi
    done
    ok "required commands present"

    # Vault reachable + authenticated
    if ! VAULT_ADDR="${VAULT_ADDR}" vault token lookup >/dev/null 2>&1; then
        fatal "Vault at ${VAULT_ADDR} is unreachable or token is invalid"
    fi
    ok "Vault reachable and authenticated"

    # PKI mount present
    if ! VAULT_ADDR="${VAULT_ADDR}" vault read -format=json "${VAULT_PKI_MOUNT}/roles/${VAULT_PKI_ROLE}" >/dev/null 2>&1; then
        fatal "Vault PKI role ${VAULT_PKI_MOUNT}/${VAULT_PKI_ROLE} not found"
    fi
    ok "Vault PKI role ${VAULT_PKI_MOUNT}/${VAULT_PKI_ROLE} available"

    # TLS dir writable
    if [[ ! -d "${TLS_DIR}" ]]; then
        fatal "TLS directory ${TLS_DIR} does not exist (expected shared cert store)"
    fi
    if [[ ! -w "${TLS_DIR}" ]]; then
        fatal "TLS directory ${TLS_DIR} is not writable by current user"
    fi
    ok "TLS directory ${TLS_DIR} writable"

    # tenant_ca.crt must already exist (shared root chain)
    if [[ ! -f "${TLS_DIR}/tenant_ca.crt" ]]; then
        fatal "${TLS_DIR}/tenant_ca.crt missing — run ca-bootstrap.sh first"
    fi
    ok "tenant_ca.crt present"

    # vm5 reachable
    if ! ping -c 1 -W 2 "${VM5_IP}" >/dev/null 2>&1; then
        warn "vm5 (${VM5_IP}) did not respond to ICMP — SSH test will follow"
    fi
    if ! ssh -o BatchMode=yes -o ConnectTimeout=5 "kartal@${VM5_IP}" true 2>/dev/null; then
        fatal "cannot SSH to kartal@${VM5_IP} — configure key auth or set up ~/.ssh/config"
    fi
    ok "vm5 reachable via SSH"

    # Single-node CH must be stopped OR this is a first-time bring-up
    if docker ps --format '{{.Names}}' | grep -Fxq 'personel-clickhouse'; then
        warn "Single-node personel-clickhouse is still running."
        warn "You MUST stop it before starting the cluster:"
        warn "    docker compose -f ${COMPOSE_DIR}/docker-compose.yaml stop clickhouse"
        if [[ "${CHECK_ONLY}" == "false" ]]; then
            fatal "refusing to continue while single-node CH is running"
        fi
    else
        ok "single-node personel-clickhouse not running"
    fi

    # Data dirs exist or can be created
    local dd
    for dd in "/var/lib/personel/clickhouse-cluster/01" "/var/lib/personel/clickhouse-cluster/01/data" \
              "/var/lib/personel/clickhouse-cluster/01/logs" "/var/lib/personel/clickhouse-cluster/01/keeper"; do
        if [[ ! -d "${dd}" ]]; then
            if [[ "${CHECK_ONLY}" == "true" ]]; then
                warn "data dir missing: ${dd} (would be created)"
            else
                if ! mkdir -p "${dd}" 2>/dev/null; then
                    fatal "cannot create ${dd} — check permissions or run as root"
                fi
                ok "created ${dd}"
            fi
        fi
    done
}

# -------------------------------------------------------------------------
# Cert issue from Vault PKI
# -------------------------------------------------------------------------
# $1 common name    (clickhouse-01 / clickhouse-02 / keeper-01 / keeper-02)
# $2 extra SAN IP   (192.168.5.44 or 192.168.5.32)
# $3 output prefix  (full path without extension)
issue_cert() {
    local cn="$1"
    local san_ip="$2"
    local out_prefix="$3"
    local cert="${out_prefix}.crt"
    local key="${out_prefix}.key"

    # Idempotency: skip if cert exists and is not near expiry
    if [[ "${FORCE}" == "false" && -f "${cert}" && -f "${key}" ]]; then
        local end_epoch now_epoch days_left
        end_epoch=$(openssl x509 -in "${cert}" -noout -enddate 2>/dev/null | \
                    sed 's/^notAfter=//' | xargs -I {} date -d '{}' +%s 2>/dev/null || echo 0)
        now_epoch=$(date +%s)
        days_left=$(( (end_epoch - now_epoch) / 86400 ))
        if [[ "${days_left}" -gt 14 ]]; then
            ok "${cn}: cert valid for ${days_left} more days — skipping (use --force to reissue)"
            return 0
        fi
        warn "${cn}: cert expires in ${days_left} days — reissuing"
    fi

    log "issuing cert for ${cn} (SAN IP ${san_ip})"
    local response
    response=$(VAULT_ADDR="${VAULT_ADDR}" vault write -format=json \
        "${VAULT_PKI_MOUNT}/issue/${VAULT_PKI_ROLE}" \
        common_name="${cn}" \
        alt_names="${cn}" \
        ip_sans="127.0.0.1,${san_ip}" \
        ttl="8760h") || fatal "Vault issue for ${cn} failed"

    jq -r '.data.certificate'   <<<"${response}" > "${cert}"
    jq -r '.data.private_key'   <<<"${response}" > "${key}"
    jq -r '.data.issuing_ca'    <<<"${response}" >> "${cert}"
    chmod 0644 "${cert}"
    chmod 0600 "${key}"
    ok "${cn}: cert written to ${cert}"
}

# -------------------------------------------------------------------------
# Interserver replication password
# -------------------------------------------------------------------------
generate_interserver_secret() {
    section "Interserver replication secret"

    local secret_file="${STAGING_DIR}/ch-cluster-interserver.secret"
    local pw_sha_file="${STAGING_DIR}/ch-cluster-interserver.sha256"
    mkdir -p "${STAGING_DIR}"
    chmod 0700 "${STAGING_DIR}"

    if [[ "${FORCE}" == "false" && -f "${secret_file}" && -f "${pw_sha_file}" ]]; then
        ok "interserver secret already generated at ${secret_file}"
        return 0
    fi

    # 48-char random base64
    local pw
    pw=$(openssl rand -base64 36 | tr -d '\n')
    printf '%s\n' "${pw}" > "${secret_file}"
    chmod 0600 "${secret_file}"

    # SHA-256 hex digest for <password_sha256_hex>
    local sha
    sha=$(printf '%s' "${pw}" | openssl dgst -sha256 -hex | awk '{print $2}')
    printf '%s\n' "${sha}" > "${pw_sha_file}"
    chmod 0600 "${pw_sha_file}"

    ok "interserver secret and SHA-256 digest staged at ${STAGING_DIR}"
    warn "Operator MUST copy these into BOTH the vm3 .env and the vm5 .env:"
    warn "  CH_INTERSERVER_PASSWORD_SHA256=<contents of ${pw_sha_file}>"
}

# -------------------------------------------------------------------------
# Cluster password
# -------------------------------------------------------------------------
generate_cluster_password() {
    section "Cluster user password"
    local pw_file="${STAGING_DIR}/ch-cluster-password"
    mkdir -p "${STAGING_DIR}"
    chmod 0700 "${STAGING_DIR}"

    if [[ "${FORCE}" == "false" && -f "${pw_file}" ]]; then
        ok "cluster password already generated at ${pw_file}"
        return 0
    fi

    local pw
    pw=$(openssl rand -base64 24 | tr -d '\n')
    printf '%s\n' "${pw}" > "${pw_file}"
    chmod 0600 "${pw_file}"
    ok "cluster password staged at ${pw_file}"
    warn "Operator MUST copy into BOTH .env files as CH_CLUSTER_PASSWORD"
}

# -------------------------------------------------------------------------
# Scp bundle for vm5
# -------------------------------------------------------------------------
print_scp_bundle() {
    section "Files to copy from vm3 to vm5"

    local dest="${STAGING_DIR}/vm5-bundle"
    mkdir -p "${dest}"
    cp "${COMPOSE_DIR}/clickhouse/config.xml"                 "${dest}/"
    cp "${COMPOSE_DIR}/clickhouse/config.cluster.xml"         "${dest}/"
    cp "${COMPOSE_DIR}/clickhouse/users.xml"                  "${dest}/"
    cp "${COMPOSE_DIR}/clickhouse/keeper-config-02.xml"       "${dest}/"
    cp "${COMPOSE_DIR}/clickhouse/docker-compose.cluster-node2.yaml" "${dest}/"
    cp "${TLS_DIR}/tenant_ca.crt"                             "${dest}/"
    cp "${TLS_DIR}/clickhouse-02.crt"                         "${dest}/"
    cp "${TLS_DIR}/clickhouse-02.key"                         "${dest}/"
    cp "${TLS_DIR}/keeper-02.crt"                             "${dest}/"
    cp "${TLS_DIR}/keeper-02.key"                             "${dest}/"
    chmod 0600 "${dest}"/*.key
    ok "bundle staged at ${dest}"

    cat <<EOF

Operator run on vm3:
  ssh kartal@${VM5_IP} 'sudo mkdir -p /etc/personel/tls /opt/personel-cluster && sudo chown kartal:kartal /etc/personel/tls /opt/personel-cluster'
  scp ${dest}/tenant_ca.crt        kartal@${VM5_IP}:/etc/personel/tls/
  scp ${dest}/clickhouse-02.crt    kartal@${VM5_IP}:/etc/personel/tls/
  scp ${dest}/clickhouse-02.key    kartal@${VM5_IP}:/etc/personel/tls/
  scp ${dest}/keeper-02.crt        kartal@${VM5_IP}:/etc/personel/tls/
  scp ${dest}/keeper-02.key        kartal@${VM5_IP}:/etc/personel/tls/
  scp ${dest}/config.xml \\
      ${dest}/config.cluster.xml \\
      ${dest}/users.xml \\
      ${dest}/keeper-config-02.xml \\
      ${dest}/docker-compose.cluster-node2.yaml \\
      kartal@${VM5_IP}:/opt/personel-cluster/

Also copy the secrets into vm5's .env:
  CH_CLUSTER_PASSWORD            (from ${STAGING_DIR}/ch-cluster-password)
  CH_INTERSERVER_PASSWORD_SHA256 (from ${STAGING_DIR}/ch-cluster-interserver.sha256)
  CLICKHOUSE_CLUSTER_DATA_DIR_02=/var/lib/personel/clickhouse-cluster/02
EOF
}

# -------------------------------------------------------------------------
# Run sequence
# -------------------------------------------------------------------------
print_run_sequence() {
    section "Operator run sequence (bring-up order)"
    cat <<EOF

  1. On vm3: stop single-node CH
       docker compose -f ${COMPOSE_DIR}/docker-compose.yaml stop clickhouse

  2. On vm3: start cluster node-1 (CH + keeper)
       docker compose \\
         -f ${COMPOSE_DIR}/docker-compose.yaml \\
         -f ${COMPOSE_DIR}/clickhouse/docker-compose.cluster-node1.yaml \\
         up -d keeper-01 clickhouse-01

  3. Wait for keeper-01 healthcheck: up to 60 s
       docker logs -f personel-keeper-01 | grep -m1 'Ready for connections'

  4. On vm5: start cluster node-2 (CH + keeper)
       ssh kartal@${VM5_IP} 'cd /opt/personel-cluster && docker compose -f docker-compose.cluster-node2.yaml up -d'

  5. Wait for Raft quorum to form (keeper-01 sees keeper-02 as follower):
       docker exec personel-keeper-01 bash -c 'echo stat | nc -w 2 127.0.0.1 9181' | grep 'Mode'
       # Expect: Mode: leader (or follower, whichever won election)

  6. Run schema migration (converts MergeTree -> ReplicatedMergeTree):
       ${SCRIPT_DIR}/clickhouse-cluster-migrate-schemas.sh

  7. Run replication validation:
       ${SCRIPT_DIR}/clickhouse-cluster-test.sh

  8. Point the enricher + api at clickhouse-01 (primary writer). No
     config change needed — CLICKHOUSE_HOST=clickhouse-01 keeps resolving
     to vm3's primary replica and the ReplicatedMergeTree engine mirrors
     to clickhouse-02 over the interserver replication log.

EOF
}

# -------------------------------------------------------------------------
# Main
# -------------------------------------------------------------------------
main() {
    preflight

    if [[ "${CHECK_ONLY}" == "true" ]]; then
        ok "pre-flight only — no changes applied"
        return 0
    fi

    section "Issuing TLS certs from Vault PKI"
    issue_cert "clickhouse-01" "${VM3_IP}" "${TLS_DIR}/clickhouse-01"
    issue_cert "clickhouse-02" "${VM5_IP}" "${TLS_DIR}/clickhouse-02"
    issue_cert "keeper-01"     "${VM3_IP}" "${TLS_DIR}/keeper-01"
    issue_cert "keeper-02"     "${VM5_IP}" "${TLS_DIR}/keeper-02"

    generate_cluster_password
    generate_interserver_secret

    print_scp_bundle
    print_run_sequence

    section "Done"
    ok "cluster bootstrap staging complete"
}

main "$@"
