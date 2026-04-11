# Personel Platform — Kurulum Kılavuzu / Installation Guide

> **TR:** Bu kılavuz, Personel platformunu Linux sunucuya kurmak için adım adım talimatlar içerir. Hedef süre: 2 saat veya daha az.
>
> **EN:** This guide provides step-by-step instructions for installing the Personel platform on a Linux server. Target time: 2 hours or less.

---

## Gereksinimler / Requirements

| Özellik / Spec | Minimum | Önerilen / Recommended |
|---|---|---|
| OS | Ubuntu 22.04 LTS | Ubuntu 24.04 LTS |
| CPU | 8 core | 16 core |
| RAM | 32 GB | 64 GB |
| Disk | 200 GB SSD | 1 TB NVMe SSD |
| Docker | 25.0+ | 25.0+ |
| Kernel | 5.15+ | 6.x |

---

## Kontrol Listesi / Checklist

Kurulum öncesinde hazır olması gerekenler:

- [ ] TR: Sunucu IP adresi ve DNS kaydı oluşturuldu
- [ ] EN: Server IP address and DNS record created
- [ ] Tenant ID (UUID formatında / in UUID format): `_________________`
- [ ] DPO e-posta adresi: `_________________`
- [ ] SMTP sunucu bilgileri (bildirimler için / for notifications)
- [ ] GPG yedek şifresi oluşturuldu (güvenli yerde saklanmalı)
- [ ] Vault Shamir paylaşımı için 5 kişi hazır

---

## ADIM 1: Sunucu Hazırlığı / Server Preparation

**Tahmini süre / Estimated time: 15 dakika / minutes**

```bash
# TR: Sistemi güncelleyin
# EN: Update the system
sudo apt-get update && sudo apt-get upgrade -y

# TR: Docker kurun
# EN: Install Docker
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER
newgrp docker

# TR: Docker sürümünü doğrulayın
# EN: Verify Docker version
docker version   # Must be 25.0+
docker compose version   # Must be v2
```

### Kontrol Noktası 1 / Checkpoint 1
```bash
docker compose version   # Çıktı: Docker Compose version v2.x.x
```

---

## ADIM 2: Personel Dosyalarını İndirin / Download Personel Files

**Tahmini süre / Estimated time: 10 dakika / minutes**

```bash
# TR: Kurulum dosyalarını sunucuya kopyalayın
# EN: Copy installation files to the server
sudo mkdir -p /opt/personel
sudo chown $USER:$USER /opt/personel
# scp or rsync infra/ to /opt/personel/infra/

cd /opt/personel/infra
chmod +x install.sh uninstall.sh upgrade.sh backup.sh restore.sh
chmod +x scripts/*.sh tests/*.sh
```

---

## ADIM 3: Ortam Yapılandırması / Environment Configuration

**Tahmini süre / Estimated time: 20 dakika / minutes**

```bash
cd /opt/personel/infra/compose
cp .env.example .env
chmod 600 .env
nano .env   # TR: Tüm CHANGEME değerlerini doldurun / EN: Fill all CHANGEME values
```

### Kritik Değerler / Critical Values

| Değişken / Variable | Açıklama / Description |
|---|---|
| `PERSONEL_TENANT_ID` | Benzersiz UUID (örn. / e.g.: `uuidgen` ile oluşturun) |
| `PERSONEL_EXTERNAL_HOST` | Sunucunun FQDN'i (örn. / e.g.: `personel.sirket.com`) |
| `POSTGRES_PASSWORD` | Güçlü parola / Strong password |
| `BACKUP_GPG_PASSPHRASE` | Yedek şifreleme parolası — güvenli yerde saklayın |
| `DPO_EMAIL` | Veri Koruma Görevlisi e-postası |
| `KEYCLOAK_ADMIN_PASSWORD` | Keycloak yönetici parolası |

### Kontrol Noktası 2 / Checkpoint 2
```bash
grep "CHANGEME" .env | grep -v "^#"   # TR: Çıktı boş olmalı / EN: Should return no output
```

---

## ADIM 4: Ön Kontrol / Preflight Check

**Tahmini süre / Estimated time: 5 dakika / minutes**

