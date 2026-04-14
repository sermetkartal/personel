# Personel — Üretim Kurulum Kılavuzu

> Dil: Türkçe. Hedef okuyucu: müşteri sistem yöneticisi, Personel deployment mühendisi. Hedef süre: ilk tam kurulum **2 saat** (Faz 1 MVP).
>
> Bu doküman, sıfırdan yeni bir on-prem Personel kurulumunu son uca kadar götürür. Her adım idempotent'tir; hata durumunda baştan çalıştırılabilir.
>
> İlgili dokümanlar:
> - `infra/runbooks/install.md` — kısa operatör runbook'u
> - `docs/operations/troubleshooting.md` — hata giderme matrisi
> - `docs/security/runbooks/pki-bootstrap.md` — PKI detayları
> - `docs/security/runbooks/vault-setup.md` — Vault seremonisi
> - `docs/compliance/kvkk-framework.md` — KVKK çerçevesi
> - `docs/operations/vault-prod-migration.md` — prod-grade Vault üretim yapılandırması

## İçindekiler

1. [Ön Koşullar](#1-ön-koşullar)
2. [Pre-Flight Kontrol](#2-pre-flight-kontrol)
3. [Adım Adım Kurulum](#3-adım-adım-kurulum)
4. [Vault Unseal Seremonisi (Shamir 3/5)](#4-vault-unseal-seremonisi-shamir-35)
5. [Keycloak Realm İçe Aktarımı](#5-keycloak-realm-içe-aktarımı)
6. [PKI Bootstrap (Server Cert + Root CA + Agent Role)](#6-pki-bootstrap)
7. [İlk Yönetici Kullanıcı Oluşturma](#7-ilk-yönetici-kullanıcı-oluşturma)
8. [İlk Uç Nokta Kaydı (Enrollment)](#8-ilk-uç-nokta-kaydı)
9. [Kurulum Sonrası Doğrulama](#9-kurulum-sonrası-doğrulama)
10. [Sorun Giderme Hızlı Bakış](#10-sorun-giderme-hızlı-bakış)

---

## 1. Ön Koşullar

### 1.1 Donanım (500 uç nokta için minimum)

| Kaynak | Minimum | Önerilen | Notlar |
|---|---|---|---|
| CPU | 8 vCPU | 16 vCPU | ClickHouse + Go servisleri; hyperthreading açık |
| RAM | 32 GB | 64 GB | ClickHouse 16 GB, OpenSearch 8 GB, Postgres 4 GB, rest 4 GB |
| Disk (sistem) | 100 GB SSD | 200 GB SSD | OS + Docker image cache |
| Disk (data) | 1 TB SSD | 2 TB NVMe | ClickHouse + MinIO + Postgres WAL |
| Ağ | 1 Gbps | 10 Gbps | Ajanlar aynı LAN veya VPN üstünden |

> **Ölçeklendirme notu**: 10.000 uç nokta hedefi için Faz 5 cluster yapılandırması gereklidir (`docs/operations/clickhouse-cluster.md`, `docs/operations/postgres-replication.md`).

### 1.2 İşletim Sistemi

- Ubuntu 22.04 LTS veya 24.04 LTS (destekleniyor)
- Kernel ≥ 5.15 (`uname -r`)
- SELinux / AppArmor enforcing durumda çalışır (AppArmor profilleri `infra/compose/dlp/` altında)
- Sistem saati: chrony veya systemd-timesyncd ile NTP senkronize (drift < 1s)

### 1.3 Ağ

| Port | Yön | Kaynak | Amaç |
|---|---|---|---|
| 443/tcp | ingress | iç LAN / VPN | Konsol + Portal (Caddy) |
| 9443/tcp | ingress | ajanlar | Gateway mTLS |
| 8443/tcp | ingress | yöneticiler | Admin API (opsiyonel doğrudan erişim) |
| 3478-3479/udp | ingress | ajanlar | LiveKit STUN |
| 50000-60000/udp | ingress | ajanlar | LiveKit TURN media |
| 22/tcp | ingress | yalnız bastion | SSH (asla public değil) |
| 53/udp, 123/udp | egress | — | DNS + NTP |
| 443/tcp | egress | — | Update pull (release CDN) — opsiyonel |

> **KVKK notu**: Tüm trafik iç ağda kalmalıdır. Gateway'in internete açık olması yalnızca saha (field) cihazları için gerekli olup bu durumda Cloudflare/WAF önerilir (`docs/operations/registry-policies.md`).

### 1.4 Yazılım

- Docker CE 25+ (`docker --version`)
- Docker Compose v2.20+ (`docker compose version`)
- systemd
- OpenSSL 3+ (PKI seremonisi için)
- `jq`, `curl`, `git` (installer bağımlılıkları)

---

## 2. Pre-Flight Kontrol

`install.sh` çalıştırılmadan önce `infra/scripts/preflight-check.sh` koşulur. Bu script şunları doğrular:

- [ ] Docker daemon çalışıyor ve kullanıcı `docker` grubunda
- [ ] Minimum disk alanı (200 GB sistem, 1 TB data partition)
- [ ] Minimum RAM (32 GB)
- [ ] `/etc/sysctl.d/99-personel.conf` ayarlı (`vm.max_map_count = 262144` OpenSearch için)
- [ ] `ulimit -n` ≥ 65536
- [ ] Saat senkronize (NTP drift < 1s)
- [ ] `/var/lib/personel` mevcut ve yazılabilir
- [ ] Gerekli portlar (bkz. §1.3) boş
- [ ] `.env` dosyası dolu — CHANGEME değerleri yok
- [ ] TLS materyali hazır veya PKI bootstrap seçeneği işaretli

```bash
cd /opt/personel
sudo infra/scripts/preflight-check.sh
```

Hata çıktısı varsa kurulum BAŞLATMAYIN. Hataları `docs/operations/troubleshooting.md` §Kurulum Ön Koşulları bölümünden giderin.

---

## 3. Adım Adım Kurulum

### 3.1 Kaynak Kodu Alın

```bash
sudo mkdir -p /opt/personel
sudo chown $(whoami):$(whoami) /opt/personel
cd /opt/personel
git clone https://github.com/<musteri>/personel.git .
git checkout v1.0.0   # pilot etiket
```

### 3.2 Ortam Dosyasını Hazırlayın

```bash
cd /opt/personel/infra/compose
cp .env.example .env
$EDITOR .env
```

Doldurulacak zorunlu alanlar:

```bash
# Tenant
PERSONEL_TENANT_ID=<UUID v4>
PERSONEL_TENANT_NAME="Müşteri A.Ş."
PERSONEL_DEPLOYMENT_ID=<UUID v4>
PERSONEL_ENVIRONMENT=production

# Hostname'ler (reverse proxy ve ajan bağlantıları için)
PERSONEL_CONSOLE_HOST=personel.musteri.local
PERSONEL_PORTAL_HOST=portal.personel.musteri.local
PERSONEL_API_HOST=api.personel.musteri.local
PERSONEL_GATEWAY_HOST=gw.personel.musteri.local
PERSONEL_LIVEKIT_HOST=lv.personel.musteri.local

# Postgres
POSTGRES_PASSWORD=<32 karakter rastgele>
POSTGRES_APP_PASSWORD=<32 karakter rastgele>

# ClickHouse
CLICKHOUSE_ADMIN_PASSWORD=<32 karakter rastgele>
CLICKHOUSE_APP_PASSWORD=<32 karakter rastgele>

# MinIO
MINIO_ROOT_USER=personel_minio
MINIO_ROOT_PASSWORD=<32 karakter rastgele>

# Keycloak
KEYCLOAK_ADMIN=personel_admin
KEYCLOAK_ADMIN_PASSWORD=<32 karakter rastgele>

# Vault
VAULT_ADDR=https://vault.personel.musteri.local:8200
```

> **Güvenlik notu**: `.env` dosyası hiçbir zaman git'e commit edilmemelidir. Kurulum sonrası `chmod 600 .env`. Şifreler `infra/scripts/password-gen.sh` ile üretilebilir.

### 3.3 Installer'ı Çalıştırın

```bash
cd /opt/personel
sudo infra/install.sh
```

`install.sh` sırasıyla şunları yapar (her adım idempotent):

1. **Pre-flight check** — §2'deki kontrollerin tekrarı
2. **Dizin hazırlığı** — `/var/lib/personel/{postgres,clickhouse,minio,vault,nats,opensearch,keycloak}`
3. **Docker image pull** — 18 servisin imajları
4. **PKI bootstrap** — Vault initialize + self-signed root CA (§6 detaylı)
5. **Infra stack başlatma** — Vault, Postgres, ClickHouse, MinIO, NATS, OpenSearch, Keycloak
6. **Postgres migration** — `apps/api/internal/postgres/migrations/` altındaki 28+ dosya
7. **ClickHouse schema** — `apps/gateway/internal/clickhouse/schemas.go` üzerinden
8. **Keycloak realm import** — `infra/compose/keycloak/realm-personel.json` (§5)
9. **Uygulama stack başlatma** — API, Gateway, Enricher, Console, Portal
10. **Smoke test** — `/healthz` endpoint'leri + NATS stream list + Vault status
11. **Systemd unit'leri** — `personel-*.service` + `personel-backup.timer` kurulumu

Başarılı kurulum log çıktısı:

```
[install] ✓ Pre-flight check passed
[install] ✓ Vault initialized (keys saved to /etc/personel/vault-init.json)
[install] ✓ Postgres migrations applied (28 files)
[install] ✓ ClickHouse schemas created (5 tables, 3 materialized views)
[install] ✓ Keycloak realm imported (personel, 7 roles, 1 client)
[install] ✓ All 18 services healthy
[install] ✓ Smoke test passed
[install] Personel is ready at https://personel.musteri.local
```

Süre: 10-30 dakika (imaj pull süresine bağlı).

---

## 4. Vault Unseal Seremonisi (Shamir 3/5)

`install.sh` içinde Vault otomatik initialize edilir; ancak üretimde **Shamir 3-of-5** anahtar dağıtımı manuel yapılmalıdır. Dev kurulumda auto-unseal kullanılır.

### 4.1 Manual Üretim Seremonisi

Bu seremoniye **en az 5 yetkili kişi fiziksel olarak katılmalıdır**. Her kişi kendi anahtar parçasını alır.

```bash
cd /opt/personel
sudo docker exec -it personel-vault sh
# Vault içinde:
vault operator init \
  -key-shares=5 \
  -key-threshold=3 \
  -format=json > /tmp/vault-init.json
cat /tmp/vault-init.json
```

Çıktı 5 `unseal_key_shares` + 1 `root_token` içerir. **Hemen şunu yapın**:

1. Her anahtar parçasını farklı kağıda yazın, her birini farklı kişiye verin
2. Her kişi parçasını bir tamper-evident zarfta kilitli kasaya koyar
3. Kağıt kopyaları üretir, dijital kopya SİLİNİR:
   ```bash
   sudo shred -u /tmp/vault-init.json
   ```
4. Root token'ı da ayrıca yazılır; her zamanki kullanım için **kullanılmaz** (yalnızca acil durum). Bunun yerine AppRole/OIDC auth kullanılır.

### 4.2 Unseal

Her Vault restart sonrası unseal gereklidir. Üç farklı yetkili, üç farklı parçayı sırayla girer:

```bash
sudo docker exec -it personel-vault vault operator unseal
# Parça 1 girilir
sudo docker exec -it personel-vault vault operator unseal
# Parça 2 girilir (farklı kişi)
sudo docker exec -it personel-vault vault operator unseal
# Parça 3 girilir (farklı kişi)
```

Vault artık "unsealed" durumdadır (`sealed=false`).

> **Auto-unseal alternatifi**: Prod ortamda manuel unseal operasyonel yüktür. HSM veya cloud KMS (AWS KMS, Azure Key Vault) ile auto-unseal önerilir. Kararlar: `docs/adr/0005-vault-auto-unseal.md` (TBD — Faz 3).

### 4.3 Sealed Dosya ile Dev Auto-Unseal (SADECE dev)

`infra/scripts/vault-unseal.sh` dev ortamda `/etc/personel/vault-unseal.key` dosyasından ilk parçayı okur. Bu dosya **PROD'DA ASLA BULUNMAMALIDIR**.

---

## 5. Keycloak Realm İçe Aktarımı

### 5.1 Otomatik Import

`install.sh` Keycloak başlatılır başlatılmaz realm'i JSON dosyasından otomatik import eder:

```bash
docker exec personel-keycloak /opt/keycloak/bin/kc.sh import \
  --file /opt/keycloak/data/import/realm-personel.json \
  --override true
```

Realm tanımı `infra/compose/keycloak/realm-personel.json` içinde. İçerir:

- **Realm adı**: `personel`
- **7 rol**: admin, dpo, hr, manager, investigator, auditor, employee
- **1 client**: `personel-api` (confidential, OIDC, authorization enabled)
- **1 client**: `personel-console` (public, PKCE)
- **1 client**: `personel-portal` (public, PKCE)
- **Protocol mapper**: `tenant_id` (user attribute → JWT claim)
- **Password policy**: 12+ karakter, complexity, 90-day expire

### 5.2 Realm Özelleştirme (opsiyonel)

Müşteri kurumsal SSO kullanıyorsa (ADFS, Azure AD):

```
Admin Console (https://auth.personel.musteri.local) →
  realm personel → Identity Providers → Add provider → SAML v2.0
```

Detaylar: `docs/operations/keycloak-saml-setup.md` (opsiyonel eklenti).

---

## 6. PKI Bootstrap

Vault PKI engine üç rol oluşturur:

1. **`pki/root`** — self-signed Root CA (tenant_ca.crt)
2. **`pki/roles/agent-cert`** — uç nokta client sertifikaları (TTL 90 gün, `client_flag=true`)
3. **`pki/roles/server-cert`** — gateway + API server sertifikaları (TTL 365 gün, hem `server_flag` hem `client_flag` Phase 1 basitliği için)

### 6.1 Otomatik Bootstrap

`install.sh` içinden çağrılan `infra/scripts/ca-bootstrap.sh`:

```bash
# PKI engine enable
vault secrets enable -path=pki pki
vault secrets tune -max-lease-ttl=87600h pki  # 10 yıl

# Root CA oluştur
vault write -format=json pki/root/generate/internal \
  common_name="Personel Root CA - ${TENANT_NAME}" \
  ttl=87600h > /tmp/root-ca.json

# Agent cert rolü
vault write pki/roles/agent-cert \
  allow_any_name=true \
  client_flag=true \
  server_flag=false \
  max_ttl=2160h   # 90 gün

# Server cert rolü
vault write pki/roles/server-cert \
  allow_any_name=true \
  client_flag=true \
  server_flag=true \
  allowed_domains="*.personel.musteri.local" \
  allow_subdomains=true \
  max_ttl=8760h   # 365 gün

# AppRole auth (gateway + API)
vault auth enable approle
vault write auth/approle/role/gateway-service token_policies=gateway-pki
vault write auth/approle/role/api-service token_policies=api-pki

# Agent enrollment AppRole
vault write auth/approle/role/agent-enrollment token_policies=agent-enroll
```

### 6.2 Server Sertifikası Çekme

Gateway ve API, başlangıçta kendi sertifikalarını Vault'tan ister:

```bash
vault write -format=json pki/issue/server-cert \
  common_name=gw.personel.musteri.local \
  alt_names=gateway.internal,gw.personel.musteri.local \
  ttl=8760h > /etc/personel/tls/gateway.json

jq -r .data.certificate /etc/personel/tls/gateway.json > /etc/personel/tls/gateway.crt
jq -r .data.private_key /etc/personel/tls/gateway.json > /etc/personel/tls/gateway.key
jq -r .data.issuing_ca /etc/personel/tls/gateway.json > /etc/personel/tls/tenant_ca.crt
```

### 6.3 Cert Rotasyonu

Gateway ve API cert'leri 30 gün kala Vault'tan otomatik yenilenir (`infra/systemd/personel-cert-rotate.timer`). Detay: `docs/operations/all-services-tls-migration.md`.

---

## 7. İlk Yönetici Kullanıcı Oluşturma

Keycloak import realm'inde varsayılan kullanıcı **yoktur**. İlk admin kullanıcıyı CLI ile oluşturun:

```bash
docker exec personel-keycloak /opt/keycloak/bin/kcadm.sh config credentials \
  --server http://localhost:8080 \
  --realm master \
  --user $KEYCLOAK_ADMIN \
  --password $KEYCLOAK_ADMIN_PASSWORD

# Yeni kullanıcı
docker exec personel-keycloak /opt/keycloak/bin/kcadm.sh create users \
  -r personel \
  -s username=admin \
  -s email=admin@musteri.local \
  -s enabled=true \
  -s emailVerified=true \
  -s 'attributes.tenant_id=["'$PERSONEL_TENANT_ID'"]'

# Şifre belirle
docker exec personel-keycloak /opt/keycloak/bin/kcadm.sh set-password \
  -r personel \
  --username admin \
  --new-password 'ChangeThisOnFirstLogin123!' \
  --temporary

# Admin rolü ver
docker exec personel-keycloak /opt/keycloak/bin/kcadm.sh add-roles \
  -r personel \
  --uusername admin \
  --rolename admin
```

Kullanıcı ilk girişinde şifresini değiştirmeye zorlanır.

> **KVKK notu**: Admin kullanıcı KVKK m.12 kapsamında "veri sorumlusu temsilcisi" olarak atanmalıdır. DPO rolü ayrı bir kullanıcıda olmalı (kriptografik tarafsızlık için).

---

## 8. İlk Uç Nokta Kaydı

### 8.1 Enroll Token Alın

Admin konsoluna giriş yapın (`https://personel.musteri.local`) → **Cihazlar** → **Yeni Cihaz Ekle** → kopya token'ı al.

Veya API ile:

```bash
TOKEN=$(curl -sk -X POST https://api.personel.musteri.local/v1/endpoints/enroll \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"'$PERSONEL_TENANT_ID'","asset_tag":"LAPTOP-001"}' \
  | jq -r .token)
echo $TOKEN
# Opaque base64url: eyJlbnJvbGxfdXJsIjoi...
```

### 8.2 Windows Ajanı Kurun

Müşteri laptop'unda (admin olarak çalıştır):

```powershell
# MSI indir
Invoke-WebRequest -Uri https://personel.musteri.local/downloads/personel-agent.msi `
  -OutFile C:\Temp\personel-agent.msi

# Kur
msiexec /i C:\Temp\personel-agent.msi /qn /log C:\Temp\install.log

# Enroll
& "C:\Program Files (x86)\Personel\Agent\enroll.exe" --token "$TOKEN"

# Servisi başlat
Start-Service PersonelAgent
```

Enroll adımı şunları yapar:

1. Token'ı decode et (enroll_url + role_id + secret_id)
2. Ed25519 anahtar çifti üret
3. CSR oluştur + Vault AppRole üzerinden imzalat
4. Sertifika + private key'i DPAPI LocalMachine scope'ta seal et
5. `C:\ProgramData\Personel\agent\config.toml` yaz
6. Root CA'yı `C:\ProgramData\Personel\agent\root_ca.pem` olarak kaydet

### 8.3 İlk Event'i Doğrulayın

Sunucuda:

```bash
docker exec personel-nats wget -qO- "http://127.0.0.1:8222/jsz?streams=1" \
  | jq '.account_details[0].stream_detail[] | select(.name=="events_raw") | .state.messages'
# → > 0 olmalı
```

ClickHouse'ta:

```bash
docker exec personel-clickhouse clickhouse-client --query \
  "SELECT count() FROM personel.events WHERE endpoint_id='<endpoint-id>'"
```

Konsolda: **Cihazlar** → ilgili cihaz → **Son Aktivite** → aktif heartbeat görünmeli.

---

## 9. Kurulum Sonrası Doğrulama

Aşağıdaki checklist'i tamamlayın:

- [ ] `https://personel.musteri.local` açılıyor, admin giriş yapıyor
- [ ] `https://portal.personel.musteri.local` açılıyor
- [ ] Tüm 18 servis `docker compose ps` → `healthy`
- [ ] `curl https://api.personel.musteri.local/healthz` → `{"status":"ok"}`
- [ ] Vault `sealed=false`
- [ ] Postgres `SELECT version()` çalışıyor
- [ ] ClickHouse `SELECT version()` çalışıyor
- [ ] MinIO konsolu erişilebilir (`https://minio.personel.musteri.local:9001`)
- [ ] Test ajanı enroll oldu ve event akıyor
- [ ] İlk event ClickHouse'ta göründü (p95 < 5s hedef)
- [ ] Audit log'a ilk kayıt düştü (login olayı)
- [ ] Backup cron timer aktif (`systemctl list-timers personel-backup.timer`)
- [ ] Prometheus scrape target'ları UP
- [ ] Grafana dashboard'ları veri gösteriyor

Faz 1 Exit Criteria #1-6: `apps/qa/ci/thresholds.yaml`.

---

## 10. Sorun Giderme Hızlı Bakış

İlk kurulumda en sık karşılaşılan 10 sorun:

| # | Belirti | Olası Sebep | Çözüm |
|---|---|---|---|
| 1 | `docker compose up` → `network not found` | İlk çalıştırmada network henüz oluşmadı | `docker compose up -d infra_net` ile tek tek başlat |
| 2 | Vault `connection refused` | TLS cert path yanlış | `/etc/personel/tls/vault.{crt,key}` varlığını kontrol et |
| 3 | Postgres migration dirty state | Bir önceki run yarıda kaldı | `psql -c "UPDATE schema_migrations SET dirty=false"` |
| 4 | ClickHouse port 9000 çakışması | Başka bir servis 9000'i kullanıyor | `.env` içinde `CLICKHOUSE_PORT=9100` |
| 5 | Keycloak realm import fail | KC_HOSTNAME eksik | `.env` içinde `KC_HOSTNAME=keycloak` (service name) |
| 6 | OpenSearch `path.logs not writable` | Volume permission | `chown 1000:1000 /var/lib/personel/opensearch/logs` |
| 7 | Gateway mTLS handshake fail | CA chain eksik | `/etc/personel/tls/tenant_ca.crt` Vault'tan yeniden çek |
| 8 | Console `/tr` redirect loop | Keycloak client secret yanlış | Realm → clients → personel-console → credentials |
| 9 | MinIO bucket create fail | MINIO_ROOT_USER boş | `.env` kontrol + `docker compose restart minio` |
| 10 | Enroll `401 unauthorized` | JWT süresi dolmuş | Keycloak'tan yeni token al |

Daha detaylı hata giderme: `docs/operations/troubleshooting.md`.

---

## Versiyon

| Sürüm | Tarih | Yazar | Değişiklik |
|---|---|---|---|
| 1.0 | 2026-04-13 | Faz 15 #157 | İlk sürüm — Faz 1 MVP kurulum kılavuzu |
