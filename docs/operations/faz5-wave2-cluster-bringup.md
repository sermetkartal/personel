# Faz 5 Wave 2 — Cluster Bring-up Orchestrator Runbook

> **Kapsam**: Faz 5 Wave 2 cluster scaffold'larının (Postgres replica,
> ClickHouse 2-node, NATS cluster, MinIO distributed, OpenSearch cluster,
> Keycloak HA) vm3 primary + vm5 secondary + 3. node TBD üzerinde sıralı
> deploy'u.
> **Sahip**: SRE / DevOps — DPO sign-off zorunlu
> **Süre**: 6 adım x 20-45 dk = 3-4 saat (kesintisiz)
> **Mod**: Orchestrator — her adımın detay runbook'u bu belgede referans
> verilir, burada sadece sıra, önkoşul zinciri ve verification top-level'ı
> tutulur.

---

## 0. Makine Envanteri

| Rol | Host | IP | Notlar |
|---|---|---|---|
| Primary | vm3 | 192.168.5.44 | 12 servis canlı, dev ortamı |
| Secondary | vm5 | 192.168.5.32 | Ubuntu 24.04, 7.7 GB RAM, 4 vCPU, 87 GB disk, Docker 29.4 + Compose 5.1.2 |
| 3. node | **TBD** | — | Operatör tarafından sağlanacak; NATS quorum + MinIO erasure code için gerekli |

**Güvenlik kuralı**: CLAUDE.md §0'daki 3-makine limitine kesinlikle
sadık kalın. 3. node gelmeden NATS quorum 2-node'a düşürülmemeli (split
brain riski), MinIO distributed 4-disk minimuma düşürülmemeli (erasure
code).

## 1. Önkoşul Zinciri

Wave 2 bring-up'ı başlatmadan önce Wave 1 **tamamen bitmiş** olmalı:

| Wave 1 runbook | Durum gerekli |
|---|---|
| `vault-prod-migration.md` | Vault prod unseal + PKI engine aktif |
| `postgres-tls-migration.md` | `sslmode=verify-full` |
| `all-services-tls-migration.md` | 18 servisin hepsi Vault PKI cert |
| `nats-prod-auth-migration.md` | Operator JWT + NKeys |
| `minio-worm-migration.md` | audit-worm + evidence-worm COMPLIANCE |
| `secret-rotation.md` | timer aktif |
| `healthcheck-restoration.md` | strict `service_healthy` |
| `backup-restore.md` | nightly backup çalışıyor |

**STOP kuralı**: Yukarıdaki runbook'lardan herhangi biri `AWAITING` ise
Wave 2 başlatılamaz. Wave 1 deploy sırası için bkz. CLAUDE.md §0
"Faz 5 Wave 1 operator handoff".

## 2. Adım 1 — Postgres Streaming Replication (vm3 → vm5)

**Detay runbook**: `docs/operations/postgres-replication.md`

Kısa özet:

1. vm3 primary'de `postgresql.conf.replication-primary` uygula:
   - `wal_level = replica`
   - `max_wal_senders = 10`
   - `wal_keep_size = 1GB`
   - `archive_mode = on`, `archive_command = '/opt/personel/bin/wal-archive.sh %p %f'`
2. `pg_hba.conf`'ta replicator rolu için `hostssl replication replicator
   192.168.5.32/32 scram-sha-256` kaydı ekle.
3. vm3'te `CREATE ROLE replicator WITH REPLICATION LOGIN PASSWORD ...`
   (parola `Vault kv/personel/postgres/replicator/password`).
4. vm3 primary restart, `pg_is_in_recovery()` → `false`.
5. vm5'te `infra/scripts/postgres-replica-bootstrap.sh` çalıştır — bu
   `pg_basebackup -h 192.168.5.44 -U replicator -D /var/lib/personel/postgres
   -X stream -P` koşar ve standby.signal + recovery config yazar.
6. vm5 postgres container'ını start et → `pg_is_in_recovery()` → `true`.

### Network + firewall

- vm3 → vm5 outbound 5432/tcp açık
- vm5 → vm3 outbound 5432/tcp açık (WAL shipping geri tarafı için)
- Her iki yön Vault TLS cert'i aynı root CA altında olmalı

### Doğrulama

```bash
# vm3 primary
docker exec personel-postgres psql -U postgres -c \
  "SELECT application_name, state, sync_state, write_lag
   FROM pg_stat_replication"