```bash
sudo /opt/personel/infra/scripts/preflight-check.sh
```

TR: Tüm PASS göstergelerini görene kadar sorunları çözün.
EN: Resolve issues until all checks show PASS.

### Kontrol Noktası 3 / Checkpoint 3
```
[PASS] Ubuntu 24.04 — supported
[PASS] Kernel 6.x — OK
[PASS] Docker 25.x — OK
[PASS] RAM: 64 GB
[PASS] Disk available: 900 GB
```

---

## ADIM 5: PKI ve Sertifika Önyükleme / PKI Bootstrap

**Tahmini süre / Estimated time: 30 dakika / minutes**

> **TR:** Üretim ortamı için Kök CA töreni hava boşluklu bir makinede yapılmalıdır. Pilot kurulumlar için geliştirme modunu kullanabilirsiniz.
> **EN:** For production, Root CA ceremony must be performed on an air-gapped machine. For pilot installations, you can use dev mode.

**Pilot / Development (hava boşluğu yok / no air gap):**
```bash
sudo /opt/personel/infra/scripts/ca-bootstrap.sh \
  --tenant-id "$(grep PERSONEL_TENANT_ID /opt/personel/infra/compose/.env | cut -d= -f2)" \
  --tls-dir /etc/personel/tls \
  --non-interactive
```

**Üretim (air-gapped ceremony):**
Bakın / See: `docs/security/runbooks/pki-bootstrap.md`

### Kontrol Noktası 4 / Checkpoint 4
```bash
ls /etc/personel/tls/   # tenant_ca.crt, vault.crt, postgres.crt görünmeli
```

---

## ADIM 6: Tam Kurulum / Full Installation

**Tahmini süre / Estimated time: 30 dakika / minutes**

```bash
sudo /opt/personel/infra/install.sh
```

TR: Komut dosyası sizi yönlendirecektir. Vault Shamir paylaşımı için 5 kişi hazır olmalıdır (veya `--unattended` bayrağı ile atlanabilir, pilotlar için).

EN: The script will guide you. 5 Shamir share custodians must be available for the Vault ceremony (or can be skipped with `--unattended` for pilot installations).

---

## ADIM 7: İlk Kullanıcı Oluşturma / First User Creation

**Tahmini süre / Estimated time: 5 dakika / minutes**

```bash
sudo /opt/personel/infra/scripts/create-admin.sh
```

---

## ADIM 8: Doğrulama / Verification

```bash
# Tüm servisleri kontrol edin / Check all services
docker compose -f /opt/personel/infra/compose/docker-compose.yaml ps

# Duman testini çalıştırın / Run smoke tests
sudo /opt/personel/infra/tests/smoke.sh
```

### Son Kontrol Noktası / Final Checkpoint
```
[PASS] vault: healthy
[PASS] postgres: healthy
[PASS] clickhouse: healthy
[PASS] nats: healthy
[PASS] minio: healthy
[PASS] opensearch: healthy
[PASS] keycloak: healthy
[PASS] gateway: healthy
[PASS] api: healthy
[PASS] console: healthy
[PASS] portal: healthy
```

---

## Sonraki Adımlar / Next Steps

1. **TR:** DPO'nun VERBİS kaydını tamamlaması
   **EN:** DPO completes VERBİS registration

2. **TR:** Windows ajanlarını kurun (MSI ile)
   **EN:** Install Windows agents (via MSI)

3. **TR:** İlk politikayı oluşturun
   **EN:** Create first policy

4. **TR:** Nightly backup'ı doğrulayın (ilk gece sonrasında)
   **EN:** Verify nightly backup (after first night)

5. **TR:** Kurtarma tatbikatı yapın (ilk 30 gün içinde)
   **EN:** Perform recovery drill (within first 30 days)

---

## Sorun Giderme / Troubleshooting

Bakın / See: `runbooks/troubleshooting.md`

| Sorun / Issue | Çözüm / Solution |
|---|---|
| Vault sealed after restart | `scripts/vault-unseal.sh` |
| Service unhealthy | `docker compose logs SERVICE_NAME` |
| Disk full | Review ClickHouse TTL and MinIO lifecycle |
| Certificate expired | `scripts/rotate-secrets.sh --certs-only` |
