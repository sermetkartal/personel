#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Preflight Check (HARDENED for Faz 13 #134)
# TR: Kurulum öncesi sistem gereksinimlerini sıkı şekilde doğrular.
# EN: Strictly validates system requirements before install.
#
# Exit codes:
#   0 — all checks pass (possibly with WARN)
#   1 — one or more FAIL items blocking install
#   2 — WARN only + --strict flag (treat warnings as fatal)
#
# Flags:
#   --json          emit machine-readable report to stdout
#   --strict        fail on any WARN
#   --data-dir DIR  override data mount point (default /var/lib/personel)
#   --log-dir DIR   override log mount point (default /var/log)
# =============================================================================
set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
DATA_DIR="/var/lib/personel"
LOG_MOUNT="/var/log"
EMIT_JSON=false
STRICT=false

for arg in "$@"; do
  case "${arg}" in
    --json)      EMIT_JSON=true ;;
    --strict)    STRICT=true ;;
    --data-dir=*)DATA_DIR="${arg#*=}" ;;
    --log-dir=*) LOG_MOUNT="${arg#*=}" ;;
  esac
done

# ---------------------------------------------------------------------------
# Result tracking
# ---------------------------------------------------------------------------
PASS=0; WARN=0; FAIL=0
RESULTS_JSON="["
SEP=""

record() {
  local status="$1" name="$2" msg="$3" remediation="${4:-}"
  RESULTS_JSON="${RESULTS_JSON}${SEP}{\"check\":\"${name}\",\"status\":\"${status}\",\"message\":\"${msg//\"/\\\"}\",\"remediation\":\"${remediation//\"/\\\"}\"}"
  SEP=","
}

green() {
  local name="$1" msg="$2"
  [[ "${EMIT_JSON}" == false ]] && echo -e "\033[0;32m[PASS]\033[0m ${msg}"
  PASS=$((PASS+1))
  record pass "${name}" "${msg}" ""
}
warn() {
  local name="$1" msg="$2" remediation="${3:-}"
  [[ "${EMIT_JSON}" == false ]] && echo -e "\033[1;33m[WARN]\033[0m ${msg}" >&2
  WARN=$((WARN+1))
  record warn "${name}" "${msg}" "${remediation}"
}
fail() {
  local name="$1" msg="$2" remediation="${3:-}"
  [[ "${EMIT_JSON}" == false ]] && echo -e "\033[0;31m[FAIL]\033[0m ${msg}" >&2
  FAIL=$((FAIL+1))
  record fail "${name}" "${msg}" "${remediation}"
}

hdr() { [[ "${EMIT_JSON}" == false ]] && echo -e "\n--- $* ---"; }
say() { [[ "${EMIT_JSON}" == false ]] && echo "$*"; }

[[ "${EMIT_JSON}" == false ]] && {
  echo ""
  echo "=== Personel Platform Preflight (hardened Faz 13 #134) ==="
  echo ""
}

# ---------------------------------------------------------------------------
# OS detection
# ---------------------------------------------------------------------------
hdr "OS"
OS_ID="unknown"; OS_VER="unknown"
if [[ -f /etc/os-release ]]; then
  # shellcheck source=/dev/null
  source /etc/os-release
  OS_ID="${ID}"
  OS_VER="${VERSION_ID:-unknown}"
  case "${OS_ID}" in
    ubuntu)
      IFS='.' read -r major _ _ <<< "${OS_VER}"
      if [[ "${major}" -ge 22 ]]; then
        green os.version "Ubuntu ${OS_VER} — supported"
      else
        fail os.version "Ubuntu ${OS_VER} below 22.04" "do-release-upgrade"
      fi
      ;;
    rhel|rocky|almalinux)
      IFS='.' read -r major _ <<< "${OS_VER}"
      if [[ "${major}" -ge 9 ]]; then
        green os.version "${PRETTY_NAME:-$OS_ID $OS_VER} — supported"
      else
        fail os.version "${OS_ID} ${OS_VER} below 9.x" "upgrade to RHEL 9+"
      fi
      ;;
    *)
      warn os.version "Untested OS (${OS_ID} ${OS_VER})" "use Ubuntu 22/24 or RHEL 9"
      ;;
  esac
