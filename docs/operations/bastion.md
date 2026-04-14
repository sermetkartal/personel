# Personel Platform — Bastion Host Kurulumu (Faz 13 #142)

> **Amaç**: Personel production hostlarına (vm3 + vm5) tek bir sertleştirilmiş
> giriş noktası. Operatör erişimi TOTP 2FA ile doğrulanır, tüm oturumlar
> kayıt altına alınır.

---

## 1. Mimari

```
  Operatör dizüstü (WireGuard)
           │
           ▼
  ┌──────────────────────┐
  │   BASTION HOST       │   Standalone VM
  │   - SSHD hardened    │   192.168.5.10 (örnek)
  │   - TOTP PAM         │
  │   - auditd + sudo    │
  │   - WireGuard server │
  └─────────┬────────────┘
            │  SSH cert-based jump
            ▼
  ┌──────────────────────┐
  │ Personel production  │
  │ - vm3 (192.168.5.44) │
  │ - vm5 (192.168.5.32) │
  └──────────────────────┘
```

---

## 2. Host gereksinimleri

| Özellik | Minimum | Önerilen |
|---|---|---|
| OS | Ubuntu 22.04 LTS | Ubuntu 24.04 LTS |
| CPU | 2 vCPU | 4 vCPU |
| RAM | 2 GB | 4 GB |
| Disk | 20 GB | 40 GB |
| Network | statik IP, ayrı subnet | — |

**YASAK**: Bastion host üzerinde uygulama, veritabanı, cache veya başka servis
çalıştırma. Sadece SSHD + auditd + wireguard + google-authenticator.

---

## 3. Kurulum

```bash
sudo /opt/personel/infra/scripts/bastion-bootstrap.sh \
  --admin-user=kartal \
  --target-hosts="192.168.5.44,192.168.5.32"
```

Script şunu yapar:

1. Paketleri kurar (openssh-server, libpam-google-authenticator, auditd,
   wireguard)
2. `/etc/ssh/sshd_config` sertleştirir (aşağıdaki ayarlar)
3. PAM'de TOTP zorunlu kılar
4. sudo için MFA zorunlu kılar
5. auditd kurallarını yükler (komut loglaması)
6. WireGuard server keygen + config
7. Log rotation setup

---

## 4. SSHD sertleştirme ayarları

`bastion-bootstrap.sh` şu `sshd_config` değerlerini atar:

```
Protocol 2
Port 22
AddressFamily inet
PermitRootLogin no
PasswordAuthentication no
PubkeyAuthentication yes
ChallengeResponseAuthentication yes
AuthenticationMethods publickey,keyboard-interactive
UsePAM yes
X11Forwarding no
AllowAgentForwarding no
AllowTcpForwarding yes      # SSH jump için gerekli
GatewayPorts no
ClientAliveInterval 300
ClientAliveCountMax 2
MaxAuthTries 3
MaxSessions 4
LoginGraceTime 20
HostKeyAlgorithms ssh-ed25519,rsa-sha2-512
KexAlgorithms curve25519-sha256,curve25519-sha256@libssh.org
Ciphers chacha20-poly1305@openssh.com,aes256-gcm@openssh.com
MACs hmac-sha2-512-etm@openssh.com,hmac-sha2-256-etm@openssh.com
Banner /etc/issue.net
LogLevel VERBOSE
AllowUsers kartal personel-ops
```

---

## 5. TOTP 2FA kaydı

Her operatör ilk girişte:

```bash
ssh kartal@bastion.example.com
# PAM bu adımda TOTP zorlar
google-authenticator -t -d -r3 -R30 -W -f
# QR kodu authenticator app'e tara (Google Auth, Aegis, 1Password, vs.)
# Recovery code'larını secret safe'e koy
```

`/home/kartal/.google_authenticator` dosyası 600 izne sahip olmalı.

---

## 6. SSH ProxyJump kullanımı

Operatör laptop'unda `~/.ssh/config`:

```
Host bastion
  HostName bastion.example.com
  User kartal
  Port 22
  IdentityFile ~/.ssh/id_ed25519
  ControlMaster auto
  ControlPath ~/.ssh/cm-%r@%h:%p
  ControlPersist 10m

Host vm3 vm5
  User kartal
  ProxyJump bastion
  IdentityFile ~/.ssh/id_ed25519

Host vm3
  HostName 192.168.5.44

Host vm5
  HostName 192.168.5.32
```

Kullanım:

```bash
ssh vm3
# → Bastion authentication (pubkey + TOTP)
# → Automatic jump to vm3
```

---

## 7. sudo MFA

`/etc/pam.d/sudo` üstünde:

```
auth required pam_google_authenticator.so nullok
```

Her `sudo` komutu TOTP ister. `nullok` parametresi henüz TOTP kaydetmemiş
kullanıcılar için geçiş süresi sağlar — **production'da kaldırın**.

---

## 8. Session recording

auditd kuralları `/etc/audit/rules.d/bastion.rules`:

```
# Shell komutlarını logla
-a always,exit -F arch=b64 -S execve -F euid>=1000 -F auid!=4294967295 -k operator_cmd
-a always,exit -F arch=b32 -S execve -F euid>=1000 -F auid!=4294967295 -k operator_cmd

# Privilege escalation
-a always,exit -F arch=b64 -S setuid,setgid -k privesc
-w /usr/bin/sudo -p x -k sudo_exec

# Sensitive files
-w /etc/passwd -p wa -k user_mod
-w /etc/shadow -p wa -k shadow_mod
-w /etc/ssh/sshd_config -p wa -k sshd_config
```

`aureport --start today` ile günlük özet.

SSH session snoop için alternatif: `auditd` yerine `tlog` (Red Hat) veya
`snoopy` (Ubuntu). Bunlar her komutun tam argüman listesini +
tty-recording sağlar.

---

## 9. Hiçbir secret saklama

Bastion host'a hiç kalıcı secret yazılmamalı:

- `/root/.ssh/authorized_keys` dışında hiçbir private key yok
- Target hostlara pubkey-based auth (bastion'dan target'a da TOFU pubkey)
- WireGuard server private key sadece `/etc/wireguard/wg0.conf` (600, root)
- Vault token'ı bastion üzerinde asla — operatör kendi laptop'unda tutar
- `~/.bash_history` dışındaki shell history dosyaları disable

---

## 10. Monitoring

Bastion → Prometheus:

- `node_exporter` CPU/mem/disk + login count
- `openssh-exporter` (3rd party) — başarısız giriş denemesi sayacı
- auditd events → Loki (promtail `/var/log/audit/audit.log`)

Alert örneği:

```yaml
- alert: BastionBruteForce
  expr: rate(sshd_failed_auth_total[5m]) > 2
  for: 2m
  labels: {severity: warning}
  annotations:
    description: "Bastion SSH brute force: {{ $value }} fail/s"
```

---

## 11. Felaket kurtarma

Bastion offline olursa:

1. Fiziksel/console erişim (VM hypervisor)
2. Emergency break-glass hesabı (`emergency-root`) — parola Vault break-glass
   bucket'ında; her kullanım audit log'da
3. Yeni bastion VM provision → WireGuard key rotation → eski config revoke
