#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Host Firewall (nftables) Apply Script (Faz 13 #141)
# TR: Ubuntu/RHEL host üzerinde nftables kuralını uygular.
# EN: Applies nftables ruleset to Ubuntu/RHEL host.
#
# Usage:
#   sudo ./firewall-apply.sh [--dry-run] [--endpoint-cidr=10.0.0.0/8]
#                            [--bastion-cidr=192.168.100.0/24]
#                            [--monitoring-cidr=192.168.200.0/24]
#
# Defaults assume no segmentation and accept endpoint gateway from anywhere.
# Override via environment or flags for production deployment.
# =============================================================================
set -euo pipefail

DRY_RUN=false
BASTION_CIDR="${PERSONEL_BASTION_CIDR:-}"
ENDPOINT_CIDR="${PERSONEL_ENDPOINT_CIDR:-0.0.0.0/0}"
MONITORING_CIDR="${PERSONEL_MONITORING_CIDR:-127.0.0.1/32}"
PUBLIC_CIDR="${PERSONEL_PUBLIC_CIDR:-0.0.0.0/0}"
RULESET_FILE="/etc/nftables-personel.conf"

for arg in "$@"; do
  case "${arg}" in
    --dry-run)           DRY_RUN=true ;;
    --bastion-cidr=*)    BASTION_CIDR="${arg#*=}" ;;
    --endpoint-cidr=*)   ENDPOINT_CIDR="${arg#*=}" ;;
    --monitoring-cidr=*) MONITORING_CIDR="${arg#*=}" ;;
    --public-cidr=*)     PUBLIC_CIDR="${arg#*=}" ;;
    -h|--help)
      sed -n '/^# Usage:/,/^$/p' "$0" | sed 's/^# //'
      exit 0
      ;;
  esac
done

[[ "${EUID}" -eq 0 ]] || { echo "Run as root"; exit 1; }

if ! command -v nft &>/dev/null; then
  echo "ERROR: nft not installed. apt-get install nftables" >&2
  exit 1
fi

if [[ -z "${BASTION_CIDR}" ]]; then
  echo "WARN: --bastion-cidr not set; SSH allowed from anywhere. Set for production." >&2
  BASTION_CIDR="0.0.0.0/0"
fi

cat > "${RULESET_FILE}" <<NFT
# =============================================================================
# Personel Platform — nftables ruleset
# Applied by infra/scripts/firewall-apply.sh
# =============================================================================
flush ruleset

table inet personel {

    # -----------------------------------------------------------------
    # Named sets — allow sourcecidrs
    # -----------------------------------------------------------------
    set bastion_cidr {
        type ipv4_addr
        flags interval
        elements = { ${BASTION_CIDR} }
    }
    set endpoint_cidr {
        type ipv4_addr
        flags interval
        elements = { ${ENDPOINT_CIDR} }
    }
    set monitoring_cidr {
        type ipv4_addr
        flags interval
        elements = { ${MONITORING_CIDR} }
    }
    set public_cidr {
        type ipv4_addr
        flags interval
        elements = { ${PUBLIC_CIDR} }
    }

    # -----------------------------------------------------------------
    # INPUT chain — default DROP
    # -----------------------------------------------------------------
    chain input {
        type filter hook input priority 0; policy drop;

        # Accept established + related
        ct state {established, related} accept
        ct state invalid drop

        # Loopback
        iif lo accept

        # ICMP (ping) rate-limited
        icmp type { echo-request } limit rate 5/second accept
        icmpv6 type { echo-request, nd-neighbor-solicit, nd-router-advert, nd-neighbor-advert } accept

        # SSH from bastion network only
        ip saddr @bastion_cidr tcp dport 22 accept comment "SSH from bastion"

        # HTTPS + HTTP (console + portal + nginx reverse proxy)
        ip saddr @public_cidr tcp dport { 80, 443 } accept comment "Web public"

        # Gateway mTLS — agent ingestion
        ip saddr @endpoint_cidr tcp dport 9443 accept comment "Gateway agent mTLS"

        # Prometheus scraping (metrics) from monitoring network only
        ip saddr @monitoring_cidr tcp dport { 9090, 9091, 9092, 9093, 9100, 9187 } accept comment "Metrics scraping"

        # Docker overlay (VXLAN) + docker DNS (only from docker0 bridge)
        iifname "docker0" accept
        iifname "br-*" accept

        # Log + drop everything else
        limit rate 5/minute log prefix "nft-drop: " level warn
        drop
    }

    # -----------------------------------------------------------------
    # FORWARD chain — docker managed
    # -----------------------------------------------------------------
    chain forward {
        type filter hook forward priority 0; policy accept;
        # Docker manages its own FORWARD rules under DOCKER-USER.
        # We don't interfere.
    }

    # -----------------------------------------------------------------
    # OUTPUT chain — default accept (egress allowed)
    # -----------------------------------------------------------------
    chain output {
        type filter hook output priority 0; policy accept;
        # No egress restriction at host level. Per-container egress
        # is managed by docker network internal:true (bkz. #140).
    }
}
NFT

echo "[firewall] Ruleset written to ${RULESET_FILE}"

if [[ "${DRY_RUN}" == true ]]; then
  echo "[firewall] DRY RUN — validating syntax..."
  nft -c -f "${RULESET_FILE}" && echo "[firewall] Syntax OK"
  exit 0
fi

echo "[firewall] Applying ruleset..."
nft -f "${RULESET_FILE}"
echo "[firewall] Ruleset active. Current input rules:"
nft list chain inet personel input

# Persist across reboot
if systemctl is-enabled nftables.service &>/dev/null; then
  cp "${RULESET_FILE}" /etc/nftables.conf
  systemctl reload nftables.service || systemctl restart nftables.service
  echo "[firewall] Ruleset persisted via nftables.service"
else
  echo "[firewall] WARN: nftables.service not enabled; rules will be lost on reboot."
  echo "[firewall] Run: systemctl enable --now nftables.service"
fi

echo "[firewall] Done."
