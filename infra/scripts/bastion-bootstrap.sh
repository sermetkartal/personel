#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Bastion Host Bootstrap (Faz 13 #142)
# TR: Tek komutla sertleştirilmiş SSH bastion kurar.
# EN: One-command hardened SSH bastion setup.
#
# Usage:
#   sudo ./bastion-bootstrap.sh --admin-user=kartal
#                               --target-hosts="192.168.5.44,192.168.5.32"
#                               [--wireguard]
#
# Çalıştıktan sonra:
#   1. SSH açık (TOTP + pubkey zorunlu)
#   2. sudo MFA aktif
#   3. auditd komut loglaması açık
#   4. İsteğe bağlı: WireGuard server hazır
# =============================================================================
set -euo pipefail

ADMIN_USER=""
TARGET_HOSTS=""
ENABLE_WG=false

for arg in "$@"; do
  case "${arg}" in
    --admin-user=*)    ADMIN_USER="${arg#*=}" ;;
    --target-hosts=*)  TARGET_HOSTS="${arg#*=}" ;;
    --wireguard)       ENABLE_WG=true ;;
    -h|--help)
      sed -n '/^# Usage:/,/^$/p' "$0" | sed 's/^# //'
      exit 0
      ;;
  esac
done

[[ "${EUID}" -eq 0 ]] || { echo "Run as root"; exit 1; }
[[ -n "${ADMIN_USER}" ]] || { echo "--admin-user required"; exit 1; }

log() { echo -e "\033[0;32m[bastion]\033[0m $*"; }
warn() { echo -e "\033[1;33m[bastion WARN]\033[0m $*" >&2; }

# ---------------------------------------------------------------------------
# 1. Packages
# ---------------------------------------------------------------------------
log "Installing packages..."
DEBIAN_FRONTEND=noninteractive apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
  openssh-server \
  libpam-google-authenticator \
  auditd \
  audispd-plugins \
  fail2ban \
  unattended-upgrades \
  nftables \
  >/dev/null

if [[ "${ENABLE_WG}" == true ]]; then
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq wireguard wireguard-tools >/dev/null
fi

# ---------------------------------------------------------------------------
# 2. Admin user
# ---------------------------------------------------------------------------
if ! id "${ADMIN_USER}" &>/dev/null; then
  useradd -m -s /bin/bash -G sudo "${ADMIN_USER}"
  log "Created user ${ADMIN_USER}"
fi

mkdir -p "/home/${ADMIN_USER}/.ssh"
touch "/home/${ADMIN_USER}/.ssh/authorized_keys"
chmod 700 "/home/${ADMIN_USER}/.ssh"
chmod 600 "/home/${ADMIN_USER}/.ssh/authorized_keys"
chown -R "${ADMIN_USER}:${ADMIN_USER}" "/home/${ADMIN_USER}/.ssh"

# ---------------------------------------------------------------------------
# 3. SSHD hardening
# ---------------------------------------------------------------------------
log "Hardening sshd_config..."
cp /etc/ssh/sshd_config "/etc/ssh/sshd_config.bak.$(date +%Y%m%d-%H%M)"

cat > /etc/ssh/sshd_config.d/99-personel-bastion.conf <<EOF
# Personel bastion hardening — Faz 13 #142
Protocol 2
PermitRootLogin no
PasswordAuthentication no
PubkeyAuthentication yes
ChallengeResponseAuthentication yes
KbdInteractiveAuthentication yes
AuthenticationMethods publickey,keyboard-interactive
UsePAM yes

X11Forwarding no
AllowAgentForwarding no
AllowTcpForwarding yes
GatewayPorts no
PermitTunnel no

ClientAliveInterval 300
ClientAliveCountMax 2
MaxAuthTries 3
MaxSessions 4
LoginGraceTime 20

HostKeyAlgorithms ssh-ed25519,rsa-sha2-512,rsa-sha2-256
KexAlgorithms curve25519-sha256,curve25519-sha256@libssh.org,diffie-hellman-group16-sha512
Ciphers chacha20-poly1305@openssh.com,aes256-gcm@openssh.com,aes128-gcm@openssh.com
MACs hmac-sha2-512-etm@openssh.com,hmac-sha2-256-etm@openssh.com

Banner /etc/issue.net
LogLevel VERBOSE
AllowUsers ${ADMIN_USER}
EOF

cat > /etc/issue.net <<'EOF'
###############################################################################
#                    AUTHORIZED USE ONLY                                      #
# This system is property of Personel Platform and is for authorized use     #
# only. All activity is monitored and recorded. Unauthorized access is       #
# prohibited. KVKK m.4/m.5 uyarınca tüm oturumlar kayıt altındadır.           #
###############################################################################
EOF

# ---------------------------------------------------------------------------
# 4. PAM TOTP
# ---------------------------------------------------------------------------
log "Enabling PAM google-authenticator..."
if ! grep -q pam_google_authenticator /etc/pam.d/sshd; then
  echo "auth required pam_google_authenticator.so nullok" >> /etc/pam.d/sshd
