#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Preflight Check
# TR: Kurulum öncesi sistem gereksinimlerini doğrular. Hata varsa çıkar.
# EN: Validates system requirements before install. Exits on failure.
# =============================================================================
set -euo pipefail

PASS=0; WARN=0; FAIL=0

green() { echo -e "\033[0;32m[PASS]\033[0m $*"; ((PASS++)) || true; }
warn()  { echo -e "\033[1;33m[WARN]\033[0m $*" >&2; ((WARN++)) || true; }
fail()  { echo -e "\033[0;31m[FAIL]\033[0m $*" >&2; ((FAIL++)) || true; }

echo ""
echo "=== Personel Platform Preflight Check ==="
echo ""

# ---------------------------------------------------------------------------
# OS Version
# ---------------------------------------------------------------------------
echo "--- OS Version ---"
if [[ -f /etc/os-release ]]; then
  # shellcheck source=/dev/null
  source /etc/os-release
  if [[ "${ID}" == "ubuntu" ]]; then
    IFS='.' read -r major minor _ <<< "${VERSION_ID}"
    if [[ "${major}" -ge 22 ]]; then
      green "Ubuntu ${VERSION_ID} — supported"
    else
      fail "Ubuntu ${VERSION_ID} — minimum is 22.04. Upgrade OS."
    fi
  else
    warn "Non-Ubuntu OS (${ID} ${VERSION_ID}) — untested. Proceed at your own risk."
  fi
else
  fail "Cannot determine OS version (/etc/os-release not found)"
fi

# ---------------------------------------------------------------------------
# Kernel version (minimum 5.15 for cgroup v2 + memlock)
# ---------------------------------------------------------------------------
KERNEL=$(uname -r)
IFS='.' read -r kmajor kminor _ <<< "${KERNEL}"
if [[ "${kmajor}" -gt 5 ]] || ( [[ "${kmajor}" -eq 5 ]] && [[ "${kminor}" -ge 15 ]] ); then
  green "Kernel ${KERNEL} — OK"
else
  fail "Kernel ${KERNEL} — minimum 5.15 required for cgroup v2 and memlock support"
fi

# ---------------------------------------------------------------------------
# Docker
# ---------------------------------------------------------------------------
echo ""
echo "--- Docker ---"
if command -v docker &>/dev/null; then
  DOCKER_VERSION=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "unknown")
  IFS='.' read -r dmajor _ <<< "${DOCKER_VERSION}"
  if [[ "${dmajor}" -ge 25 ]]; then
    green "Docker ${DOCKER_VERSION} — OK"
  else
    fail "Docker ${DOCKER_VERSION} — minimum Docker 25 required"
  fi
else
  fail "Docker not installed. Install from https://docs.docker.com/engine/install/ubuntu/"
fi

# Docker Compose v2
if docker compose version &>/dev/null; then
  COMPOSE_VERSION=$(docker compose version --short 2>/dev/null || echo "unknown")
  green "Docker Compose v2 ${COMPOSE_VERSION} — OK"
else
  fail "Docker Compose v2 plugin not installed (need: docker compose, not docker-compose)"
fi

# Docker socket accessible
if docker ps &>/dev/null; then
  green "Docker socket accessible"
else
  fail "Cannot connect to Docker socket. Is the current user in the docker group?"
fi

# ---------------------------------------------------------------------------
# CPU
# ---------------------------------------------------------------------------
echo ""
echo "--- CPU ---"
CPU_COUNT=$(nproc)
if [[ "${CPU_COUNT}" -ge 8 ]]; then
  green "CPU cores: ${CPU_COUNT} (minimum 8, recommended 16)"
elif [[ "${CPU_COUNT}" -ge 4 ]]; then
  warn "CPU cores: ${CPU_COUNT} — minimum met for testing; production needs 16+"
else
  fail "CPU cores: ${CPU_COUNT} — minimum 4 cores required"
fi