# Beklenen: 1 satır, state=streaming, sync_state=async

# vm5 replica
docker exec personel-postgres-replica psql -U postgres -c \
  "SELECT pg_is_in_recovery(), now() - pg_last_xact_replay_timestamp()"
# Beklenen: t, < 1s
```

## 3. Adım 2 — ClickHouse 2-node + Keeper Cluster

**Detay runbook**: `docs/operations/clickhouse-cluster.md`

Kısa özet:

1. vm3 + vm5'te `clickhouse-keeper` container'larını start et (3-node
   quorum için 3. node TBD; ara çözüm: her makinada 2 keeper = tehlikeli
   split brain, EN AZINDAN 3. node gelene kadar `replication=1` modda
   kal).
2. `events`, `keystroke`, `dsr` tablolarını `ReplicatedMergeTree` motoruna
   çevir:
   ```sql
   -- Primary'de yeni tablo, eski tablodan INSERT
   CREATE TABLE personel.events_replicated AS personel.events
   ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/events', '{replica}')
   PARTITION BY toYYYYMM(timestamp)
   ORDER BY (tenant_id, timestamp, endpoint_id);

   INSERT INTO personel.events_replicated SELECT * FROM personel.events;
   RENAME TABLE personel.events TO personel.events_local_only,
                personel.events_replicated TO personel.events;
   ```
3. vm5'te aynı şemayı `DROP ... CREATE ... AS SELECT FROM clickhouse('vm3', ...)`
   ile bootstrap et (veya Keeper zookeeper-shim üzerinden auto-replicate).
4. Enricher `clickhouse_addr` config'ini her iki node'a round-robin
   `vm3:9000,vm5:9000` olarak güncelle.

### Network + firewall

- 9000/tcp (TCP native) + 8123/tcp (HTTP) + 9009/tcp (inter-server)
  + 9181/tcp (Keeper client) + 9234/tcp (Keeper raft) — tüm vm3↔vm5 yön
- TLS 9440/tcp aktif olmalı (Wave 1'in all-services-tls çıktısı)

### Doğrulama

```bash
docker exec personel-clickhouse clickhouse-client --query "
  SELECT host_name, replica_is_active FROM system.replicas
  WHERE database='personel'
"
# Beklenen: her iki host aktif

docker exec personel-clickhouse clickhouse-client --query "
  SELECT count() FROM personel.events