else
  fail os.version "/etc/os-release missing" ""
fi

# ---------------------------------------------------------------------------
# Kernel
# ---------------------------------------------------------------------------
hdr "Kernel"
KERNEL=$(uname -r)
IFS='.' read -r kmajor kminor _ <<< "${KERNEL}"
# Minimum 5.4 per Faz 13 #134 spec (was 5.15 previously).
if [[ "${kmajor}" -gt 5 ]] || ( [[ "${kmajor}" -eq 5 ]] && [[ "${kminor}" -ge 4 ]] ); then
  green kernel.version "Kernel ${KERNEL} — OK"
else
  fail kernel.version "Kernel ${KERNEL} below 5.4" "upgrade kernel"
fi

# ---------------------------------------------------------------------------
# Docker + Compose
# ---------------------------------------------------------------------------
hdr "Docker"
if command -v docker &>/dev/null; then
  DOCKER_VERSION=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "unknown")
  IFS='.' read -r dmajor _ <<< "${DOCKER_VERSION}"
  if [[ "${dmajor}" -ge 25 ]] 2>/dev/null; then
    green docker.version "Docker ${DOCKER_VERSION} (>=25)"
  else
    fail docker.version "Docker ${DOCKER_VERSION} (<25)" "apt-get install docker-ce"
  fi
else
  fail docker.installed "Docker not installed" "https://docs.docker.com/engine/install/"
fi

if docker compose version &>/dev/null; then
  COMPOSE_VERSION=$(docker compose version --short 2>/dev/null || echo "v0")
  CMAJOR=$(echo "${COMPOSE_VERSION}" | sed 's/^v//' | awk -F. '{print $1}')
  CMINOR=$(echo "${COMPOSE_VERSION}" | sed 's/^v//' | awk -F. '{print $2}')
  if [[ "${CMAJOR}" -ge 2 ]] 2>/dev/null && [[ "${CMINOR}" -ge 20 ]] 2>/dev/null; then
    green compose.version "Docker Compose ${COMPOSE_VERSION} (>=v2.20)"
  else
    warn compose.version "Docker Compose ${COMPOSE_VERSION} below v2.20" "apt-get install docker-compose-plugin"
  fi
else
  fail compose.installed "docker compose plugin missing" "apt-get install docker-compose-plugin"
fi

if docker ps &>/dev/null; then
  green docker.socket "Docker socket accessible"
else
  fail docker.socket "Docker socket not accessible" "usermod -aG docker \${USER}"
fi

# ---------------------------------------------------------------------------
# CPU
# ---------------------------------------------------------------------------
hdr "CPU"
CPU_COUNT=$(nproc)
if [[ "${CPU_COUNT}" -ge 8 ]]; then
  green cpu.cores "CPU cores: ${CPU_COUNT} (>=8)"
else
  fail cpu.cores "CPU cores ${CPU_COUNT} below minimum 8" "resize VM"
fi

# ---------------------------------------------------------------------------
# Memory
# ---------------------------------------------------------------------------
hdr "Memory"
TOTAL_MEM_KB=$(grep MemTotal /proc/meminfo | awk '{print $2}')
TOTAL_MEM_GB=$((TOTAL_MEM_KB / 1024 / 1024))
if [[ "${TOTAL_MEM_GB}" -ge 16 ]]; then
  if [[ "${TOTAL_MEM_GB}" -ge 32 ]]; then
    green mem.total "RAM ${TOTAL_MEM_GB} GB (>=32 recommended)"
  else
    warn mem.total "RAM ${TOTAL_MEM_GB} GB below recommended 32 GB" "scale VM"
  fi
else
  fail mem.total "RAM ${TOTAL_MEM_GB} GB below minimum 16 GB" "scale VM"
fi

