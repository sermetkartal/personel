# PostgreSQL TLS Üretim Geçiş Runbook'u

> Roadmap #42 — Postgres `sslmode=disable` dev kısayolundan üretim
> `sslmode=verify-full` moduna geçiş. Tüm adımlar geri alınabilir.

## Önkoşullar

- [ ] Vault PKI engine kurulu (Roadmap #41 tamamlandı)
- [ ] Vault PKI altında `server-cert` rolü tanımlı (CN allow-list:
      `*.personel.internal`, IP SAN izinli)
- [ ] `tenant_ca.crt` ve `root_ca.crt` `/etc/personel/tls/` altında mevcut
- [ ] Postgres container şu an base compose ile ayakta, `sslmode=disable`
      ile API bağlanabiliyor
- [ ] Postgres bind-mount data dizini `/var/lib/personel/postgres/data`
      sağlam (kontrol: `ls -la /var/lib/personel/postgres/data/global`)
- [ ] WAL arşiv dizini hazır:
      ```bash
      sudo mkdir -p /var/backups/personel/pg/wal
      sudo chown 999:999 /var/backups/personel/pg/wal   # postgres uid
      sudo chmod 750 /var/backups/personel/pg/wal
      ```

## Geçiş Sırası — Zorunlu Sıra

> **Cert önce. Config sonra. Service en son.**
> Cert dosyaları yerinde olmadan postgres yeni config ile başlatılırsa
> conteyner crashloop'a girer çünkü `ssl_cert_file` okunamaz.

### 1. Vault'tan postgres server cert üret

```bash
# Vault token'ınız production root token olmalı (login ceremony)
vault login -method=oidc

# Postgres için 90 gün TTL'li cert iste
vault write -format=json pki/issue/server-cert \
  common_name="postgres.personel.internal" \
  alt_names="postgres,personel-postgres" \
  ip_sans="192.168.5.44,127.0.0.1" \
  ttl="2160h" \
  > /tmp/postgres-cert.json

# Cert + key + chain'i hedef konumlara yaz
sudo mkdir -p /etc/personel/tls
sudo jq -r .data.certificate     /tmp/postgres-cert.json | sudo tee /etc/personel/tls/postgres.crt > /dev/null
sudo jq -r .data.private_key     /tmp/postgres-cert.json | sudo tee /etc/personel/tls/postgres.key > /dev/null
sudo jq -r .data.issuing_ca      /tmp/postgres-cert.json | sudo tee -a /etc/personel/tls/postgres.crt > /dev/null

# İzinler
sudo chown 999:999 /etc/personel/tls/postgres.key   # postgres container uid
sudo chmod 0600 /etc/personel/tls/postgres.key
sudo chmod 0644 /etc/personel/tls/postgres.crt

# Cert geçici dosyayı sil (private key içerdiği için)
sudo shred -u /tmp/postgres-cert.json

# Doğrula
openssl x509 -in /etc/personel/tls/postgres.crt -noout -subject -issuer -dates
```

Beklenen çıktı: subject CN postgres.personel.internal, issuer Personel
Tenant CA, notAfter ~3 ay sonrası.

### 2. Compose override'u devreye al

```bash
cd /opt/personel/infra/compose

# Doğrula: yeni override'un parsing'i temiz mi?
docker compose \
  -f docker-compose.yaml \
  -f postgres/docker-compose.tls-override.yaml \
  config postgres > /tmp/postgres-effective.yaml

# Effective config'i incele — cert mount + command satırlarını gör
less /tmp/postgres-effective.yaml
```

### 3. Postgres'i yeniden başlat

```bash
docker compose \
  -f docker-compose.yaml \
  -f postgres/docker-compose.tls-override.yaml \
  up -d --force-recreate postgres

# Healthcheck geçene kadar bekle (<60s)
docker compose ps postgres
docker compose logs --tail=50 postgres
```

Loglarda görmen gerekenler:
- `LOG: ssl=on, secure protocols TLSv1.2 - TLSv1.3`
- `LOG: database system is ready to accept connections`

Görmemen gerekenler:
- `FATAL: could not load server certificate file` → cert path/perms sorunu
- `FATAL: invalid value for parameter "ssl_cert_file"` → conf yolu yanlış

### 4. API container'ından cert verify ile bağlantı testi

```bash
docker compose exec api sh -c '
  psql "host=postgres port=5432 dbname=personel user=app_admin_api \
        sslmode=verify-full sslrootcert=/etc/personel/tls/root_ca.crt" \
    -c "SELECT version(), inet_server_addr();"
'
```

Beklenen: PostgreSQL 16.x sürüm bilgisi.
Hata olursa:
- `SSL error: certificate verify failed` → root_ca.crt API container içine
  mount edilmiş mi kontrol et
- `password authentication failed` → cert OK, parola yanlış (ayrı sorun)

### 5. API + gateway + enricher config güncelle

`apps/api/configs/api.yaml.tls-snippet` dosyasındaki `postgres.dsn` satırını
gerçek `apps/api/configs/api.yaml` içine kopyala. Aynısını
`apps/gateway/configs/gateway.yaml` ve enricher için tekrarla.

Sonra:

```bash
docker compose restart api gateway enricher
docker compose logs --tail=20 api gateway enricher | grep -i ssl
```

Beklenen: hiçbir SSL hatası yok, `database connection established`
loglarını gör.

### 6. Doğrulama testi (smoke)

```bash
# API healthz hala 200 dönüyor mu?
curl -sf http://127.0.0.1:8000/healthz | jq

# Gateway /healthz?
docker compose exec gateway /personel-gateway healthcheck && echo OK

# Postgres içeriden TLS bağlantısı görünüyor mu?
docker compose exec postgres psql -U postgres -d personel -c \
  "SELECT pid, usename, application_name, ssl, ssl_version, ssl_cipher
     FROM pg_stat_ssl
     JOIN pg_stat_activity USING (pid)
    WHERE state IS NOT NULL;"
```

Her satırda `ssl=t`, `ssl_version=TLSv1.2` veya `TLSv1.3` görmelisin.

## Geri Alma (Rollback)

Bir şey patladıysa:

```bash
cd /opt/personel/infra/compose
docker compose -f docker-compose.yaml up -d --force-recreate postgres
```

Bu komut TLS override'u devre dışı bırakır, base compose'taki config'e döner
(base config aslında `ssl=on` ama eski self-signed cert'i bekliyor).

Daha sert rollback (dev mode):

```bash
docker compose -f docker-compose.yaml -f docker-compose.dev.yaml \
  up -d --force-recreate postgres
```

Her iki rollback de data volume'u koruduğu için DB içeriği kaybolmaz.

## Cert Yenileme Otomasyonu

90 günlük cert TTL geliyor → `personel-cert-renewer.timer` (Roadmap #54)
postgres sertini de listesine almalı. Renewer çalıştığında:

1. Vault'tan yeni cert al
2. `/etc/personel/tls/postgres.{crt,key}` üzerine yaz
3. `pg_ctl reload` (HUP) → SSL re-load, mevcut bağlantılar etkilenmez

`infra/scripts/rotate-secrets.sh` dosyasında postgres-cert renewal section
hazır olmalı (Roadmap #54 paralel agent'ı tamamlayacak).

## Troubleshooting

| Belirti | Sebep | Çözüm |
|---|---|---|
| `certificate verify failed: unable to get local issuer certificate` | Client `root_ca.crt` mount edilmemiş veya yanlış path | API/gateway container'a `/etc/personel/tls` mount kontrol |
| `SSL connection has been closed unexpectedly` | TLS 1.0/1.1 client | Client driver güncelle (Postgres 16 TLS 1.2+ ister) |
| `FATAL: no pg_hba.conf entry for host` | Client subnet 172.16.0.0/12 dışında | pg_hba.conf.tls'e yeni hostssl satırı ekle ve reload |
| postgres crashloop, log'da `Permission denied` `postgres.key` | Key dosyası 0600 değil veya postgres uid'e ait değil | `chown 999:999 + chmod 0600` |
| Healthcheck geçmiyor ama container `Up` | start_period bitmemiş | 30s bekle, sonra `docker inspect` ile check |

## KVKK Uyumluluk Notu

Postgres TLS bring-up SOC 2 CC6.7 (transmission encryption) için canlı
kanıttır. `infra/scripts/verify-audit-chain.sh` çalıştırıldıktan sonra ilk
audit checkpoint, postgres TLS aktif olduğunu evidence locker'a kaydeder
(Phase 3.0 collector A1.2 backup-run komşusu).

KVKK m.12 (veri güvenliği) açısından bu adım pilot sonrası zorunlu
operasyonel kontroldür ve kurum DPIA dosyasına eklenmelidir.

---

## Son kontrol — 2026-04-14 (Wave 9 Sprint 5)

- Runbook içeriği Faz 5 Wave 1 deploy öncesi AWAITING operator action
  olarak korunuyor. vm3'te `sslmode=disable` dev kısayolu hâlâ aktif.
- Bring-up sırasında `docker-compose.dev-override.yaml` disable edilmeli,
  `docker-compose.tls.yaml` dahil edilmeli (Adım 4 bkz.).
- İlgili diğer Wave 1 runbook'ları: `nats-prod-auth-migration.md`,
  `minio-worm-migration.md`, `all-services-tls-migration.md`,
  `secret-rotation.md`, `healthcheck-restoration.md`, `backup-restore.md`.
  Bring-up sıralaması: **vault → postgres-tls → all-services-tls →
  nats-auth → minio-worm → healthcheck-restore → secret-rotation →
  backup-automation**.
- Değişiklik yok; mevcut prosedür 2026-04-13 sürümünden aynen geçerli.
