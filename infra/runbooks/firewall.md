# Personel Platform — Host Firewall Runbook (Faz 13 #141)

> **Kapsam**: Ubuntu 22/24 veya RHEL 9 host'larda nftables ruleset'ini
> uygulamak, doğrulamak, değiştirmek ve geri almak.

---

## 1. Politika özeti

- **Default**: `input` chain DROP, `output` chain ACCEPT, `forward` docker yönetiyor
- **SSH (22)**: sadece bastion CIDR'den
- **HTTPS/HTTP (80, 443)**: herkesten — nginx WAF filtreli
- **Gateway mTLS (9443)**: sadece endpoint subnet'inden
- **Metrics (9090/9091/9092/9093/9100/9187)**: sadece monitoring CIDR
- **ICMP ping**: rate-limited 5/s
- **Geri kalan her şey**: drop + 5/m rate-limited log

---

## 2. İlk kurulum

```bash
# Paket kurulumu
sudo apt-get install -y nftables

# Ruleset uygula (üretim CIDR'leri ile)
sudo /opt/personel/infra/scripts/firewall-apply.sh \
  --bastion-cidr=10.10.0.0/24 \
  --endpoint-cidr=10.20.0.0/16 \
  --monitoring-cidr=10.10.1.0/24 \
  --public-cidr=0.0.0.0/0

# Systemd ile kalıcı
sudo systemctl enable --now nftables.service
```

**Dikkat**: Eğer SSH bağlantınız bastion dışında bir yerden geliyorsa,
kuralı uygulamadan **önce** bastion üzerinden ikinci bir oturum açın — aksi
halde kendinizi kilitlersiniz.

---

## 3. Dry run

```bash
sudo /opt/personel/infra/scripts/firewall-apply.sh --dry-run
# → "Syntax OK" görene kadar apply etmeyin
```

---

## 4. Doğrulama

```bash
# Aktif kuralları listele
sudo nft list ruleset

# Sadece input chain
sudo nft list chain inet personel input

# Canlı trafik izleme
sudo nft monitor trace

# Geçmiş drop'ları görüntüle (systemd journal)
sudo journalctl -k | grep 'nft-drop'
```

---

## 5. Test senaryoları

```bash
# Bastion dışından SSH denemesi — başarısız olmalı
ssh -o ConnectTimeout=5 kartal@192.168.5.44   # FAIL beklenir

# Bastion üzerinden SSH — başarılı olmalı
ssh -o ConnectTimeout=5 -J bastion.example.com kartal@192.168.5.44   # PASS

# Gateway mTLS (endpoint CIDR'den)
curl -sk https://192.168.5.44:9443/healthz   # PASS beklenir

# Metrics endpoint dışarıdan — başarısız olmalı
curl -sk http://192.168.5.44:9090/metrics   # FAIL beklenir
```

---

## 6. Geri alma

Acil durumlarda kuralları tamamen temizle:

```bash
sudo nft flush ruleset
# → SSH açık kalacak
```

**Dikkat**: Bu işlem tüm koruma kurallarını siler. Sadece troubleshooting
için kullanılmalı, derhal yeniden uygulanmalı.

Eski versiyona dön:

```bash
sudo cp /etc/nftables-personel.conf.bak /etc/nftables.conf
sudo systemctl reload nftables.service
```

---

## 7. CIDR değişiklikleri

Yeni endpoint subnet'i ekleme:

```bash
sudo nft add element inet personel endpoint_cidr '{ 10.30.0.0/16 }'
```

Kalıcı hale getirmek için `firewall-apply.sh` flaglarını güncelleyip yeniden
çalıştırın.

---

## 8. Bilinen kenar durumlar

- **Docker bridge interface değişimi**: Docker yeniden başlatılınca `br-*`
  interface'leri yeniden oluşabilir. Kural `iifname "br-*"` wildcard olduğu
  için otomatik eşleşir.
- **Podman birlikte çalışma**: Podman kullanılıyorsa `DOCKER-USER` chain'i
  yoktur. Manuel FORWARD chain kuralı eklenmelidir.
- **IPv6**: Mevcut ruleset sadece IPv4 set'leri kullanır. IPv6 için ayrı
  `ipv6_addr` set'leri eklenmelidir.

---

## 9. Monitoring entegrasyonu

Drop sayısı için Prometheus alert:

```yaml
- alert: NftablesDropSpike
  expr: rate(node_netstat_Nft_drops[5m]) > 10
  for: 5m
  labels:
    severity: warning
  annotations:
    description: "Firewall dropping {{ $value }} pkts/s — possible attack or misconfigured client"
```

node_exporter `--collector.nftables` flag'i ile aktif olmalıdır.