"
# vm3 ve vm5'te aynı count beklenir (async replication, ~2s gecikme)
```

## 4. Adım 3 — NATS Cluster (2-node başlangıç, 3-node hedef)

**Detay runbook**: `docs/operations/nats-minio-cluster.md` (NATS bölümü)

**KRİTİK**: 2-node NATS quorum YOKTUR. `replication=2` JetStream
stream'leri async replicate eder ama leader election split brain
yaşayabilir. 3. node gelene kadar üretimden geçme.

Kısa özet:

1. vm3 + vm5'te `nats-server.conf.cluster` uygula:
   ```
   cluster {
     name: personel
     listen: 0.0.0.0:6222
     routes: [
       nats-route://vm3:6222
       nats-route://vm5:6222
     ]
   }
   jetstream {
     store_dir: /var/lib/nats
     max_mem: 256MB
     max_file: 20GB
   }
   ```
2. Mevcut stream'leri `replication=2` modda yeniden oluştur:
   ```bash
   nats stream update events_raw --replicas=2
   nats stream update events_sensitive --replicas=2
   nats stream update live_view_control --replicas=2
   nats stream update agent_health --replicas=2
   nats stream update pki_events --replicas=2
   ```
3. JetStream at-rest encryption Wave 1'den aktif olmalı; cluster'daki
   key aynı key olmalı.

### Network + firewall

- 4222/tcp (client) + 6222/tcp (cluster routes) + 8222/tcp (monitoring)
  vm3↔vm5 açık
- TLS route (server-to-server) `tls { cert_file, key_file, ca_file }`
  bölümü ile etkin olmalı (Wave 1 all-services-tls)

### Doğrulama

```bash
docker exec personel-nats nats server list
# Beklenen: 2 server, leader + follower
docker exec personel-nats nats stream info events_raw
# Beklenen: replicas=2, cluster leader görünür
```

## 5. Adım 4 — MinIO Distributed (4-disk minimum)

**Detay runbook**: `docs/operations/nats-minio-cluster.md` (MinIO bölümü)

**KRİTİK**: MinIO distributed mode en az 4 disk ister. 2 node x 2 disk
(bind mount) = 4 disk minimum. Erasure code `EC:2` (2 data + 2 parity)
— 1 node kaybına dayanır. 3. node gelince `EC:3`'e geçiş için tüm data
re-stripe gerekir (offline operation, ~2 saat/TB).

Kısa özet:

1. vm3'te 2 mount point hazırla: `/var/lib/personel/minio/data1`,
   `/var/lib/personel/minio/data2`. vm5'te aynı.
2. Compose'a distributed args ekle:
   ```yaml
   minio:
     command: >
       server
       http://vm3/var/lib/personel/minio/data{1...2}
       http://vm5/var/lib/personel/minio/data{1...2}
   ```
3. Mevcut verileri `mc mirror` ile yeni cluster'a taşı:
   ```bash
   mc alias set old http://vm3:9000 <old-root> <old-pass>
   mc alias set new http://vm3:9000 <new-root> <new-pass>
   mc mirror --preserve old/personel-screenshots new/personel-screenshots
   # audit-worm ve evidence-worm için TEK YÖNLÜ mirror — WORM nedeniyle
   # yeniden PUT edilebilir değil; önce HEAD compare ile eksikleri bul
   ```
4. WORM bucket'lar: `mc retention set --default compliance 1826d`
   yeniden uygula (cluster metadatası default olarak WORM'u taşımaz).

### Network + firewall

- 9000/tcp (client) + 9001/tcp (console) vm3↔vm5 açık
- TLS `cert.pem + private.key` her node'da aynı (Vault PKI cert)
- Bucket replication endpoint `https://vm5:9000` vm3'ten erişilebilir

### Doğrulama

```bash
docker exec personel-minio mc admin info personel
# Beklenen: 2 Drives Online, Erasure Set 1, EC:2
docker exec personel-minio mc ls personel/audit-worm | head
# Beklenen: boş değil
```

## 6. Adım 5 — OpenSearch 2-node Cluster

**Detay runbook**: `docs/operations/opensearch-keycloak-cluster.md`
(OpenSearch bölümü)

Kısa özet:

1. vm3 + vm5'te `opensearch.yml.cluster` uygula:
   ```yaml
   cluster.name: personel
   node.name: ${HOSTNAME}
   network.host: 0.0.0.0
   discovery.seed_hosts:
     - vm3
     - vm5
   cluster.initial_master_nodes:
     - vm3
     - vm5
   plugins.security.ssl.transport.pemcert_filepath: certs/node.pem
   plugins.security.ssl.transport.pemkey_filepath: certs/node.key
   ```