# ---------------------------------------------------------------------------
# Disk space
# ---------------------------------------------------------------------------
hdr "Disk"
# Create data dir if missing so df works.
[[ -d "${DATA_DIR}" ]] || DATA_DIR="$(dirname "${DATA_DIR}")"
DATA_AVAIL_GB=$(df -BG "${DATA_DIR}" 2>/dev/null | tail -1 | awk '{print $4}' | tr -d 'G' || echo "0")
if [[ "${DATA_AVAIL_GB}" -ge 100 ]]; then
  green disk.data "Data mount ${DATA_DIR}: ${DATA_AVAIL_GB}G free (>=100G)"
else
  fail disk.data "Data mount ${DATA_DIR}: ${DATA_AVAIL_GB}G free (<100G)" "extend LV"
fi

LOG_AVAIL_GB=$(df -BG "${LOG_MOUNT}" 2>/dev/null | tail -1 | awk '{print $4}' | tr -d 'G' || echo "0")
if [[ "${LOG_AVAIL_GB}" -ge 50 ]]; then
  green disk.log "Log mount ${LOG_MOUNT}: ${LOG_AVAIL_GB}G free (>=50G)"
else
  fail disk.log "Log mount ${LOG_MOUNT}: ${LOG_AVAIL_GB}G free (<50G)" "extend LV"
fi

# ---------------------------------------------------------------------------
# sysctl
# ---------------------------------------------------------------------------
hdr "sysctl"
MAP_COUNT=$(sysctl -n vm.max_map_count 2>/dev/null || echo 0)
if [[ "${MAP_COUNT}" -ge 262144 ]]; then
  green sysctl.max_map_count "vm.max_map_count=${MAP_COUNT} (>=262144)"
else
  fail sysctl.max_map_count "vm.max_map_count=${MAP_COUNT} (<262144)" "sysctl -w vm.max_map_count=262144"
fi

SOMAXCONN=$(sysctl -n net.core.somaxconn 2>/dev/null || echo 0)
if [[ "${SOMAXCONN}" -ge 1024 ]]; then
  green sysctl.somaxconn "net.core.somaxconn=${SOMAXCONN} (>=1024)"
else
  fail sysctl.somaxconn "net.core.somaxconn=${SOMAXCONN} (<1024)" "sysctl -w net.core.somaxconn=4096"
fi

# ---------------------------------------------------------------------------
# Required tools
# ---------------------------------------------------------------------------
hdr "Tools"
for tool in curl jq gpg python3 openssl nftables; do
  if command -v "${tool}" &>/dev/null || [[ "${tool}" == "nftables" && -e /usr/sbin/nft ]]; then
    green "tool.${tool}" "${tool} present"
  else
    if [[ "${tool}" == "nftables" ]]; then
      warn "tool.${tool}" "nft not installed" "apt-get install nftables"
    else
      fail "tool.${tool}" "${tool} missing" "apt-get install ${tool}"
    fi
  fi
done
if command -v step &>/dev/null; then
  green tool.step "step-cli present"
else
  warn tool.step "step-cli missing" "https://smallstep.com/docs/step-cli/installation/"
fi

# ---------------------------------------------------------------------------
# Port availability (Personel listener ports)
# ---------------------------------------------------------------------------
hdr "Ports"
PORTS=(5432 8123 9000 9200 8200 8080 8000 9443 3000 3001 4222 6222 443)
for p in "${PORTS[@]}"; do
  if ss -tlnp 2>/dev/null | awk '{print $4}' | grep -Eq ":${p}$"; then
    # Check if it's already a Personel container
    if docker ps --format '{{.Names}} {{.Ports}}' 2>/dev/null | grep -q "personel-.*:${p}->"; then
      warn "port.${p}" "Port ${p} in use by Personel container (idempotent re-run?)" ""
    else
      fail "port.${p}" "Port ${p} already in use by external service" "stop conflicting service"
    fi
  else
    green "port.${p}" "Port ${p} free"
  fi
done

