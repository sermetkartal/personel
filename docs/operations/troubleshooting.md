# Personel — Sorun Giderme Rehberi

> Dil: Türkçe. Belirti → Teşhis → Düzeltme formatında. Faz 1 deploy deneyiminden + Wave 1/2 operator lessons + pilot oturumlarından damıtılmıştır. 40+ senaryo.
>
> Daha ciddi olaylar için: `docs/security/incident-response-playbook.md`.

## İçindekiler

1. [API (Admin API)](#1-api-admin-api)
2. [Gateway + Enricher](#2-gateway--enricher)
3. [Console (Admin UI)](#3-console-admin-ui)
4. [Portal (Şeffaflık Portalı)](#4-portal-şeffaflık-portalı)
5. [Postgres](#5-postgres)
6. [ClickHouse](#6-clickhouse)
7. [Vault](#7-vault)
8. [NATS JetStream](#8-nats-jetstream)
9. [MinIO](#9-minio)
10. [OpenSearch](#10-opensearch)
11. [Keycloak](#11-keycloak)
12. [Docker / Compose](#12-docker--compose)
13. [Windows Agent](#13-windows-agent)
14. [Kurulum Ön Koşulları](#14-kurulum-ön-koşulları)

---

## 1. API (Admin API)

### 1.1 `401 Unauthorized` her request'te

**Belirti**: Tüm API çağrıları 401 dönüyor, WWW-Authenticate header `Bearer`.

**Teşhis**:
```bash
# JWT'yi decode et (base64 middle segment)
echo $JWT | cut -d. -f2 | base64 -d | jq
```

**Olası sebepler**:
- Token süresi dolmuş (`exp` geçmiş)
- Keycloak'ın `iss` claim'i API config'deki `oidc.issuer_url`'e uyuşmuyor
- JWKS fetch fail (API Keycloak'a erişemiyor)
- Clock drift — container saati kayık

**Çözüm**:
```bash
# Clock drift
docker exec personel-api date
docker exec personel-keycloak date
# Sync gerekirse: NTP aktif mi kontrol et

# JWKS reachability
docker exec personel-api curl -sk https://keycloak:8443/realms/personel/protocol/openid-connect/certs

# Issuer mismatch
docker exec personel-api grep oidc.issuer_url /etc/personel/api.yaml
```

### 1.2 `403 Forbidden` admin kullanıcıda bile

**Belirti**: Admin giriş yaptı ama cihaz listesine erişemiyor.

**Teşhis**: JWT'de `realm_access.roles` array'ini kontrol et. `admin` rolü var mı? `tenant_id` attribute'u var mı?

**Çözüm**:
```bash
# Keycloak'ta role ve attribute kontrol
docker exec personel-keycloak /opt/keycloak/bin/kcadm.sh get users -r personel -q username=admin
# tenant_id attribute yoksa:
docker exec personel-keycloak /opt/keycloak/bin/kcadm.sh update users/<id> \
  -r personel -s 'attributes.tenant_id=["'$TENANT_ID'"]'
# Kullanıcı çıkış yapıp yeniden girsin (yeni JWT)
```

### 1.3 `404 Not Found` mevcut endpoint'te

**Belirti**: OpenAPI spec'te olan endpoint 404 dönüyor.

**Olası sebep**: Router binding sırasında feature flag kapalı (`apps/api/internal/httpserver/router.go`).

**Çözüm**: Config'de ilgili feature enable edilmiş mi? API restart et.

### 1.4 `500 Internal Server Error` — vague

**Belirti**: 500, log'da stack trace yok.

**Teşhis**:
```bash
sudo docker compose logs api --tail 500 | grep -A 20 ERROR
```

En sık sebepler:
- Postgres connection pool tükenmiş → `max_conns` artır
- Vault token expired → API restart (AppRole token yenilemeyi tetikle)
- ClickHouse timeout → sorgu optimize

### 1.5 `413 Payload Too Large` — bulk endpoint

**Belirti**: Bulk endpoint kaydı 413 veriyor.

**Çözüm**: `apps/api/configs/api.yaml` `http.max_body_bytes` artır (varsayılan 10 MB). Caddy reverse proxy `request_body max_size` de artırılmalı.

### 1.6 `429 Too Many Requests`

**Belirti**: Üst üste istek atılınca 429 + `Retry-After: 30`.

**Olası sebep**: Per-tenant rate limit aşıldı (bkz. `internal/ratelimit/`).

**Çözüm**: Rate limit config'i `apps/api/configs/ratelimit.yaml`. Veya kaldıraç: `docker compose restart api` (window reset).

---

## 2. Gateway + Enricher

### 2.1 mTLS Handshake Fail (en sık)

**Belirti**: Gateway log'da `tls: handshake failure` veya `bad certificate`.

**Olası sebepler**:
1. Ajan cert'i Vault'tan revoke edilmiş
2. Ajan cert'i süresi dolmuş (90 gün)
3. Gateway tenant_ca.crt eski CA
4. Ajan SNI yanlış hostname

**Teşhis**:
```bash
# Ajan tarafında (Windows):
openssl s_client -connect gw.personel.musteri.local:9443 \
  -cert C:\ProgramData\Personel\agent\cert.pem \
  -key C:\ProgramData\Personel\agent\key.pem \
  -CAfile C:\ProgramData\Personel\agent\root_ca.pem

# Gateway tarafında:
docker compose logs gateway | grep -i 'verification error'
```

**Çözüm**:
- Cert expired → ajanı re-enroll et (token üret, `enroll.exe --token`)
- Revoked → ajan yeni enroll; log'da revoke sebebini DPO'ya bildir
- CA drift → `infra/scripts/ca-bootstrap.sh --rotate-trust` ve gateway restart

### 2.2 NATS Publish Error

**Belirti**: Gateway log'da `nats publish error: no responders`.

**Teşhis**:
```bash
docker compose logs nats --tail 100
docker exec personel-nats nats stream list
```

**Olası sebep**: JetStream stream `events_raw` yok.

**Çözüm**:
```bash
# Stream'i yeniden yarat
docker exec personel-nats nats stream add events_raw \
  --subjects "events.raw.>" \
  --retention limits --max-age 24h \
  --replicas 1
```

Wave 2'den gelen cluster'da R=2 kontrol: `nats stream info events_raw`.

### 2.3 ClickHouse Timeout (Enricher)

**Belirti**: Enricher log'da `context deadline exceeded` ClickHouse insert'te.

**Olası sebepler**:
- Merge queue tıkalı (çok parçalı insert)
- Disk I/O doyumu
- Network packet loss (cluster)

**Çözüm**:
```bash
docker exec personel-clickhouse clickhouse-client --query \
  "SELECT count() FROM system.merges"
# > 10 ise merge backlog var
docker exec personel-clickhouse clickhouse-client --query \
  "SELECT * FROM system.merges WHERE elapsed > 300"
```

Batch size'ı düşür (`apps/gateway/cmd/enricher/config.yaml` → `batch_size: 500 → 200`).

### 2.4 Ajan Batch Drop

**Belirti**: Enricher log `message dropped: schema validation failed`.

**Çözüm**: Schema registry güncellenmiş mi? Ajan eski proto v1 ama server v2 bekliyor. `docs/architecture/event-schema-registry.md`.

---

## 3. Console (Admin UI)

### 3.1 Keycloak Redirect Loop

**Belirti**: `/tr/dashboard` → Keycloak login → callback → tekrar login (sonsuz döngü).

**Olası sebepler**:
- `NEXTAUTH_URL` yanlış (trailing slash veya port mismatch)
- Keycloak client `valid redirect URIs` listesi eksik
- Cookie `Secure` set ama HTTP erişim

**Çözüm**:
```bash
# Keycloak client config
docker exec personel-keycloak /opt/keycloak/bin/kcadm.sh get clients/<id> -r personel | jq .redirectUris
# Eksikse:
docker exec personel-keycloak /opt/keycloak/bin/kcadm.sh update clients/<id> -r personel \
  -s 'redirectUris=["https://personel.musteri.local/*"]'
```

### 3.2 Cookie Cross-Origin Fail

**Belirti**: Console ve portal aynı domain'de değil, session cookie set olmuyor.

**Çözüm**:
- Reverse proxy ile aynı domain altında alt-path (`/console`, `/portal`) tercih et
- Veya cookie domain `.musteri.local` olsun (her ikisi de aynı parent'ta)

### 3.3 CORS Preflight Fail

**Belirti**: Browser console `CORS policy: no Access-Control-Allow-Origin`.

**Çözüm**: `apps/api/configs/api.yaml` `cors.allowed_origins` listesine console origin'i ekle. API restart.

### 3.4 `next-intl` — Locale 404

**Belirti**: `/en/dashboard` 404 ama `/tr/dashboard` çalışıyor.

**Çözüm**: `apps/console/src/i18n/config.ts` `locales` listesinde `en` var mı? `messages/en.json` dosyası var mı?

### 3.5 Static Asset 404 (`.woff2`)

**Belirti**: Inter font yüklenmiyor, `/fonts/inter-var.woff2` 404.

**Çözüm**: `apps/console/public/fonts/inter-var.woff2` commit edilmemiş (tech debt §10). Manuel indir veya CDN'e düş.

---

## 4. Portal (Şeffaflık Portalı)

Aynı Next.js tabanlı, Console ile paralel hatalar. Portal'a özgü:

### 4.1 Çalışan Giriş Yapamıyor

**Belirti**: LDAP ile kimlik doğrulanmış ama portal `403`.

**Teşhis**: Keycloak'ta `employee` rolü var mı? `tenant_id` attribute?

**Çözüm**: LDAP federation role mapper'da `employee` default olarak atansın:
```
Keycloak → User Federation → LDAP → Mappers → role-mapper → roles = employee
```

### 4.2 DSR Form Submit → 500

**Belirti**: KVKK m.11 başvuru form submit 500 veriyor.

**Çözüm**: API `/v1/dsr` endpoint'ine bakın. Postgres `dsr_requests` tablo migration'ı uygulanmış mı?

---

## 5. Postgres

### 5.1 Connection Pool Exhausted

**Belirti**: API log `dial postgres: too many clients already`.

**Teşhis**:
```bash
docker exec personel-postgres psql -U postgres -c \
  "SELECT count(*), state FROM pg_stat_activity GROUP BY state"
```

**Çözüm**:
- `postgresql.conf` `max_connections = 200` (varsayılan 100)
- API config `db.pool.max_open = 50`
- Idle connection'ları timeout'la: `idle_in_transaction_session_timeout = 60000`

### 5.2 Migration Dirty State

**Belirti**: API startup log `migration: Dirty database version X`.

**Çözüm**:
```sql
-- Son migration'ın sonucunu kontrol et
SELECT * FROM schema_migrations ORDER BY version DESC LIMIT 5;
-- Clean:
UPDATE schema_migrations SET dirty = false WHERE version = X;
-- API restart
```

Eğer migration yarıda kaldıysa: manuel olarak dosyayı bitir, sonra flag temizle.

### 5.3 Replication Lag (Wave 2 Cluster)

**Belirti**: `pg_stat_replication` → `replay_lag > 30s`.

**Teşhis**:
```sql
SELECT client_addr, state, replay_lag, sync_state FROM pg_stat_replication;
```

**Olası sebepler**:
- Network latency (vm3 ↔ vm5)
- Replica disk yavaş
- Write load burst

**Çözüm**: `docs/operations/postgres-replication.md` troubleshooting bölümü.

### 5.4 `audit.append_event` Signature Drift

**Belirti**: API startup `function audit.append_event(...) does not exist`.

**Teşhis**: init.sql vs migration 004 signature uyumsuzluğu (CLAUDE.md §0 tech debt).

**Çözüm**: Migration 0029 overload oluşturdu; yalnızca bu migration çalıştırılmalı. Manuel fix:
```sql
\df audit.append_event
-- Eksikse migration 0029 yeniden uygula
```

### 5.5 RLS Policy Fail

**Belirti**: Query boş sonuç dönüyor ama data var.

**Olası sebep**: `SET personel.tenant_id` session variable set edilmedi.

**Çözüm**: Her connection'da connection pool hook `SET personel.tenant_id = '<uuid>'`.

---

## 6. ClickHouse

### 6.1 Replication Lag (2-node)

**Belirti**: `system.replication_queue` büyüyor.

**Teşhis**:
```sql
SELECT table, num_tries, last_exception FROM system.replication_queue LIMIT 20;
```

**Çözüm**: `docs/operations/clickhouse-cluster.md` troubleshooting.

### 6.2 Merge Tree Mutation Stuck

**Belirti**: `system.mutations` → `is_done=0` saatlerdir.

**Teşhis**:
```sql
SELECT * FROM system.mutations WHERE is_done=0 ORDER BY create_time;
```

**Çözüm**:
```sql
-- Stuck mutation'ı iptal
KILL MUTATION WHERE mutation_id = '<id>';
```

### 6.3 Disk Full → Read-Only

**Belirti**: Insert `Too many parts (X). Parts cleaning are processing significantly slower`.

**Çözüm**:
- Disk temizle (`/var/lib/personel/clickhouse` quota)
- TTL manuel tetikle: `OPTIMIZE TABLE personel.events FINAL`
- Batch size'ı düşür

### 6.4 Query Timeout (p95 > 1s — Phase 1 exit fail)

**Teşhis**:
```sql
SELECT query_duration_ms, query FROM system.query_log
WHERE event_time > now() - INTERVAL 1 HOUR
ORDER BY query_duration_ms DESC LIMIT 10;
```

**Çözüm**: Skip index ekle, projection kullan, pre-aggregate materialized view.

### 6.5 DateTime64 TTL Hatası

**Belirti**: Schema reload `TTL expression cannot be evaluated`.

**Çözüm**: `toDateTime()` wrap gerekli. CLAUDE.md §0 tech debt; kalıcı fix push edilmiş (`apps/gateway/internal/clickhouse/schemas.go`).

---

## 7. Vault

### 7.1 Sealed After Restart

**Belirti**: `vault status` → `Sealed: true`.

**Çözüm**: Shamir unseal seremonisi (bkz. `docs/operations/installation-guide.md` §4.2).

### 7.2 AppRole Auth Fail

**Belirti**: Gateway log `vault: permission denied: approle login`.

**Olası sebepler**:
- Secret ID expired (TTL dolmuş)
- Role policy değişti, AppRole lost permission

**Çözüm**:
```bash
docker exec personel-vault vault write -f auth/approle/role/gateway-service/secret-id
# Yeni secret_id al, config'e yaz, gateway restart
```

### 7.3 PKI Role Not Found

**Belirti**: `pki/roles/agent-cert: no handler for route`.

**Çözüm**: PKI engine disable edilmiş. `ca-bootstrap.sh` yeniden çalıştır.

### 7.4 `disable_mlock` Warning (dev)

**Belirti**: Log `mlock is not supported`.

**Açıklama**: Dev kurulumda normal (Docker default). Production'da `disable_mlock=false` + `cap_add: IPC_LOCK`.

### 7.5 Audit Device Full

**Belirti**: Vault write'lar `audit log write failure`.

**Çözüm**: `/var/lib/personel/vault/audit/vault_audit.log` dosyasını rotate et. Cron: `infra/scripts/vault-audit-rotate.sh`.

---

## 8. NATS JetStream

### 8.1 Stream Depth Büyüyor

**Belirti**: `events_raw` depth > 10k, consumer lag.

**Teşhis**:
```bash
docker exec personel-nats nats stream info events_raw
docker exec personel-nats nats consumer info events_raw enricher
```

**Olası sebep**: Enricher slow / down.

**Çözüm**: Enricher scale, batch_size artır, ya da temporary drain consumer:
```bash
docker exec personel-nats nats consumer delete events_raw enricher
# Enricher'ı restart et → yeniden create edecek
```

### 8.2 `no responders` Publish Hatası

**Açıklama**: Stream yok veya subject mismatch.

**Çözüm**: Bkz. §2.2

### 8.3 Cluster Split-Brain (R=2)

**Belirti**: vm3 ve vm5'te farklı `events_raw` state.

**Teşhis**: `nats server list` → raft leader?

**Çözüm**: `docs/operations/nats-minio-cluster.md` split-brain bölümü.

### 8.4 DeliverAllPolicy Hatası

**Açıklama**: Eski consumer config'i `DeliverLast` ama yeni batch beklenen `DeliverAll`.

**Çözüm**: Gateway içinde router.go düzeltildi (CLAUDE.md §0); migration otomatik uygulanır.

---

## 9. MinIO

### 9.1 Bucket Create Fail

**Belirti**: API log `AccessDenied` MinIO PUT.

**Teşhis**:
```bash
docker exec personel-minio mc admin info myminio
docker exec personel-minio mc ls myminio
```

**Çözüm**: IAM policy eksik. `infra/compose/minio/policies/*.json` uygulandı mı?
```bash
docker exec personel-minio mc admin policy set myminio app-rw user=personel_app
```

### 9.2 Object Lock Violation

**Belirti**: WORM bucket'a DELETE denendi → `Object is WORM protected`.

**Açıklama**: Bu beklenen davranış — Compliance mode WORM 5 yıl. Silme yapmayın; TTL ile bekleyin.

### 9.3 Erasure Set Degraded (4-node)

**Belirti**: `mc admin info` → `Drives: 3 online, 1 offline`.

**Çözüm**: Offline drive'ı replace + `mc admin heal myminio`.

Detay: `docs/operations/nats-minio-cluster.md`.

---

## 10. OpenSearch

### 10.1 Cluster Yellow/Red

**Teşhis**:
```bash
curl -sk -u admin:$PWD https://opensearch:9200/_cluster/health?pretty
curl -sk -u admin:$PWD https://opensearch:9200/_cat/shards | grep -v STARTED
```

**Olası sebepler**:
- Tek node'lu cluster → yellow normal
- Shard unallocated → disk watermark aşıldı

**Çözüm**:
```bash
curl -XPUT -u admin:$PWD https://opensearch:9200/_cluster/settings -d \
  '{"persistent":{"cluster.routing.allocation.disk.watermark.high":"90%"}}'
```

### 10.2 `path.logs not writable`

**Çözüm**: Kalıcı fix push edilmiş (`infra/compose/opensearch/opensearch.yml`). Volume permission: `chown 1000:1000 /var/lib/personel/opensearch/logs`.

### 10.3 Shard Allocation Failed

**Teşhis**: `GET /_cluster/allocation/explain`.

**Çözüm**: Node capacity ekle veya shard sayısı azalt (index template).

---

## 11. Keycloak

### 11.1 Realm Import Fail

**Belirti**: Startup log `Failed to load realm`.

**Olası sebepler**:
- JSON syntax hatası
- Duplicate client (import --override false)

**Çözüm**: `--override true` flag kullan. Veya manuel import Admin UI'dan.

### 11.2 `KC_HOSTNAME` Hatası

**Belirti**: Login sayfası redirect `https://localhost:8080`.

**Çözüm**: `.env` içinde `KC_HOSTNAME=auth.personel.musteri.local` + reverse proxy header `X-Forwarded-Host`.

### 11.3 Infinispan Cluster Split (HA)

**Belirti**: User login bir node'da geçerli, diğerinde 401.

**Çözüm**: `docs/operations/opensearch-keycloak-cluster.md` split-brain bölümü.

### 11.4 `tenant_id` Claim Missing

**Çözüm**: Protocol mapper eklendi mi? `docs/operations/installation-guide.md` §5'te tanımlı.

---

## 12. Docker / Compose

### 12.1 Container Restart Loop

**Teşhis**:
```bash
docker compose ps
docker compose logs <service> --tail 50
```

**Sık sebepler**:
- OOM kill (RAM limit)
- Healthcheck fail → restart policy
- Config hatası

**Çözüm**: `mem_limit` artır, healthcheck endpoint'i elle dene, config validate.

### 12.2 Network Not Found

**Belirti**: `network personel_default not found`.

**Çözüm**: 
```bash
docker network ls | grep personel
docker compose up -d  # network yeniden yaratılır
```

### 12.3 Volume Mount Failed

**Belirti**: `error mounting ... no such file`.

**Çözüm**: `/var/lib/personel/<service>` dizini yoksa oluştur, chown doğru kullanıcıya.

### 12.4 Image Pull Rate Limit

**Belirti**: `toomanyrequests: You have reached your pull rate limit`.

**Çözüm**: Docker Hub login, veya müşteri registry'ye mirror (`docs/operations/registry-policies.md`).

---

## 13. Windows Agent

### 13.1 Enroll `401 Unauthorized`

**Belirti**: `enroll.exe --token ...` → `HTTP 401`.

**Olası sebep**: Token süresi dolmuş veya bir kez kullanılmış.

**Çözüm**: Konsoldan yeni token al.

### 13.2 Service Not Starting

**Belirti**: `Start-Service PersonelAgent` → `service did not start in time`.

**Teşhis**: Event Viewer → Windows Logs → System → Source: Service Control Manager.

**Olası sebepler**:
- DPAPI seal read fail (profile yok)
- Config path yanlış
- Watchdog binary missing

**Çözüm**:
```powershell
# Log incele
Get-EventLog -LogName Application -Source PersonelAgent -Newest 10
# Config kontrol
Get-Content C:\ProgramData\Personel\agent\config.toml
# Re-enroll (config corrupt ise)
Remove-Item C:\ProgramData\Personel\agent\* -Force
.\enroll.exe --token <new>
```

### 13.3 mTLS Handshake Fail (Agent tarafında)

**Belirti**: Log `failed to connect: tls handshake timeout`.

**Çözüm**: §2.1.

### 13.4 SQLite Queue Corruption

**Belirti**: Log `database disk image is malformed`.

**Çözüm**:
```powershell
Stop-Service PersonelAgent
Remove-Item C:\ProgramData\Personel\agent\queue.db*
Start-Service PersonelAgent
# İn-flight event'ler kaybolur, yenileri akmaya başlar
```

### 13.5 Auto-Update Fail

**Belirti**: Log `update signature verification failed`.

**Çözüm**: Dual-signed manifest doğruluyor mu? Update server'dan manifest'i manuel çek ve doğrula.

### 13.6 Anti-Tamper Alert Spam

**Belirti**: `agent.tamper_detected` event'leri sürekli.

**Olası sebep**: Legitimate Windows güncellemesi registry ACL değiştirdi.

**Çözüm**: Watchdog log'unu incele. False positive ise allow-list ekle (`C:\ProgramData\Personel\agent\tamper-allowlist.json`).

---

## 14. Kurulum Ön Koşulları

### 14.1 `docker: permission denied`

**Çözüm**:
```bash
sudo usermod -aG docker $USER
newgrp docker
```

### 14.2 `vm.max_map_count too low` (OpenSearch)

**Çözüm**:
```bash
echo "vm.max_map_count=262144" | sudo tee /etc/sysctl.d/99-personel.conf
sudo sysctl -p /etc/sysctl.d/99-personel.conf
```

### 14.3 Disk Space Critical

**Teşhis**:
```bash
df -h /var/lib/personel
du -sh /var/lib/personel/*
```

**Çözüm**: ClickHouse TTL tetikle, Docker image prune, eski log'ları rotate.

### 14.4 Clock Drift

**Belirti**: JWT `exp` kontrolü fail, mTLS cert validity kontrolü fail.

**Çözüm**:
```bash
sudo timedatectl set-ntp true
sudo systemctl restart chronyd
```

### 14.5 DNS 1.1.1.1 Bloke

**Açıklama**: CLAUDE.md — bu ağda 1.1.1.1 bloke. 8.8.8.8 kullanılır.

**Çözüm**: `/etc/systemd/resolved.conf` → `DNS=8.8.8.8`, `chattr +i` ile kalıcı.

### 14.6 `ulimit -n` Low

**Çözüm**:
```bash
echo "* soft nofile 65536" | sudo tee -a /etc/security/limits.conf
echo "* hard nofile 65536" | sudo tee -a /etc/security/limits.conf
```

### 14.7 `.env` CHANGEME Kaldı

**Çözüm**: `infra/scripts/preflight-check.sh` bunu yakalar. `grep CHANGEME /opt/personel/infra/compose/.env` ile manuel kontrol.

### 14.8 TLS Cert Expired (Pilot Yenileme)

**Belirti**: Kurulum 1 yıl önce, cert'ler süresi doldu.

**Çözüm**: `infra/scripts/cert-rotate-all.sh` çalıştır (Vault'tan yeniden iste). Tüm servisler kısa süreli restart.

---

## Ek: Genel Teşhis Komutları

```bash
# Docker stats canlı
docker stats --no-stream

# Disk I/O
iostat -x 1

# Network connections
ss -tnp | grep :9443

# Memory
free -m
cat /proc/meminfo

# Systemd timer'lar
systemctl list-timers personel-*

# Docker event stream
docker events --since 1h

# Full stack health tek komut
sudo /opt/personel/infra/scripts/smoke-test.sh
```

---

## Versiyon

| Sürüm | Tarih | Değişiklik |
|---|---|---|
| 1.0 | 2026-04-13 | Faz 15 #159 — İlk sürüm (Wave 1/2 pilot lessons dahil) |
