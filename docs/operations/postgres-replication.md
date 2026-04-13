# Postgres Streaming Replication — Operatör Runbook

**Roadmap**: Faz 5 Wave 2 — Madde #43 (streaming replication) + Madde #45'in
Postgres kısmı (replication test + failover drill).
**Dil**: Türkçe (DPO okuyabilsin).
**Hedef okur**: On-prem pilot operatörü + DPO.
**Mod**: Scaffolding — bu commit çalışan bir kurulum BIRAKMAZ. Operatör
`postgres-replica-bootstrap.sh` çalıştırana kadar hiçbir şey canlıya
çıkmıyor.

---

## 1. Mimari

```
       vm3 (192.168.5.44)                        vm5 (192.168.5.32)
       ┌────────────────────┐                    ┌────────────────────┐
       │ personel-postgres  │   WAL stream       │ personel-postgres- │
       │ (primary, RW)      │ ─────────────────► │ replica (hot       │
       │                    │   TLS verify-full  │ standby, RO)       │
       │ role: replicator   │                    │ pg_is_in_recovery()│
       │ port: 5432 (lo)    │                    │ = true             │
       └────────────────────┘                    └────────────────────┘
                │                                         │
                │ hostssl replication replicator          │
                │ 192.168.5.32/32 scram-sha-256           │
                │                                         │
                ▼                                         ▼
       ┌────────────────────┐                    ┌────────────────────┐
       │ WAL archive        │                    │ Read-only console  │
       │ /var/backups/...   │                    │ queries (DR panel) │
       │ (PITR + #58)       │                    │                    │
       └────────────────────┘                    └────────────────────┘
```