# ---------------------------------------------------------------------------
# Conflicting containers
# ---------------------------------------------------------------------------
hdr "Containers"
if command -v docker &>/dev/null && docker ps &>/dev/null; then
  STRAY=$(docker ps --format '{{.Names}}' 2>/dev/null | grep -Ev '^personel-|^$' || true)
  if [[ -z "${STRAY}" ]]; then
    green docker.no_stray "No non-Personel containers running"
  else
    N=$(echo "${STRAY}" | wc -l)
    warn docker.stray "${N} non-Personel containers running" "review and stop if not needed"
  fi
fi

# ---------------------------------------------------------------------------
# Filesystem writability
# ---------------------------------------------------------------------------
hdr "Filesystem"
for d in /etc/personel/tls /var/lib/personel /var/log/personel; do
  if mkdir -p "${d}" 2>/dev/null && [[ -w "${d}" ]]; then
    green "fs.${d//\//_}" "${d} writable"
  else
    fail "fs.${d//\//_}" "${d} not writable" "chown -R personel:personel ${d}"
  fi
done

# ---------------------------------------------------------------------------
# DNS resolution (Keycloak hostname from .env if exists)
# ---------------------------------------------------------------------------
hdr "DNS"
KC_HOST="${PERSONEL_KEYCLOAK_HOST:-}"
if [[ -z "${KC_HOST}" && -f /opt/personel/infra/compose/.env ]]; then
  KC_HOST=$(grep -E '^PERSONEL_KEYCLOAK_HOST=' /opt/personel/infra/compose/.env | cut -d= -f2 | tr -d '"' || true)
fi
if [[ -n "${KC_HOST}" ]]; then
  if getent hosts "${KC_HOST}" &>/dev/null; then
    green dns.keycloak "Keycloak host ${KC_HOST} resolves"
  else
    warn dns.keycloak "Keycloak host ${KC_HOST} does not resolve" "add /etc/hosts or configure DNS"
  fi
else
  warn dns.keycloak "PERSONEL_KEYCLOAK_HOST unset — using localhost" ""
fi

# ---------------------------------------------------------------------------
# AppArmor + seccomp
# ---------------------------------------------------------------------------
hdr "Security"
if command -v apparmor_status &>/dev/null && apparmor_status --enabled 2>/dev/null; then
  green security.apparmor "AppArmor enabled"
else
  warn security.apparmor "AppArmor not enabled" "apt-get install apparmor-utils"
fi

if zcat /proc/config.gz 2>/dev/null | grep -q "CONFIG_SECCOMP=y" \
   || grep -q "CONFIG_SECCOMP=y" "/boot/config-$(uname -r)" 2>/dev/null; then
  green security.seccomp "Kernel seccomp supported"
else
  warn security.seccomp "Could not verify seccomp" "enable CONFIG_SECCOMP"
fi

RESULTS_JSON="${RESULTS_JSON}]"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
if [[ "${EMIT_JSON}" == true ]]; then
  printf '{"pass":%d,"warn":%d,"fail":%d,"results":%s}\n' \
    "${PASS}" "${WARN}" "${FAIL}" "${RESULTS_JSON}"
else
  echo ""
  echo "====================================="
  echo "  PASS: ${PASS}  WARN: ${WARN}  FAIL: ${FAIL}"
  echo "====================================="
  echo ""
fi

if [[ "${FAIL}" -gt 0 ]]; then
  [[ "${EMIT_JSON}" == false ]] && \
    echo -e "\033[0;31mPreflight FAILED — resolve FAIL items before installing.\033[0m"
  exit 1
fi
if [[ "${STRICT}" == true && "${WARN}" -gt 0 ]]; then
  [[ "${EMIT_JSON}" == false ]] && \
    echo -e "\033[1;33mStrict mode: WARN treated as FAIL.\033[0m"
  exit 2
fi
[[ "${EMIT_JSON}" == false ]] && \
  echo -e "\033[0;32mPreflight passed.\033[0m"
exit 0
