# ClickHouse Kümesi — İki-Host Replikasyon Runbook'u

> **Sürüm**: 1.0 — Phase 5 Wave 2, Roadmap #44
> **Kapsam**: Personel Platform pilotunda ClickHouse'u iki fiziksel
> Ubuntu makineye (vm3 + vm5) yayan üretim öncesi replikasyon kümesi.

## 1. Mimari

```
              ┌─────────────────────────────┐
              │   Personel uygulamaları     │
              │   (api, gateway, enricher)  │
              │          — vm3 —            │
              └──────────────┬──────────────┘
                             │ yazma: clickhouse-01
                             ▼
  ┌──────────────────────────────────────────────────────┐
  │            vm3  (192.168.5.44)                       │
  │  ┌──────────────────┐      ┌───────────────────┐     │
  │  │  clickhouse-01   │◄────►│    keeper-01      │     │
  │  │  replica-01      │      │    server_id=1    │     │
  │  │  8123 / 9000     │      │    9181 / 9234    │     │
  │  │  9009 interserv. │      │    (lider tercih) │     │
  │  └────────┬─────────┘      └─────────┬─────────┘     │
  │           │ ReplicatedMergeTree      │ Raft          │
  │           │ interserver replication  │ consensus     │
  │           │ log (port 9009)          │               │
  └───────────┼──────────────────────────┼───────────────┘
              │                          │
              │      LAN (192.168.5.0/24)│
              │                          │
  ┌───────────┼──────────────────────────┼───────────────┐
  │           ▼                          ▼               │
  │  ┌──────────────────┐      ┌───────────────────┐     │
  │  │  clickhouse-02   │◄────►│    keeper-02      │     │
  │  │  replica-02      │      │    server_id=2    │     │
  │  │  8123 / 9000     │      │    9181 / 9234    │     │
  │  │  9009 interserv. │      │    (follower)     │     │
  │  └──────────────────┘      └───────────────────┘     │
  │            vm5  (192.168.5.32)                       │
  └──────────────────────────────────────────────────────┘
```

**Küme adı**: `personel_cluster`
**Topoloji**: 1 shard, 2 replika (hot-standby, okuma ölçeklemesi için)
**Yazma yolu**: Uygulamalar sadece `clickhouse-01`'e yazar.
ReplicatedMergeTree motoru interserver replication log üzerinden
`clickhouse-02`'ye aktarır. Distributed table fan-out KULLANILMAZ
(`internal_replication=true` olarak ayarlı).

## 2. Tek Host Staging Rig'i İle Karıştırmayın

Bu runbook **çoklu-host** rig'ini anlatır. Repoda ikinci bir dosya daha
vardır:

| Dosya | Amaç | Fiziksel dağılım |
|---|---|---|
| `infra/compose/docker-compose.replication.yaml` | Phase 1 exit kriteri #17 validation rig | İki replika tek vm3'te |
| `infra/compose/clickhouse/docker-compose.cluster-node1.yaml` + `docker-compose.cluster-node2.yaml` | Bu runbook — üretim öncesi gerçek iki-host kümesi | vm3 + vm5 |

**İki rig aynı anda ÇALIŞTIRILMAZ.** Aynı container adlarını ve bind-mount
volume yollarını kullanırlar, Docker çakışır. Çoklu-host rig'ine geçmeden
önce tek-host rig'ini teardown edin:

```bash
docker compose -f infra/compose/docker-compose.yaml \
               -f infra/compose/docker-compose.replication.yaml down
```

## 3. Keeper Quorum Uyarısı (ÜRETIM RISKI — DOKÜMANLAŞTIRILMIŞ)

Raft consensus'un temel kuralı: `N` katılımcı varsa majority için
`floor(N/2) + 1` oylama gerekir. **İki katılımcı = 2/2 çoğunluk = sıfır
hata toleransı.** Bu yüzden:

- `keeper-01` VEYA `keeper-02`'den biri düşerse, ReplicatedMergeTree
  **yazma operasyonları bloke olur** (ZooKeeper ACL alamaz). Okumalar
  çalışmaya devam eder çünkü mevcut replika lokal veriyi sunabilir.
- Network partition (vm3 ↔ vm5 arası LAN'in kesilmesi) da yazma blokajına
  yol açar — split-brain koruması.
- Bu durum Phase 1 pilotu için **kabul edilmiş bir risktir**. SOC 2
  observation window'u başlamadan önce üçüncü keeper katılımcısı
  eklenmelidir (aşağıdaki §9).

Bu pilotun SLA'sı: **tek node bakımı sırasında yazma kesintisine toleranslı
değildir**. Bakım pencereleri business-hours dışında planlanmalı.

## 4. Ön Koşullar

- [ ] vm3'te tek-node `personel-clickhouse` durduruldu
- [ ] vm3 ve vm5 aynı LAN'da (ping ve SSH key-based auth çalışıyor)
- [ ] Vault PKI up, `pki_int/server-cert` role tanımlı
- [ ] vm3'te `/etc/personel/tls/tenant_ca.crt` mevcut
- [ ] vm3'te `infra/scripts/clickhouse-cluster-bootstrap.sh` çalıştırıldı
      (TLS certler issue edildi, vm5 bundle'ı staged)
- [ ] vm5'te Docker 25+ kurulu, `kartal` kullanıcısı docker group üyesi
- [ ] vm5'te `/etc/hosts`:
      ```
      192.168.5.44  clickhouse-01  keeper-01
      192.168.5.32  clickhouse-02  keeper-02
      ```
- [ ] vm3'te `/etc/hosts` aynı iki satırı içeriyor (container `network_mode: host`)
- [ ] vm5'te `/opt/personel-cluster/` dizini bootstrap bundle'ı bekliyor
- [ ] vm5'te `/var/lib/personel/clickhouse-cluster/02/{data,logs,keeper}` dizinleri `chown kartal:kartal`
- [ ] `.env` (vm3) ve `.env` (vm5) aşağıdaki değişkenleri içeriyor:
      ```
      CH_CLUSTER_PASSWORD=<bootstrap script çıktısından>
      CH_INTERSERVER_PASSWORD_SHA256=<bootstrap script çıktısından>
      CLICKHOUSE_CLUSTER_DATA_DIR_01=/var/lib/personel/clickhouse-cluster/01   # vm3
      CLICKHOUSE_CLUSTER_DATA_DIR_02=/var/lib/personel/clickhouse-cluster/02   # vm5
      PERSONEL_TLS_DIR=/etc/personel/tls
      ```
- [ ] Mevcut ClickHouse şemalarının yedeği alındı:
      ```bash
      docker exec personel-clickhouse clickhouse-client \
          --query "SELECT table, create_table_query FROM system.tables \
                   WHERE database = 'personel'" > /tmp/pre-cluster-schemas.sql
      ```
- [ ] Tüm ClickHouse verisinin **tam yedeği** MinIO audit-worm bucket'ına
      alındı. Migration sırasında küçük bir data loss riski vardır
      (INSERT...SELECT yaklaşımı, canlı trafikte eşzamanlı insert'ler).
      Pilotta gateway → ClickHouse trafiği geçici olarak durdurulmalıdır.

## 5. Açılış Sırası

### Aşama A — vm3 (tek-node ClickHouse'u durdur)

```bash
cd /home/kartal/personel/infra/compose

# Gateway + enricher ClickHouse yazımını durdur
docker compose stop enricher gateway

# Tek-node ClickHouse'u durdur
docker compose stop clickhouse
```

### Aşama B — vm3 (bootstrap)

```bash
cd /home/kartal/personel

# TLS cert + secret generation
./infra/scripts/clickhouse-cluster-bootstrap.sh

# Bundle'ı vm5'e kopyala (script çıktısındaki scp komutlarını koş)
```

### Aşama C — vm3 (node-1 başlat)

```bash
cd /home/kartal/personel/infra/compose

docker compose \
    -f docker-compose.yaml \
    -f clickhouse/docker-compose.cluster-node1.yaml \
    up -d keeper-01 clickhouse-01

# Keeper hazır olana kadar bekle
docker logs -f personel-keeper-01 | grep -m1 'Ready for connections'
# Ctrl+C ile çık

# ClickHouse hazır olana kadar bekle
timeout 120 bash -c 'until docker exec personel-clickhouse-01 clickhouse-client -q "SELECT 1" >/dev/null 2>&1; do sleep 2; done'
```

### Aşama D — vm5 (node-2 başlat)

```bash
ssh kartal@192.168.5.32
cd /opt/personel-cluster

# .env oluştur (§4'deki değişkenlerle)
vi .env

docker compose -f docker-compose.cluster-node2.yaml up -d

# Keeper quorum form olmasını bekle (~10 saniye)
docker exec personel-keeper-02 bash -c 'echo stat | nc -w 2 127.0.0.1 9181'
# Çıktıda Mode: follower (veya leader) görmelisiniz
```

### Aşama E — vm3 (şema migration)

```bash
./infra/scripts/clickhouse-cluster-migrate-schemas.sh --dry-run   # önizleme
./infra/scripts/clickhouse-cluster-migrate-schemas.sh              # uygula
```

Script her tabloyu MergeTree'den ReplicatedMergeTree'ye çevirir:
- Yeni ReplicatedMergeTree tablosunu `<name>_replicated` olarak create eder
- Eski veriyi INSERT...SELECT ile aktarır
- Eski tabloyu `<name>_old` olarak rename eder, yeniyi `<name>` yapar
- Eski `<name>_old` tablosu manuel doğrulama sonrası elle DROP edilir

**Büyük tablolarda** (>100 GB) bu script uygun değildir. Sıfır-downtime
yol için `ALTER TABLE ... ATTACH PARTITION` yöntemini elle uygulayın —
aşağıdaki §7'ye bakın.

### Aşama F — vm3 (validation)

```bash
./infra/scripts/clickhouse-cluster-test.sh
```

Beklenen çıktı:
```
[PASS] clickhouse-01 responding
[PASS] clickhouse-02 responding
[PASS] personel_cluster has 2 replicas
[PASS] clickhouse-01 sees keeper (N root znodes)
[PASS] test table created on both replicas
[PASS] inserted marker (took X ms)
[PASS] row visible on clickhouse-02 after Y ms (1 s polling)
[PASS] ch-01 replication_queue empty
[PASS] ch-02 replication_queue empty
[PASS] ch-01 replicas is_readonly=0
[PASS] ch-02 replicas is_readonly=0
=== CLUSTER TEST: PASS ===
```

### Aşama G — vm3 (trafiği geri aç)

```bash
cd /home/kartal/personel/infra/compose
docker compose start enricher gateway

# Smoke: bir event pipeline'a yolla, ClickHouse'ta görünüyor mu
```

## 6. KVKK Notu — Replikasyon Gecikmesi = RPO

ClickHouse tüm davranışsal analitik event'leri saklar (events_raw,
events_sensitive_*, agent_heartbeats). Replikasyon gecikmesi,
`clickhouse-01` çöküşü durumunda **kaybedilecek maksimum veri** miktarını
temsil eder (RPO — recovery point objective).

- **Kabul edilebilir maks lag**: 60 saniye
- **Uyarı eşiği**: 10 saniye (Prometheus `clickhouse_replication_lag_seconds > 10`)
- **Kritik eşiği**: 60 saniye (paging + incident açma)

Lag Prometheus metrikleri `system.replicas.absolute_delay` üzerinden
elde edilir. KVKK saklama matrisi bakımından event'lerin en geç
60 saniye içinde her iki replikada da bulunması, `events_sensitive_*`
tablolarının 7 yıllık retention SLA'sını etkilemez (tek-node kaybında
dahi 60 saniyelik bir pencere RPO olarak dokümantedir).

## 7. Büyük Tablolar İçin Sıfır-Downtime Migration

Phase 1 veri hacmi küçüktür. 100 GB+ tablolar için aşağıdaki yolu elle
uygulayın:

```sql
-- 1. Yeni replicated tabloyu yaratın
CREATE TABLE personel.events_raw_new ON CLUSTER personel_cluster AS personel.events_raw
ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/events_raw', '{replica}')
PARTITION BY toYYYYMM(event_time)
ORDER BY (tenant_id, event_time, event_id);

-- 2. Her partition'ı ATTACH ile aktarın (satır-bazlı kopya yok)
ALTER TABLE personel.events_raw_new ATTACH PARTITION ID '202604' FROM personel.events_raw;
ALTER TABLE personel.events_raw_new ATTACH PARTITION ID '202603' FROM personel.events_raw;
-- ... tüm partition'lar için tekrarla

-- 3. Atomik rename
RENAME TABLE
    personel.events_raw     TO personel.events_raw_old,
    personel.events_raw_new TO personel.events_raw
ON CLUSTER personel_cluster;

-- 4. Doğrulamadan sonra eski tabloyu drop
DROP TABLE personel.events_raw_old ON CLUSTER personel_cluster SYNC;
```

## 8. Failover Prosedürü

### `clickhouse-01` (vm3) düşerse

- Okuma trafiği `clickhouse-02`'ye yönlendirilebilir — uygulama config'inde
  `CLICKHOUSE_HOST` değiştir, gateway + enricher + api restart et.
- **Ancak yazma**: keeper quorum kaybolduğu için ReplicatedMergeTree
  yazma reddi döner (`is_readonly=1`). 2-node keeper ile hata toleransı
  yoktur.
- Kısa bakım için vm3'ü hızla kaldır. Uzun kesinti için §9 acil durum
  prosedürü.

### `clickhouse-02` (vm5) düşerse

- Yazma ve okuma `clickhouse-01` üzerinden çalışmaya devam eder —
  keeper-01 Raft majority'yi yalnız tutamayacak olsa da ClickHouse
  `force_sync=true` ile lokal write-ahead log'a yazabilir. **HOWEVER
  bu durum da yazma blokajına yol açar**; test etmeden üretime almayın.
- vm5'i geri getir, `clickhouse-02` otomatik catch-up yapar. Gecikme
  `system.replication_queue` üzerinden izlenir.

## 9. Üçüncü Keeper Katılımcısı Ekleme (Üretim Öncesi Zorunlu)

Pilotu üretime çıkarmadan önce `keeper-03` bir üçüncü hosta eklenmeli.
Aday senaryolar:

1. **Ayrı keeper-only container** (önerilir): vm3 VEYA vm5 dışında
   ayrı bir küçük host (1 vCPU / 1 GB RAM yeterli), sadece
   `clickhouse-keeper` container'ı çalıştırır. Bu host asla ClickHouse
   data tutmaz — yalnızca Raft oylaması yapar.
2. **Co-hosted vm3 üzerinde ikinci keeper**: Split-brain korumasını
   bozar çünkü vm3 düştüğünde iki keeper birden kaybolur. **Kullanılmamalı.**
3. **vm4 (yeni host)**: vm4 Ubuntu provision edilir, sadece keeper-03.

Her senaryoda:
- `keeper-config-01.xml` ve `keeper-config-02.xml` dosyalarının
  `<raft_configuration>` bölümüne üçüncü `<server id="3"/>` eklenir.
- Yeni `keeper-config-03.xml` oluşturulur.
- `config.cluster.xml` içindeki `<zookeeper>` bölümüne üçüncü node eklenir.
- Tüm ClickHouse ve keeper container'ları sırayla restart edilir
  (önce keeper'lar, sonra CH).

Bu, Phase 6 backend hardening roadmap'inde planlanmıştır.

## 10. Tek-Node Moda Geri Dönüş (Rollback)

Kümeleme başarısız olursa veya pilot tek-node moda geri döndürülmek
istenirse:

```bash
# vm5: cluster node-2 durdur ve sil
ssh kartal@192.168.5.32 '
    cd /opt/personel-cluster &&
    docker compose -f docker-compose.cluster-node2.yaml down &&
    sudo rm -rf /var/lib/personel/clickhouse-cluster/02
'

# vm3: cluster node-1 durdur
cd /home/kartal/personel/infra/compose
docker compose \
    -f docker-compose.yaml \
    -f clickhouse/docker-compose.cluster-node1.yaml \
    stop clickhouse-01 keeper-01
docker compose \
    -f docker-compose.yaml \
    -f clickhouse/docker-compose.cluster-node1.yaml \
    rm -f clickhouse-01 keeper-01

# vm3: tek-node ClickHouse'u geri getir
docker compose -f docker-compose.yaml up -d clickhouse

# Şemaları restore et (MergeTree geri dönüşü)
# ReplicatedMergeTree verisi tek-node için READ'lenebilir ama yeni
# create'ler MergeTree olarak yapılmalı. Tek seferlik migration:
#
#   RENAME TABLE personel.events_raw TO personel.events_raw_repl;
#   CREATE TABLE personel.events_raw AS personel.events_raw_repl
#       ENGINE = MergeTree ORDER BY ... PARTITION BY ...;
#   INSERT INTO personel.events_raw SELECT * FROM personel.events_raw_repl;
#   DROP TABLE personel.events_raw_repl;
```

## 11. Health Check Komutları

```bash
# Keeper-01 durumu
docker exec personel-keeper-01 bash -c 'echo mntr | nc -w 2 127.0.0.1 9181'

# Cluster üyelik
docker exec personel-clickhouse-01 clickhouse-client \
    --query "SELECT * FROM system.clusters WHERE cluster = 'personel_cluster'"

# Replikasyon queue
docker exec personel-clickhouse-01 clickhouse-client \
    --query "SELECT database, table, type, num_tries, last_exception \
             FROM system.replication_queue \
             WHERE database = 'personel' \
             FORMAT Vertical"

# Replika durumu
docker exec personel-clickhouse-01 clickhouse-client \
    --query "SELECT database, table, is_readonly, is_leader, \
                    absolute_delay, queue_size, inserts_in_queue \
             FROM system.replicas \
             WHERE database = 'personel' \
             FORMAT Vertical"

# ZooKeeper path'leri
docker exec personel-clickhouse-01 clickhouse-client \
    --query "SELECT name, value FROM system.zookeeper WHERE path = '/clickhouse/tables/01'"
```

## 12. Bilinen Eksikler (Scaffold Listesi)

Bu runbook ve eşlik eden script'ler Phase 5 Wave 2 kapsamında
**scaffold** seviyesindedir. Live deploy için aşağıdakiler gerçek
ortamda doğrulanmalıdır:

- [ ] `clickhouse-cluster-bootstrap.sh` gerçek Vault PKI ile end-to-end
      koşmadı (sadece mantık hazır)
- [ ] `clickhouse-cluster-test.sh` vm3 + vm5 gerçek cluster'ına karşı
      çalıştırılmadı
- [ ] `clickhouse-cluster-migrate-schemas.sh` INSERT...SELECT path'i
      gerçek veri ile test edilmedi
- [ ] Keeper Raft konvergensiyonu gerçek LAN gecikmesi altında
      ölçülmedi (heartbeat 500 ms varsayılan doğru mu?)
- [ ] Prometheus alert'leri (replication_lag) yazılmadı
- [ ] Keeper `keeper-03` scaffold ekleme prosedürü test edilmedi
- [ ] vm5'te systemd unit'i yok — manuel `docker compose up` ile başlatılıyor
- [ ] Backup + restore drill (MinIO Object Lock'lu audit-worm bucket'ına
      cluster config dahil) yapılmadı