- **Replication modu**: asenkron (`synchronous_commit = local`).
  Birinciyi kaybedersek ~30 saniye / 10 MB kadar veri kaybı kabul edilebilir
  (RPO bütçesi, DPIA'ya eklenmiş). Senkron moda geçmek için
  `postgresql.conf.replication-primary` içinde
  `synchronous_commit = remote_apply` + `synchronous_standby_names =
  'personel-replica-vm5'` etkinleştirilmeli, **ama** replica erişilemez
  olunca birincideki tüm yazma işlemleri bloke olacaktır — bu trade-off'u
  müşteriyle netleştirmeden açılmaz.
- **TLS**: replica WAL stream'i `sslmode=verify-full` ile açar. Replica,
  Vault PKI'den çıkmış kendi server cert'ine + aynı root CA'ya sahiptir.
- **Şifreler**: replicator şifresi `/etc/personel/secrets/postgres-replicator-password`
  dosyasında yaşar, 0600 mod, asla commit edilmez.
- **WAL arşivi**: birinci `archive_command` ile `/var/backups/personel/pg/wal`
  dizinine yazmaya devam eder; replikasyon PITR'ı bozmaz, tersine
  birleşiktirler.

---

## 2. Ön koşullar

### vm3 (primary)

- [x] Faz 5 Wave 1 #42 — Postgres TLS override aktif (`docker-compose.tls-override.yaml`)
- [x] Vault PKI reachable (`https://127.0.0.1:8200`, token `pki/issue/server-cert` yetkili)
- [x] `/etc/personel/tls/postgres.crt` + `postgres.key` + `root_ca.crt` mevcut
- [x] `personel-postgres` container çalışıyor
- [x] `/etc/personel/secrets` dizini (0700 root:root)
- [x] `docker`, `vault`, `jq`, `openssl`, `psql` CLI'ları PATH'te

### vm5 (replica)

- [x] Ubuntu 24.04 + Docker 29.4 + Compose v5.1.2
- [x] vm3'ten ping ve 5432 TCP erişilebilir
- [x] `/etc/personel/tls` + `/etc/personel/secrets` + `/opt/personel/replica`
      dizinleri (bootstrap script'in handoff sırasında oluşturacak komutları var)
- [x] `/var/lib/personel/postgres-replica/data` dizini boş (mount hedefi)

---

## 3. Bring-up sırası

> **Önemli**: Aşağıdaki adımların her biri operatör tarafından manuel
> çalıştırılır. Script'lerin hiçbiri otomatik olarak iki makineye birden
> komut çalıştırmaz. Her adım idempotenttir — hata alırsan tekrar çalıştır.

### Adım 1 — Birincide replicator rolünü + cert'i hazırla

```bash
ssh kartal@192.168.5.44
cd /home/kartal/personel
sudo VAULT_TOKEN=hvs.xxxx ./infra/scripts/postgres-replica-bootstrap.sh --dry-run
# çıkışı incele, sonra gerçek çalıştırma:
sudo VAULT_TOKEN=hvs.xxxx ./infra/scripts/postgres-replica-bootstrap.sh
```

Script şunları yapar:
1. Pre-flight (container ayakta mı, vault erişilebilir mi, vm5 ping)
2. `/etc/personel/secrets/postgres-replicator-password` dosyasını 0600 mod
   ile yazar (varsa koruır, rotate için `--force-password`)
3. Birincide `CREATE ROLE replicator WITH LOGIN REPLICATION PASSWORD ...`
4. Vault PKI'den `CN=postgres-replica.personel.internal` + SAN IP `192.168.5.32`
   cert'ini üretir
5. `/tmp/personel-replica-bootstrap.<ts>/` altına şu bundle'ı hazırlar:
   - `docker-compose.replication-replica.yaml`
   - `postgresql.conf.replication-replica`
   - `pg-replica-init.sh`
   - `postgres-replica.crt`, `postgres-replica.key`, `root_ca.crt`
   - `postgres-replicator-password`
6. vm5'e kopyalanacak scp komutunu basar

### Adım 2 — Birincide replication compose overlay'ını aktif et

```bash
cd /home/kartal/personel/infra/compose
docker compose \
  -f docker-compose.yaml \
  -f postgres/docker-compose.tls-override.yaml \
  -f postgres/docker-compose.replication-primary.yaml \
  up -d postgres
```

Bu adım `postgresql.conf` ve `pg_hba.conf`'u değiştirir. Postgres
soft-reload yetmez, container restart gerekir (compose `up -d` bunu yapar).
Restart sırasında API + Gateway + Enricher kısa süre (~10 s) bağlantı
kesilmesi yaşar; üç servis de retry ile geri döner.

Doğrulama:

```bash
docker exec -i personel-postgres \
  psql -U postgres -d personel -c "SHOW wal_level"
# beklenen: replica

docker exec -i personel-postgres \
  psql -U postgres -d personel -c "SELECT usename FROM pg_roles WHERE rolname='replicator'"
# beklenen: replicator
```

### Adım 3 — Bundle'ı vm5'e taşı

Bootstrap script'i tam komutu basar, özet:

```bash
scp -r /tmp/personel-replica-bootstrap.<ts>/ kartal@192.168.5.32:/tmp/personel-replica-bundle
ssh kartal@192.168.5.32 'sudo bash -s' <<'REMOTE'
  set -euo pipefail
  install -d -m 0700 -o root -g root /etc/personel/secrets /etc/personel/tls /opt/personel/replica
  install -m 0600 -o root -g root /tmp/personel-replica-bundle/postgres-replicator-password /etc/personel/secrets/
  install -m 0644 -o root -g root /tmp/personel-replica-bundle/postgres-replica.crt /etc/personel/tls/
  install -m 0600 -o 999 -g 999 /tmp/personel-replica-bundle/postgres-replica.key /etc/personel/tls/
  install -m 0644 -o root -g root /tmp/personel-replica-bundle/root_ca.crt /etc/personel/tls/
  install -m 0755 -o root -g root /tmp/personel-replica-bundle/pg-replica-init.sh /opt/personel/replica/
  install -m 0644 -o root -g root /tmp/personel-replica-bundle/postgresql.conf.replication-replica /opt/personel/replica/
  install -m 0644 -o root -g root /tmp/personel-replica-bundle/docker-compose.replication-replica.yaml /opt/personel/replica/
  install -d -m 0700 -o 999 -g 999 /var/lib/personel/postgres-replica/data
  rm -rf /tmp/personel-replica-bundle
REMOTE
```

### Adım 4 — Replica container'ını ayağa kaldır

```bash
ssh kartal@192.168.5.32
cd /opt/personel/replica
docker compose -f docker-compose.replication-replica.yaml up -d
docker logs -f personel-postgres-replica
```

İlk başlangıçta `pg-replica-init.sh` boş `PGDATA`'yı tespit edip
`pg_basebackup`'ı başlatır. Database boyutuna göre 2-10 dakika sürer.
Log'da şu satırları görmelisin:

```
[pg-replica-init ...] PGDATA is empty — starting pg_basebackup from 192.168.5.44:5432
transfer completed
[pg-replica-init ...] pg_basebackup complete
[pg-replica-init ...] standby.signal + primary_conninfo written — handoff to postgres
[pg-replica-init ...] exec postgres -c config_file=/etc/postgresql/postgresql.conf
...
LOG:  database system is ready to accept read only connections
LOG:  started streaming WAL from primary at 0/xxxxxxx on timeline 1
```

---

## 4. Doğrulama

### Otomatik test

vm3'ten:

```bash
/home/kartal/personel/infra/scripts/postgres-replication-test.sh
```

Başarı kriterleri:
- `pg_stat_replication` birinci üzerinde replica'yı `streaming` state'inde
  görür
- replay lag < 10 MB
- replay lag < 30 saniye
- Marker satır birincide insert'ten sonra en fazla 5 saniye içinde
  replica'da görünür

### Manuel kontrol

Birinci:

```sql
SELECT application_name, client_addr, state, sync_state,
       pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn) AS replay_lag_bytes,
       replay_lag
FROM pg_stat_replication;
```

Replica (vm5'teki psql veya `ssh kartal@192.168.5.32 "docker exec -i
personel-postgres-replica psql -U postgres -d personel -c ..."`):

```sql
SELECT pg_is_in_recovery();         -- t
SELECT pg_last_wal_replay_lsn();    -- güncel LSN
SELECT now() - pg_last_xact_replay_timestamp();  -- replay yaşı
```

### Izleme

`pg_stat_replication` Prometheus exporter'ına Faz 13 #137 kapsamında
eklenecek. Şu an monitoring manuel — günde bir kez
`postgres-replication-test.sh` cron'a konabilir (opsiyonel).

---

## 5. Failover prosedürü (manuel, Faz 1)

> **Uyarı**: Faz 1'de otomatik failover yok. Split-brain riski en büyük
> tehlikedir — birincinin gerçekten öldüğünden EMIN olduktan sonra
> promotion başlat.

### 5.1 Birincinin gerçekten öldüğünü doğrula

```bash
ssh kartal@192.168.5.44 "systemctl status docker" || echo "vm3 unreachable"
ping -c 3 192.168.5.44
```

Eğer vm3 sadece network olarak erişilemezse fakat hala yazma alıyorsa,
replica'yı promote etmek split-brain yaratır. Network ayırma durumunda
müşteri DPO'suna danış.

### 5.2 Replica'yı promote et

```bash
ssh kartal@192.168.5.32
docker exec -i personel-postgres-replica pg_ctl promote -D /var/lib/postgresql/data
docker exec -i personel-postgres-replica psql -U postgres -c "SELECT pg_is_in_recovery()"
# beklenen: f
```

Promote sonrası replica yeni birinci olur, `standby.signal` dosyası
otomatik silinir. Artık RW bağlantıları kabul eder.

### 5.3 API + Gateway + Enricher'ı yeni birinciye yönlendir

`/home/kartal/personel/infra/compose/.env` içinde:

```
POSTGRES_HOST=192.168.5.32
```

değişkenini güncelle ve üç servisi yeniden başlat (vm3 hala çalışıyorsa
orada, yoksa vm5'e taşıma ayrı bir konudur — bu runbook dışı):

```bash
docker compose up -d api gateway enricher
```

### 5.4 Eski birinciyi karantinaya al

vm3 geri gelmişse, `docker stop personel-postgres` + `docker update
--restart=no personel-postgres` ile manuel olmadan açılmamasını garanti et.
Eski birincinin kendisini yeniden sahne alması split-brain'dir.

Yeniden birinci yapmak için (Phase 2 işi):
- Eski birincide `pg_rewind` ile yeni birinciye göre delta'yı eş
- Eski birincide `standby.signal` oluştur + yeni birinciyi
  `primary_conninfo` olarak yaz
- `postgres-replica-bootstrap.sh`'ın mantığının tersini manuel uygula

---

## 6. Geri alma (rollback)

Replikasyonu tamamen kaldırmak için (sadece birinci, DR standby yok):

### vm5

```bash
cd /opt/personel/replica
docker compose -f docker-compose.replication-replica.yaml down -v
# -v volume'u siler; postgres-replica-data kaybolur, yeniden bootstrap gerekecek
```

### vm3

```bash
cd /home/kartal/personel
sudo ./infra/scripts/postgres-replica-bootstrap.sh --teardown
# replicator rolünü drop eder; şifre arşivlenir
```

Sonra `docker-compose.replication-primary.yaml` overlay'ını compose
komutundan çıkar:

```bash
docker compose \
  -f docker-compose.yaml \
  -f postgres/docker-compose.tls-override.yaml \
  up -d postgres
```

`wal_level=replica` ayarı WAL arşivleme için zaten gereklidir, değişmez.
`max_wal_senders` ve `wal_keep_size` değerleri TLS-only override'da
olduğu şekliyle kalır.

---

## 7. KVKK notu — DPIA bağlantısı

Asenkron replikasyon, felaket durumunda ~30 saniye / 10 MB'a kadar veri
kaybı demektir. Bu kayıp:
- Audit log için: hash-chain son checkpoint'ten beri kaydedilen olaylar
  kaybolabilir. SOC 2 Type II evidence WORM bucket'ta aynı anda var;
  replikasyon tek başına audit chain'in bütünlüğü için yeterli değil.
- Olay verisi (events_raw) için: ClickHouse pipeline'ı ayrı şekilde
  replike edilmektedir (Madde #44); Postgres sadece metadata tutar.
- DSR (KVKK m.11) workflow için: 30-day SLA sayaçları en fazla 30 saniye
  geri gider — audit log'un içinde olduğu için tamamen kaybolmaz, ancak
  son 30 saniyedeki manuel admin müdahaleleri gözden geçirilmeli.

DPIA'ya "Replication RPO: 30 s asenkron, pilot kabulü" satırı eklenmelidir.
Müşteri bunu kabul etmediği takdirde senkron moda geçilmeli (yukarıda
trade-off).

---

## 8. Sorun giderme

| Belirti | Olası sebep | Düzeltme |
|---|---|---|
| `pg_basebackup: error: connection to server... FATAL: no pg_hba.conf entry for replication` | Birincide compose overlay uygulanmamış | Adım 2'yi tekrarla |
| `pg_basebackup: error: could not initiate base backup: ERROR: replication slot ... does not exist` | Slot kullanımına geçilmiş ama slot yok | Faz 1'de slot kullanmıyoruz; script'in `--wal-method=stream`'i yeterli. Eğer operatör slot etkinleştirdiyse birincide `SELECT pg_create_physical_replication_slot('personel_replica_vm5')` |
| `FATAL: could not connect to the primary server: ... certificate verify failed` | root CA yanlış veya cert CN uyuşmuyor | vm5'teki `/etc/personel/tls/root_ca.crt` birincideki ile aynı mı? Cert CN `postgres.personel.internal` mi? |
| `pg_is_in_recovery()` = f fakat beklenmedik | replica yanlışlıkla promote olmuş | Replica'yı durdur, `standby.signal` dosyasını elle oluştur + `postgres` tekrar başlat; veya temiz resync (`down -v` → `up -d`) |
| `replay_lag` sürekli artıyor | Ağ doygun veya birinci yazma hızı replay'i aşıyor | vm5 diskinin IOPS'ı yeterli mi? `iostat -xm 1` kontrol; gerekirse `wal_compression = on` olduğunu doğrula |
| `pg_stat_replication` boş | Replica birinciye bağlanamıyor | Birincide `docker logs personel-postgres 2>&1 \| grep -i replication` — `hostnossl ... reject` çıktısı varsa replica TLS ile bağlanmıyor demektir |

---

## 9. Dosya envanteri

Bu sprint'te oluşturulan dosyalar:

| Dosya | Konum | Kim kullanır |
|---|---|---|
| `postgresql.conf.replication-primary` | `infra/compose/postgres/` | vm3 postgres container |
| `pg_hba.conf.replication-primary` | `infra/compose/postgres/` | vm3 postgres container |
| `docker-compose.replication-primary.yaml` | `infra/compose/postgres/` | vm3 operatörü |
| `postgresql.conf.replication-replica` | `infra/compose/postgres/` | vm5 postgres container (scp ile) |
| `docker-compose.replication-replica.yaml` | `infra/compose/postgres/` | vm5 operatörü (scp ile) |
| `pg-replica-init.sh` | `infra/compose/postgres/` | vm5 container entrypoint |
| `postgres-replica-bootstrap.sh` | `infra/scripts/` | vm3 operatörü (root) |
| `postgres-replication-test.sh` | `infra/scripts/` | vm3 operatörü + cron (opsiyonel) |
| `postgres-replication.md` | `docs/operations/` | Operatör + DPO |

---

## 10. Sonraki iş (Faz 5 sonrası)

- [ ] #137 — `pg_stat_replication` Prometheus exporter + AlertManager rule
- [ ] Replication slot'a geçiş (Phase 2 hardening, split-brain risk balancing)
- [ ] Otomatik failover (pg_auto_failover veya Patroni) — Faz 2+ kararı
- [ ] Senkron replikasyon kararı (müşteri RPO gereksinimi netleştikten sonra)
- [ ] `pg_rewind` based re-promotion prosedürü (eski birinciyi DR standby olarak geri alma)