fi
if ! grep -q pam_google_authenticator /etc/pam.d/sudo; then
  sed -i '/^auth/i auth required pam_google_authenticator.so nullok' /etc/pam.d/sudo
fi

# ---------------------------------------------------------------------------
# 5. auditd rules
# ---------------------------------------------------------------------------
log "Installing auditd rules..."
cat > /etc/audit/rules.d/bastion.rules <<'EOF'
# Personel bastion audit rules — Faz 13 #142
# Shell command execution
-a always,exit -F arch=b64 -S execve -F euid>=1000 -F auid!=4294967295 -k operator_cmd
-a always,exit -F arch=b32 -S execve -F euid>=1000 -F auid!=4294967295 -k operator_cmd

# Privilege escalation
-a always,exit -F arch=b64 -S setuid,setgid -k privesc
-w /usr/bin/sudo -p x -k sudo_exec
-w /bin/su -p x -k su_exec

# Sensitive files
-w /etc/passwd -p wa -k user_mod
-w /etc/shadow -p wa -k shadow_mod
-w /etc/gshadow -p wa -k group_mod
-w /etc/group -p wa -k group_mod
-w /etc/ssh/sshd_config -p wa -k sshd_config
-w /etc/ssh/sshd_config.d -p wa -k sshd_config
-w /etc/sudoers -p wa -k sudoers_mod
-w /etc/sudoers.d -p wa -k sudoers_mod

# Network config
-w /etc/network -p wa -k netconfig
-w /etc/nftables.conf -p wa -k firewall

# Module loading
-w /sbin/insmod -p x -k modules
-w /sbin/modprobe -p x -k modules
EOF

augenrules --load || warn "augenrules failed — run manually"
systemctl enable --now auditd

# ---------------------------------------------------------------------------
# 6. WireGuard (optional)
# ---------------------------------------------------------------------------
if [[ "${ENABLE_WG}" == true ]]; then
  log "Generating WireGuard server keys..."
  mkdir -p /etc/wireguard
  umask 077
  if [[ ! -f /etc/wireguard/privatekey ]]; then
    wg genkey | tee /etc/wireguard/privatekey | wg pubkey > /etc/wireguard/publickey
  fi
  SERVER_PRIV=$(cat /etc/wireguard/privatekey)

  cat > /etc/wireguard/wg0.conf <<EOF
[Interface]
Address = 10.100.0.1/24
ListenPort = 51820
PrivateKey = ${SERVER_PRIV}
SaveConfig = false

# Operator peers added manually:
# [Peer]
# PublicKey = <operator pubkey>
# AllowedIPs = 10.100.0.2/32
EOF
  chmod 600 /etc/wireguard/wg0.conf

  # Enable IPv4 forwarding
  sysctl -w net.ipv4.ip_forward=1
  echo "net.ipv4.ip_forward=1" > /etc/sysctl.d/99-wireguard.conf

  systemctl enable wg-quick@wg0
  log "WireGuard server ready on UDP 51820 — add peers manually"
fi

# ---------------------------------------------------------------------------
# 7. fail2ban
# ---------------------------------------------------------------------------
log "Configuring fail2ban..."
cat > /etc/fail2ban/jail.d/personel-bastion.conf <<'EOF'
[sshd]
enabled = true
port = 22
filter = sshd
logpath = /var/log/auth.log
maxretry = 3
findtime = 600
bantime = 3600
EOF

systemctl enable --now fail2ban

# ---------------------------------------------------------------------------
# 8. unattended-upgrades
# ---------------------------------------------------------------------------
dpkg-reconfigure -f noninteractive unattended-upgrades >/dev/null 2>&1 || true

# ---------------------------------------------------------------------------
# 9. Target host jump keys note
# ---------------------------------------------------------------------------
if [[ -n "${TARGET_HOSTS}" ]]; then
  log "Target hosts: ${TARGET_HOSTS}"
  log "Each target must have ${ADMIN_USER}'s pubkey in authorized_keys."
  log "Copy the following public key to each target:"
  if [[ -f "/home/${ADMIN_USER}/.ssh/id_ed25519.pub" ]]; then
    cat "/home/${ADMIN_USER}/.ssh/id_ed25519.pub"
  else
    warn "No ~/.ssh/id_ed25519.pub for ${ADMIN_USER} — generate with: su - ${ADMIN_USER} -c 'ssh-keygen -t ed25519'"
  fi
fi

# ---------------------------------------------------------------------------
# 10. Restart sshd
# ---------------------------------------------------------------------------
log "Restarting sshd..."
sshd -t || { warn "sshd config test failed — not restarting"; exit 1; }
systemctl restart ssh

log "Bastion bootstrap complete."
log ""
log "NEXT STEPS:"
log "  1. Login as ${ADMIN_USER} and run: google-authenticator -t -d -r3 -R30 -W -f"
log "  2. Add target hosts' pubkey: ssh-copy-id -i ~/.ssh/id_ed25519.pub ${ADMIN_USER}@<target>"
log "  3. Configure ProxyJump in operator workstations (~/.ssh/config)"
log "  4. Verify auditd: sudo ausearch -k operator_cmd -ts recent"