# ---------------------------------------------------------------------------
# Memory
# ---------------------------------------------------------------------------
echo ""
echo "--- Memory ---"
TOTAL_MEM_KB=$(grep MemTotal /proc/meminfo | awk '{print $2}')
TOTAL_MEM_GB=$((TOTAL_MEM_KB / 1024 / 1024))
if [[ "${TOTAL_MEM_GB}" -ge 32 ]]; then
  green "RAM: ${TOTAL_MEM_GB} GB (minimum 32 GB, recommended 64 GB)"
elif [[ "${TOTAL_MEM_GB}" -ge 16 ]]; then
  warn "RAM: ${TOTAL_MEM_GB} GB — below recommended 64 GB; ClickHouse performance may be limited"
else
  fail "RAM: ${TOTAL_MEM_GB} GB — minimum 16 GB required"
fi

# ---------------------------------------------------------------------------
# Disk space
# ---------------------------------------------------------------------------
echo ""
echo "--- Disk Space ---"
DATA_MOUNT="${1:-/var/lib}"
AVAIL_GB=$(df -BG "${DATA_MOUNT}" | tail -1 | awk '{print $4}' | tr -d 'G')
if [[ "${AVAIL_GB}" -ge 500 ]]; then
  green "Disk available: ${AVAIL_GB} GB on ${DATA_MOUNT} (minimum 500 GB)"
elif [[ "${AVAIL_GB}" -ge 200 ]]; then
  warn "Disk available: ${AVAIL_GB} GB — below recommended 1 TB; monitor disk usage"
else
  fail "Disk available: ${AVAIL_GB} GB on ${DATA_MOUNT} — minimum 200 GB required"
fi

# ---------------------------------------------------------------------------
# Required tools
# ---------------------------------------------------------------------------
echo ""
echo "--- Required Tools ---"
for tool in curl gpg python3 openssl step; do
  if command -v "${tool}" &>/dev/null; then
    green "${tool} — found"
  else
    if [[ "${tool}" == "step" ]]; then
      warn "step-cli not found — required for PKI bootstrap. Install from: https://smallstep.com/docs/step-cli/installation/"
    else
      fail "${tool} — not found. Install with: apt-get install ${tool}"
    fi
  fi
done

# ---------------------------------------------------------------------------
# Network ports (check nothing is already bound to Personel ports)
# ---------------------------------------------------------------------------
echo ""
echo "--- Port Availability ---"
for port in 443 80 9443; do
  if ss -tlnp | grep -q ":${port} "; then
    warn "Port ${port} is already in use — Personel may conflict"
  else
    green "Port ${port} — available"
  fi
done

# ---------------------------------------------------------------------------
# AppArmor
# ---------------------------------------------------------------------------
echo ""
echo "--- Security Features ---"
if command -v apparmor_status &>/dev/null; then
  if apparmor_status --enabled 2>/dev/null; then
    green "AppArmor — enabled (required for DLP Profile 1 hardening)"
  else
    warn "AppArmor — installed but not enabled. DLP will run without AppArmor confinement."
  fi
else
  warn "AppArmor — not installed. DLP will rely on seccomp profile only."
fi

# Check seccomp support
if grep -q "seccomp" /boot/config-$(uname -r) 2>/dev/null || \
   zcat /proc/config.gz 2>/dev/null | grep -q "CONFIG_SECCOMP=y"; then
  green "Kernel seccomp — supported"
else
  warn "Could not verify seccomp kernel support"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "====================================="
echo "  PASS: ${PASS}  WARN: ${WARN}  FAIL: ${FAIL}"
echo "====================================="
echo ""

if [[ "${FAIL}" -gt 0 ]]; then
  echo -e "\033[0;31mPreflight FAILED — resolve the above FAIL items before installing.\033[0m"
  exit 1
elif [[ "${WARN}" -gt 0 ]]; then
  echo -e "\033[1;33mPreflight passed with warnings. Review WARN items.\033[0m"
  exit 0
else
  echo -e "\033[0;32mAll preflight checks passed. Ready to install.\033[0m"
  exit 0
fi
