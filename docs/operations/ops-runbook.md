# Personel — Operasyonel Runbook (Günlük İşletim)

> Dil: Türkçe. Hedef okuyucu: Personel operatörü, SRE, nöbetçi yönetici. Bu doküman **günlük işlem rehberidir**; incident response için `docs/security/incident-response-playbook.md`'ya bakın.

## İçindekiler

1. [Servis Kontrolü](#1-servis-kontrolü)
2. [Sağlık Kontrolleri](#2-sağlık-kontrolleri)
3. [Log Lokasyonları](#3-log-lokasyonları)
4. [Backup Doğrulama](#4-backup-doğrulama)
5. [Monitoring Dashboard'ları](#5-monitoring-dashboardları)
6. [Alert Cevap Rehberi](#6-alert-cevap-rehberi)
7. [Sık Karşılaşılan Operatör Görevleri](#7-sık-karşılaşılan-operatör-görevleri)

---

## 1. Servis Kontrolü

### 1.1 Stack Başlatma

```bash
cd /opt/personel/infra/compose
sudo docker compose up -d
```

Sıralı başlatma (manuel):

```bash
# 1. Data layer (Vault, Postgres, ClickHouse, MinIO, NATS, OpenSearch, Keycloak)
sudo docker compose up -d vault postgres clickhouse minio nats opensearch keycloak

# Vault unseal (varsa manuel)
sudo infra/scripts/vault-unseal.sh

# 2. Application layer
sudo docker compose up -d api gateway enricher console portal

# 3. Reverse proxy
sudo docker compose up -d caddy
```

### 1.2 Stack Durdurma (Graceful Shutdown)

Veri bütünlüğü için **tam ters sıra** kullanılır:

```bash
# 1. Edge — yeni bağlantıları kes
sudo docker compose stop caddy

# 2. Uygulama — inflight request'leri bitir (30s grace)
sudo docker compose stop console portal
sudo docker compose stop gateway  # ajanlar yeniden bağlanmayı dener — OK
sudo docker compose stop api enricher

# 3. Data layer — en son
sudo docker compose stop nats opensearch keycloak minio clickhouse postgres vault
```

> **Asla `docker compose down -v` komutunu prod'da kullanmayın** — volume'ları siler!

### 1.3 Tek Servis Yeniden Başlatma

```bash
# API'yi restart et (config değişikliği sonrası)
sudo docker compose restart api

# Rolling restart (zero-downtime için gateway scale kullan)
sudo docker compose up -d --scale gateway=2 --no-recreate gateway
sudo docker compose restart gateway
```

### 1.4 Stack Durum Kontrolü

```bash
# Tüm servislerin durumu
sudo docker compose ps

# Health + uptime
sudo docker compose ps --format 'table {{.Name}}\t{{.Status}}\t{{.Ports}}'
```

---

## 2. Sağlık Kontrolleri

### 2.1 HTTP Endpoint'leri

```bash
# API
curl -sk https://api.personel.musteri.local/healthz | jq
# Beklenen: {"status":"ok","oidc":"ok","db":"ok","vault":"ok","nats":"ok"}

# Gateway (gRPC üzerinden)
grpcurl -insecure api.personel.musteri.local:9443 grpc.health.v1.Health/Check
# Beklenen: {"status":"SERVING"}

# Console (Next.js)
curl -sk https://personel.musteri.local/tr | grep -q 'Personel' && echo OK
```

### 2.2 Veri Katmanı

```bash
# Postgres
docker exec personel-postgres pg_isready -U postgres
docker exec personel-postgres psql -U postgres -c "SELECT count(*) FROM pg_stat_activity"

# ClickHouse
docker exec personel-clickhouse clickhouse-client --query "SELECT version()"
docker exec personel-clickhouse clickhouse-client --query \
  "SELECT database, name, engine FROM system.tables WHERE database='personel'"

# NATS
docker exec personel-nats wget -qO- "http://127.0.0.1:8222/healthz"
docker exec personel-nats wget -qO- "http://127.0.0.1:8222/jsz?streams=1" | jq .total_streams

# MinIO
docker exec personel-minio mc admin info myminio

# Vault
docker exec personel-vault vault status

# OpenSearch
curl -sk -u admin:$OPENSEARCH_PWD https://opensearch:9200/_cluster/health | jq .status
# Beklenen: green veya yellow (1-node cluster için yellow normaldir)

# Keycloak
curl -sk https://auth.personel.musteri.local/health/ready
```

### 2.3 End-to-End Smoke Test

```bash
sudo /opt/personel/infra/scripts/smoke-test.sh
```

Bu script şunları test eder:
1. Tüm healthz endpoint'leri
2. Admin API ile login (test user)
3. Test event'i NATS'a yayımla → ClickHouse'ta gör
4. Audit log append + verify
5. Policy push → test endpoint'ine ulaşma

---

## 3. Log Lokasyonları

### 3.1 Docker Logları

```bash
# Canlı takip
sudo docker compose logs -f api
sudo docker compose logs -f --since 1h gateway

# Belirli bir servis, son 100 satır
sudo docker compose logs --tail 100 clickhouse

# Tüm servisler, son 5 dakika
sudo docker compose logs --since 5m
```

### 3.2 Host Logları

| Lokasyon | İçerik |
|---|---|
| `/var/log/personel/install.log` | install.sh son çalıştırma |
| `/var/log/personel/backup/*.log` | Nightly backup sonuçları |
| `/var/log/personel/cert-rotate.log` | Cert rotasyon (systemd timer) |
| `/var/log/personel/audit/*.jsonl` | API audit log mirror |
| `/var/lib/personel/vault/audit/vault_audit.log` | Vault audit device |

### 3.3 Sık Kullanılan Log Query'leri

```bash
# API'de 5xx'ler
sudo docker compose logs api --since 1h | jq 'select(.status >= 500)'

# Gateway mTLS handshake fail'leri
sudo docker compose logs gateway --since 1h | grep 'tls handshake'

# Enricher drop'lar
sudo docker compose logs enricher --since 1h | grep 'dropped'

# Audit trail'de silme girişimleri (olmaması gerek)
docker exec personel-postgres psql -U postgres -d personel -c \
  "SELECT * FROM audit_log WHERE action LIKE 'audit_log.delete%' ORDER BY ts DESC LIMIT 10"
```

---

## 4. Backup Doğrulama

### 4.1 Günlük Backup Kontrol

```bash
# Backup timer durumu
sudo systemctl list-timers personel-backup.timer

# Son backup'ın sonucu
sudo journalctl -u personel-backup.service --since "24 hours ago"

# Backup dosyaları
ls -lh /var/lib/personel/backups/$(date +%Y-%m-%d)/
```

Beklenen dosyalar (her gece):
- `postgres-full-YYYY-MM-DD.sql.gz`
- `clickhouse-parts-YYYY-MM-DD.tar.zst`
- `minio-audit-worm-YYYY-MM-DD.tar` (sadece metadata; WORM bucket değişmez)
- `vault-snapshot-YYYY-MM-DD.snap`
- `manifest.json` (SHA256 + dosya boyutları + signature)

### 4.2 Backup Doğruluğu

```bash
# Manifest signature'ı doğrula
sudo /opt/personel/infra/scripts/verify-backup.sh /var/lib/personel/backups/$(date +%Y-%m-%d)/

# Postgres dump integrity
gunzip -t /var/lib/personel/backups/$(date +%Y-%m-%d)/postgres-full-*.sql.gz && echo OK
```

### 4.3 Aylık Restore Drill

KVKK m.12 teknik tedbirler ve ISO 27001 A.17.1.3 için aylık restore tatbikatı:

```bash
# Test VM'ye restore
sudo /opt/personel/infra/scripts/restore-drill.sh \
  --source /var/lib/personel/backups/$(date +%Y-%m-%d)/ \
  --target-host drill.personel.internal \
  --verify-events
```

Detay: `infra/runbooks/backup-restore.md` ve `docs/operations/backup-restore.md`.

---

## 5. Monitoring Dashboard'ları

Grafana erişim: `https://grafana.personel.musteri.local`

### 5.1 Ana Dashboard'lar

| Dashboard | Amaç | Panel Örnekleri |
|---|---|---|
| **Stack Overview** | Tüm servislerin genel durumu | Service up/down, CPU, RAM, disk |
| **Ingest Pipeline** | Gateway → NATS → CH akışı | Msg/s, lag, drop rate, p95 latency |
| **Endpoint Health** | Ajanlar | Online/offline, heartbeat age, enroll rate |
| **ClickHouse** | Time-series DB | Query rate, p95, mutation queue, merge backlog |
| **Postgres** | Metadata | Connection pool, replication lag, slow queries |
| **NATS** | JetStream | Stream depth, consumer lag, ack rate |
| **MinIO** | Object store | Bucket sizes, PUT/GET/s, erasure set health |
| **Vault** | PKI + secrets | Seal status, issued cert count, audit events |
| **Live View** | WebRTC | Active sessions, approval rate, avg duration |
| **DSR Dashboard** | KVKK m.11 | Open DSRs, SLA gauge (30d), overdue count |
| **Audit Integrity** | Hash chain | Last verified, chain head, drift |
| **Backup Health** | Günlük yedekler | Last run, size trend, drill status |

### 5.2 Kritik Alert'ler (Prometheus → AlertManager)

`infra/compose/prometheus/alerts.yml` dosyasındaki tanımlar:

| Alert | Severity | Tetikleyici |
|---|---|---|
| `PersonelServiceDown` | critical | herhangi servis > 2m down |
| `GatewayMtlsHandshakeFailures` | warning | handshake fail rate > 5% |
| `NatsStreamLag` | warning | events_raw lag > 10s |
| `ClickHouseMutationStuck` | critical | mutation queue > 1h |
| `PostgresReplicationLag` | warning | replica lag > 30s |
| `VaultSealed` | critical | vault sealed |
| `DSRDeadlineApproaching` | warning | herhangi DSR SLA < 3 gün |
| `AuditChainBroken` | critical | daily checkpoint fail |
| `BackupFailed` | critical | nightly backup fail |
| `DiskSpaceCritical` | critical | data partition > 85% |
| `AgentMassDisconnect` | critical | > 20% endpoint offline in 5m |
| `SOC2EvidenceCoverageGap` | warning | 24h zero evidence window |

---

## 6. Alert Cevap Rehberi (Brief)

Tam incident response: `docs/security/incident-response-playbook.md`. Burada **ilk 5 dakikalık tepki** bulunur.

### `PersonelServiceDown`

1. `docker compose ps` → hangi servis down?
2. `docker compose logs <service> --tail 200` → hata pattern'ini bul
3. Basit restart dene: `docker compose restart <service>`
4. 2 deneme sonrası çözülmediyse → P2 incident aç, ekip bildir
5. Gateway down ise: ajanlar yeniden bağlanmayı dener (backoff), veri kaybı yok (SQLite queue)

### `VaultSealed`

1. Vault restart oldu mu? (`systemctl status docker` + container restart time)
2. Shamir seremonisi yap (bkz. installation-guide §4.2)
3. Unseal sonrası API ve Gateway restart et (AppRole token'ları yenilemek için)
4. P1 incident — müşteri DPO bildirimi gerekli mi? 72h KVKK m.12 breach kapsamı değerlendir

### `AuditChainBroken`

**KRİTİK — KVKK m.12 kanıt bütünlüğü**.

1. Audit log tablosunda son sağlam checkpoint'i bul:
   ```sql
   SELECT * FROM audit_checkpoints ORDER BY created_at DESC LIMIT 5;
   ```
2. Checkpoint'ler arası kayıtları incele — trigger bypass şüphesi var mı?
3. `docker exec personel-postgres psql -c "SELECT audit.verify_chain('tenant-id', '2026-04-01', '2026-04-13')"`
4. WORM mirror'ı kontrol et — MinIO audit-worm bucket'ta son mirror zamanı
5. P1 incident — DPO acil bildirim. Kurul 72h bildirimi gerekli olabilir.

Detay: `docs/architecture/audit-chain-checkpoints.md`.

### `DSRDeadlineApproaching`

1. Konsolda DSR dashboard'a git (`/tr/dsr`)
2. SLA kritik DSR'ları filtrele
3. DPO'ya bildir (Slack/email)
4. 30 gün limit yaklaştıysa: DPO takdirinde uzatma gerekçesi + çalışana bilgilendirme

---

## 7. Sık Karşılaşılan Operatör Görevleri

### 7.1 Admin Şifre Sıfırlama

```bash
docker exec personel-keycloak /opt/keycloak/bin/kcadm.sh config credentials \
  --server http://localhost:8080 --realm master \
  --user $KEYCLOAK_ADMIN --password $KEYCLOAK_ADMIN_PASSWORD

docker exec personel-keycloak /opt/keycloak/bin/kcadm.sh set-password \
  -r personel --username <user> \
  --new-password 'TempPassword123!' --temporary
```

Audit log otomatik yazılır. Kullanıcı ilk girişte değiştirmeye zorlanır.

### 7.2 Yeni Ajan Enroll Token Üretme

```bash
JWT=$(curl -sk -X POST https://auth.personel.musteri.local/realms/personel/protocol/openid-connect/token \
  -d "grant_type=password&client_id=personel-api&username=admin&password=$PWD" | jq -r .access_token)

curl -sk -X POST https://api.personel.musteri.local/v1/endpoints/enroll \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"'$TENANT_ID'","asset_tag":"LAPTOP-NEW"}'
```

Token 24 saat geçerli. Bir kez kullanılabilir.

### 7.3 Şüpheli Ajan Sertifikasını İptal Etme

```bash
# Vault'ta revoke
docker exec personel-vault vault write pki/revoke serial_number=<hex-serial>

# Gateway'e yeni CRL push
docker exec personel-vault vault read pki/crl/rotate
sudo docker compose restart gateway
```

Veya API üzerinden (audit log'a yazılır):

```bash
curl -sk -X POST https://api.personel.musteri.local/v1/endpoints/<id>/revoke \
  -H "Authorization: Bearer $JWT" \
  -d '{"reason":"compromised","ticket_id":"INC-2026-0042"}'
```

### 7.4 Vault Root Token Rotasyonu

Root token varsayılanda **asla kullanılmaz**; rotasyon acil durum sonrası:

```bash
# Yeni root token generate (Shamir seremoniği tekrar)
docker exec personel-vault vault operator generate-root -init
# → nonce + otp
docker exec personel-vault vault operator generate-root -nonce=<nonce> <share1>
# ... 3 share girilir
docker exec personel-vault vault operator generate-root -decode=<encoded> -otp=<otp>

# Eski token'ı iptal
docker exec personel-vault vault token revoke <old-token>
```

### 7.5 Yeni Tenant Ekleme

Faz 1'de pilot tek-tenant. Multi-tenant Faz 2+.

```bash
# 1. Keycloak realm'de tenant_id attribute ekle
# 2. Postgres'te tenant satırı
docker exec personel-postgres psql -U postgres -d personel -c \
  "INSERT INTO tenants (id, name, domain, created_at) VALUES ('$NEW_TENANT_ID', 'New Corp', 'newcorp.local', now())"

# 3. Vault PKI namespace (opsiyonel isolation)
# 4. ClickHouse row-level partition anahtarı zaten tenant_id'ye göre

# 5. Admin kullanıcıyı yeni tenant'a ata
docker exec personel-keycloak /opt/keycloak/bin/kcadm.sh update users/<user-id> \
  -r personel -s 'attributes.tenant_id=["'$NEW_TENANT_ID'"]'
```

### 7.6 Keycloak Mevcut Kullanıcı İçe Aktarma

LDAP federation:

```
Keycloak Admin → realm personel → User Federation → Add provider → LDAP
→ Connection URL: ldaps://ad.musteri.local:636
→ Bind DN: CN=personel-svc,OU=ServiceAccounts,DC=musteri,DC=local
→ Sync period: 12 hours
→ Import users: ON
```

Detay: `docs/operations/keycloak-ldap-federation.md` (opsiyonel eklenti).

### 7.7 Retention Enforcement (Manuel Tetik)

```bash
# ClickHouse TTL tetikle (otomatik çalışır ama manuel de mümkün)
docker exec personel-clickhouse clickhouse-client --query \
  "OPTIMIZE TABLE personel.events FINAL"

# Postgres scheduled cleanup
docker exec personel-postgres psql -U postgres -d personel -c \
  "SELECT retention.enforce_all('$TENANT_ID')"

# MinIO lifecycle (otomatik — her 24h)
docker exec personel-minio mc ilm ls myminio/screenshots
```

Detay: `infra/runbooks/retention-enforcement.md`.

### 7.8 Manuel Backup Tetikleme

```bash
sudo systemctl start personel-backup.service
# veya doğrudan
sudo /opt/personel/infra/scripts/backup-nightly.sh --on-demand
```

Sonuç `/var/log/personel/backup/` ve `/var/lib/personel/backups/<tarih>/` altına düşer.

---

## 8. Bakım Pencereleri

### 8.1 Haftalık Bakım (Pazar 03:00-05:00)

- ClickHouse `OPTIMIZE TABLE ... FINAL` büyük tablolar için
- Postgres `VACUUM ANALYZE` 
- OpenSearch force merge (arşiv indeksleri)
- Docker image cleanup: `docker image prune -a --filter "until=168h"`
- Log rotation kontrol

### 8.2 Aylık Bakım

- Backup restore drill (§4.3)
- Sertifika rotasyon dry-run
- Vault audit log archive
- Secrets rotation (API key'ler, DB şifreleri) — `docs/operations/secret-rotation.md`

### 8.3 Çeyrek Bakım

- Disaster recovery tatbikatı
- Full chain audit verification (tüm `audit_log` satırları)
- DPIA revizyonu (müşteri DPO)
- KVKK uyum denetimi iç raporu

---

## Versiyon

| Sürüm | Tarih | Değişiklik |
|---|---|---|
| 1.0 | 2026-04-13 | Faz 15 #158 — İlk sürüm |
