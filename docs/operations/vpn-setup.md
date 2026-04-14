# Personel Platform — WireGuard VPN Kurulumu (Faz 13 #143)

> **Amaç**: Operatör ve HR kullanıcılarının production Personel'ine sadece
> onaylı VPN üzerinden erişimini sağlamak. Çift faktörlü giriş (client cert +
> WireGuard private key).

---

## 1. Tasarım

- **Server**: Bastion host üzerinde WireGuard (UDP 51820)
- **Network**: 10.100.0.0/24 (peer subnet)
- **Routing mode**: Split-tunnel (sadece `10.0.0.0/8` Personel subnet'leri
  VPN üzerinden; internet doğrudan)
- **Auth**: WireGuard public key + TOTP (ikinci katman PAM üzerinden bastion
  SSH login'de; VPN kendisi preshared-key ile)
- **Kill-switch**: Her operatör laptop'unda zorunlu

---

## 2. Server tarafı (bastion üzerinde)

`bastion-bootstrap.sh --wireguard` flag'i ile otomatik kurulur. Manuel
kurulum:

```bash
sudo apt-get install -y wireguard wireguard-tools

# Server key pair
umask 077
wg genkey | sudo tee /etc/wireguard/privatekey | wg pubkey | sudo tee /etc/wireguard/publickey

sudo tee /etc/wireguard/wg0.conf <<EOF
[Interface]
Address = 10.100.0.1/24
ListenPort = 51820
PrivateKey = $(sudo cat /etc/wireguard/privatekey)
SaveConfig = false

PostUp   = nft add rule inet personel input udp dport 51820 accept
PostDown = nft delete rule inet personel input udp dport 51820 accept
EOF

sudo sysctl -w net.ipv4.ip_forward=1
echo 'net.ipv4.ip_forward=1' | sudo tee /etc/sysctl.d/99-wireguard.conf

sudo systemctl enable --now wg-quick@wg0
sudo wg show
```

---

## 3. Peer (operatör) ekleme

Her operatör için ayrı bir peer. Peer ekleme tek yönlü:
**operatör kendi anahtarını üretir, pubkey'i admin'e verir**. Admin pubkey'i
bastion üzerinde wg0'a ekler.

Operatör tarafı (laptop):

```bash
umask 077
wg genkey | tee ~/wg-priv | wg pubkey > ~/wg-pub
cat ~/wg-pub   # → admin'e yollanır (pastebin değil; GPG veya Signal)
```

Admin tarafı (bastion):

```bash
OPERATOR_PUBKEY="ABCD1234..."
OPERATOR_IP="10.100.0.10"
OPERATOR_NAME="kartal"

sudo wg set wg0 peer "${OPERATOR_PUBKEY}" allowed-ips "${OPERATOR_IP}/32"
sudo wg-quick save wg0 || true

# Client config'i üret (operator'e göndermek için)
SERVER_PUB=$(sudo cat /etc/wireguard/publickey)
cat > "/tmp/wg-${OPERATOR_NAME}.conf" <<EOF
[Interface]
PrivateKey = <fill in operator side>
Address = ${OPERATOR_IP}/32
DNS = 1.1.1.1

[Peer]
PublicKey = ${SERVER_PUB}
Endpoint = bastion.example.com:51820
AllowedIPs = 10.0.0.0/8, 192.168.5.0/24
PersistentKeepalive = 25
EOF
```

Bu config'i GPG ile şifrele, operatör'e gönder. Operatör laptop'unda:

```bash
sudo mv wg-kartal.conf /etc/wireguard/wg0.conf
# PrivateKey satırını ~/wg-priv içeriği ile güncelle
sudo wg-quick up wg0
sudo wg show
```

---

## 4. Split-tunnel önerisi

`AllowedIPs = 10.0.0.0/8, 192.168.5.0/24` yalnızca Personel subnet'lerini
VPN üzerinden gönderir. Operatörün internet trafiği VPN'e girmez.

Full-tunnel istiyorsanız `AllowedIPs = 0.0.0.0/0, ::/0` — ama bu durumda
operatör laptop'u bastion üzerinden NAT edilir ve bastion'a ek yük gelir.

**Öneri**: Split-tunnel. Full-tunnel sadece zero-trust ortamlarda
değerlendirilmeli.

---

## 5. Kill-switch

Operatör laptop'unda zorunlu: VPN düşerse trafik devam etmesin.

**Linux** (systemd-resolved + nftables):

```bash
sudo tee /etc/NetworkManager/dispatcher.d/99-wg-killswitch <<'EOF'
#!/bin/sh
case "$2" in
  down|vpn-down)
    nft add rule inet filter output oifname != "wg0" drop
    ;;
  up|vpn-up)
    nft delete rule inet filter output oifname != "wg0" drop 2>/dev/null || true
    ;;
esac
EOF
sudo chmod +x /etc/NetworkManager/dispatcher.d/99-wg-killswitch
```

**macOS**: Pf.conf rule set — ayrı setup gerekli, burada kapsam dışı.

**Windows**: WireGuard native client `Block untunneled traffic` seçeneği.

---

## 6. 2FA katmanlama

WireGuard tek başına sadece public-key auth yapar. İkinci faktör için:

1. WireGuard tunnel → bastion'a bağlanır
2. Bastion üzerinden SSH yaparken TOTP (google-authenticator) zorlanır
3. Production host'a jump ederken tekrar pubkey (bu kez cert-based olabilir)

Üç-kat giriş: **WG key + TOTP + SSH cert**.

---

## 7. Rotation

- **WG keys**: 90 günde bir rotate (cron + operatör re-onboarding)
- **TOTP**: Device değişimi olmadan rotate edilmez; cihaz kaybında DPO onayı
  ile recovery code
- **SSH cert**: CA signer 30 günde bir rotate (Vault PKI cert TTL)

---

## 8. Audit + monitoring

`/var/log/wg.log`:

```bash
sudo tee /etc/systemd/system/wg-audit.service <<'EOF'
[Unit]
Description=WireGuard connection audit
After=wg-quick@wg0.service

[Service]
Type=simple
ExecStart=/bin/sh -c 'while true; do wg show | logger -t wg-audit; sleep 60; done'
EOF
sudo systemctl enable --now wg-audit.service
```

Prometheus alert:

```yaml
- alert: WireGuardPeerDropped
  expr: changes(wireguard_peer_last_handshake_seconds[10m]) == 0
  for: 15m
  labels: {severity: warning}
  annotations:
    description: "WG peer {{ $labels.peer }} has not handshaked in 15 minutes"
```

`prometheus-wireguard-exporter` 3rd party exporter gerekli.

---

## 9. Felaket

- Bastion çökerse VPN erişimi kaybolur → emergency console access
  (hypervisor)
- VPN anahtarları kaybolursa → WG peer entry'yi sil, yeni key üret
- Operatör laptop'u çalınırsa → pubkey'ini `wg0.conf`'tan derhal kaldır +
  audit log'da son 90 günlük erişimi incele