2. 2-node cluster split brain riskli; vm3'ü `node.master: true
   node.voting_only: false`, vm5'i `node.master: true node.voting_only:
   true` olarak ayarla → vm5 master olamaz ama quorum'a dahil.
3. Audit log index'ini `number_of_replicas: 1` yap:
   ```bash
   curl -X PUT "https://vm3:9200/audit/_settings" \
     -H "Content-Type: application/json" \
     -d '{"index": {"number_of_replicas": 1}}'
   ```

### Network + firewall

- 9200/tcp (client) + 9300/tcp (transport) vm3↔vm5 açık
- Transport layer TLS (internal) + HTTP layer TLS — iki farklı cert

### Doğrulama

```bash
curl -sk https://vm3:9200/_cluster/health | jq
# Beklenen: status=green, number_of_nodes=2, active_shards_percent=100
```

## 7. Adım 6 — Keycloak HA + Infinispan

**Detay runbook**: `docs/operations/opensearch-keycloak-cluster.md`
(Keycloak bölümü)

Kısa özet:

1. vm3 + vm5'te Infinispan distributed cache konfigürasyonu:
   ```xml
   <!-- cache-ispn-jdbc-ping.xml (zaten repo'da var) -->
   <distributed-cache name="sessions" owners="2"/>
   <distributed-cache name="authenticationSessions" owners="2"/>
   ```
2. JDBC_PING ile Postgres tabanlı discovery — Adım 1'in replica'sını
   read-only kullanır:
   ```
   KC_CACHE=ispn
   KC_CACHE_STACK=jdbc-ping
   JDBC_PING_URL=jdbc:postgresql://vm3:5432/keycloak?sslmode=verify-full
   ```
3. Nginx/HAProxy sticky session (source-ip hash) vm3:8080 + vm5:8080
   önüne kur.
4. Realm export/import: **mevcut `personel-console` mapper'ları dahil
   olmalı**. CLAUDE.md §0'daki KC24→KC25 upgrade notunda kaybolan iki
   mapper (audience + tenant_id user-attribute) bu commit'te realm
   JSON'a yazıldı (Task 5). Fresh deploy'da `kcadm.sh` ek komut
   gerekmez.

### Network + firewall

- 8080/tcp (HTTP) + 7800/tcp (JGroups) + 57800/tcp (JGroups FD_SOCK)
  vm3↔vm5 açık
- Sticky session loadbalancer dış portu (443/tcp)

### Doğrulama

```bash
# Her iki node'a ayrı ayrı login dene
curl -s "http://vm3:8080/realms/personel/.well-known/openid-configuration" \
  | jq .issuer
curl -s "http://vm5:8080/realms/personel/.well-known/openid-configuration" \
  | jq .issuer
# Her ikisi de aynı issuer (loadbalancer hostname) döndürmeli

# Infinispan replica durumu
docker exec personel-keycloak-1 /opt/keycloak/bin/kc.sh show-config \
  | grep cache
```

## 8. Cutover + Rollback Planı

### 8.1 Sıralı cutover disiplini

Her adımı tamamladıktan sonra **bir sonrakine geçmeden önce** 15 dakika
smoke window açın ve şu testleri koşun:

1. `infra/scripts/final-smoke-test.sh` (full stack)
2. Agent enroll + event publish testi (Windows VM 192.168.5.30'dan)
3. Console giriş + dashboard
4. DSR create + respond

Fail varsa o adımı rollback et, sonraki adıma geçme.

### 8.2 Rollback matrisi

| Adım | Rollback aksiyon | Veri kaybı |
|---|---|---|
| 1 Postgres | vm5 container durdur, `recovery.signal` kaldır; vm3 `wal_level=minimal` | Yok (replica sadece read) |
| 2 ClickHouse | `RENAME TABLE events TO events_replicated, events_local_only TO events` | Wave 2 boyunca gelen yeni event'lar kaybolabilir |
| 3 NATS | `nats stream update events_raw --replicas=1` | Replikasyon lag varsa follower'daki yeni mesajlar kayıp |
| 4 MinIO | `mc mirror` geri yön, sonra `server /var/lib/personel/minio/data` single mode | Zorlu — erasure code undo imkansız, önce tüm veri tek node'a kopyalanmalı |
| 5 OpenSearch | `number_of_replicas: 0` + vm5 node'u çıkar | Yok (index primary vm3'te kalıyor) |
| 6 Keycloak | Sticky LB ardından vm5 node'u durdur, `KC_CACHE=local` | Session'lar düşer, kullanıcılar yeniden login |

### 8.3 KVKK + Audit kayıt

Her adım için evidence record:

```bash
curl -X POST http://vm3:8000/v1/system/change-records \
  -H "Authorization: Bearer ${DPO_JWT}" \
  -d '{
    "action": "faz5_wave2_adim_<N>_applied",
    "runbook_sha": "'"$(git rev-parse HEAD)"'",
    "operator": "<kartal>",
    "approver": "<dpo>",
    "notes": "<kısa açıklama>"
  }'
```

## 9. AWAITING

- [ ] 3. node sağlanması (NATS 3-node quorum + MinIO EC:3 için gerekli)
- [ ] Load balancer (Nginx veya HAProxy) vm3 + vm5 önüne
- [ ] DNS: `postgres.personel.internal`, `clickhouse.personel.internal`,
      `nats.personel.internal`, `minio.personel.internal` → VIP
- [ ] DPO sign-off her adım için ayrı evidence record

---

*Versiyon 1.0 — 2026-04-14 — Wave 9 Sprint 5 teslimatı.*
*Ara çözüm: 2-node scenario'da çalışma. 3. node gelene kadar üretim
trafiği bu cluster'a geçirilmemeli; dev + staging OK.*
