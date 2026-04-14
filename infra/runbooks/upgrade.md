# Personel Platform — Sıfır Kesinti Yükseltme Prosedürü (Faz 13 #136)

> **Hedef**: Production Personel kurulumunu kesinti olmadan yeni bir sürüme
> yükseltmek. Rollback yolu her adımda hazır tutulur.
>
> **Okuma süresi**: 15 dakika. **Uygulama süresi**: 45-90 dakika (veri tier
> migration sayısına bağlı).

---

## 1. Ön hazırlık

### 1.1 Yedekleme (zorunlu)

```bash
sudo /opt/personel/infra/scripts/backup-orchestrator.sh --full
```

Yedek bitmeden devam ETME. Yedek bütünlüğünü doğrula:

```bash
sudo /opt/personel/infra/scripts/verify-audit-chain.sh --latest
```

### 1.2 Bakım bildirimi

- Şeffaflık portalına 24 saat önce banner yerleştir
  (`apps/portal/src/components/banner.tsx` feature flag).
- Grafana Alertmanager'da ilgili alarmları sessizleştir (1 saatlik silence).
- MSI güncellemelerini pause et (agent'lar rollout beklesin).

### 1.3 Baseline metrikler

Yükseltme öncesi şu metrikleri kaydet (rollback kararı için kritik):

```bash
# API p95 latency
curl -s http://localhost:9090/api/v1/query?query='histogram_quantile(0.95,rate(api_http_request_duration_seconds_bucket[5m]))'
# Event ingest rate
curl -s http://localhost:9090/api/v1/query?query='rate(gateway_events_ingested_total[1m])'
# NATS consumer lag
curl -s http://localhost:9090/api/v1/query?query='max(nats_stream_consumer_pending)'
# Endpoint online count
curl -s http://localhost:9090/api/v1/query?query='sum(personel_endpoints_online)'
```

Dosyaya yaz: `/var/log/personel/upgrade-baseline-$(date +%Y%m%d).txt`

### 1.4 Sürüm uyumluluk kontrolü

| Mevcut | Hedef | Breaking? | Notlar |
|---|---|---|---|
| 0.1.x | 0.2.y | Yok | Rolling upgrade güvenli |
| 0.2.x | 0.3.y | Migration 0030+ | pg-migration down path test edilmeli |
| 0.3.x | 1.0.y | Proto v1→v2 | Agent forced re-enrollment gerekebilir |

**Not**: `docs/architecture/version-compat-matrix.md` final authoritative kaynak.

---

## 2. Rolling upgrade sırası

Personel stack'i iki gruba ayrılır: **stateless** (frontends + API/gateway/enricher)
ve **stateful** (data tier). Stateless grubunda blue-green, stateful grubunda
replica-first strategy uygulanır.

### 2.1 Stateless tier — blue-green

Sıra (her adımdan sonra sağlık kapısı kontrol):

1. **Portal** → `portal`
2. **Console** → `console`
3. **API** → `api`
4. **Gateway** → `gateway`
5. **Enricher** → `enricher`

Her servis için pattern:

```bash
cd /opt/personel/infra/compose

# 1. Yeni imajı çek
docker compose pull ${SERVICE}

# 2. Eski tag'i rollback için pinle
OLD_IMAGE=$(docker inspect personel-${SERVICE} --format '{{.Image}}' | cut -d@ -f2)
echo "${OLD_IMAGE}" > /var/lib/personel/upgrade-rollback-${SERVICE}.txt

# 3. Yeni container'ı başlat (blue-green: docker compose up recreates)
docker compose up -d --no-deps ${SERVICE}

# 4. Sağlık kapısı — 60 saniye bekle
for i in $(seq 1 30); do
  if docker compose ps ${SERVICE} | grep -q healthy; then
    echo "${SERVICE} healthy"
    break
  fi
  sleep 2
done

# 5. Canlı trafik smoke — API için:
curl -sk -o /dev/null -w "%{http_code}\n" http://localhost:8000/healthz
```

**Sağlık kapısı başarısızsa** → rollback (bkz. §4).

### 2.2 Stateful tier — replica-first

Sıra:

1. **Postgres** (replica önce, primary failover sonra)
2. **ClickHouse** (distributed: shard-by-shard)
3. **OpenSearch** (rolling restart)
4. **NATS** (cluster member-by-member)
5. **MinIO** (erasure set member-by-member)
6. **Keycloak** (Infinispan cluster member rotation)

#### Postgres örneği

```bash
# 1. Replica önce
ssh kartal@192.168.5.32 'cd /opt/personel/infra/compose && docker compose pull postgres && docker compose up -d postgres'
# Replication lag sıfırlanana kadar bekle
sleep 30
docker exec personel-postgres psql -U postgres -tAc \
  "SELECT now()-pg_last_xact_replay_timestamp() FROM pg_stat_replication"

# 2. Primary failover — pg_ctl promote replica, stop primary
# (Detay: docs/operations/postgres-replication.md §5)

# 3. Migration uygula
cd /opt/personel/infra/compose
docker compose exec api /app/api migrate up

# 4. Sağlık kapısı
docker compose exec api /app/api migrate status
```

#### ClickHouse örneği

```bash
# Shard 1 önce (trafikten çekip geri ekle)
docker compose stop clickhouse-s1
docker compose pull clickhouse
docker compose up -d clickhouse-s1
# system.replicas tablosunda absolute_delay < 10 olana kadar bekle
docker exec personel-clickhouse-s2 clickhouse-client --query \
  "SELECT absolute_delay FROM system.replicas WHERE database='personel' LIMIT 1"
```

---

## 3. Migration uygulama (Postgres v30→v33 örneği)

```bash
# Mevcut versiyon
docker compose exec api /app/api migrate status
# → current version: 29, available: 30, 31, 32, 33

# DRY RUN (her migration önce)
docker compose exec api /app/api migrate up --dry-run

# UP
docker compose exec api /app/api migrate up

# İzleme
docker compose logs -f api | grep migration
```

**Geri sarma** (emergency only):

```bash
docker compose exec api /app/api migrate down 1
```

Geri sarma test edilmemiş bir migration için TEHLİKELİ — önce staging'de deney.

---

## 4. Rollback prosedürü

Her stateless servis için:

```bash
OLD_IMAGE=$(cat /var/lib/personel/upgrade-rollback-${SERVICE}.txt)
docker tag "${OLD_IMAGE}" personel-${SERVICE}:current
docker compose up -d --no-deps ${SERVICE}
```

Migration rollback (DİKKAT: veri kaybı olabilir):

```bash
docker compose exec api /app/api migrate down 1
```

**Stateful rollback** geçerli değil → yedekten restore şart:

```bash
sudo /opt/personel/infra/scripts/restore-orchestrator.sh --from-latest
```

---

## 5. Post-upgrade doğrulama

```bash
sudo /opt/personel/infra/scripts/post-install-validate.sh \
  --report=/var/log/personel/post-upgrade-$(date +%Y%m%d-%H%M).json
```

Kontrol listesi:

- [ ] Tüm servisler `healthy`
- [ ] Postgres migration version'u hedeflenen sürüme uygun
- [ ] API p95 latency baseline'ın %20'si içinde
- [ ] Event ingest rate düşmemiş
- [ ] NATS consumer lag baseline'a dönmüş
- [ ] Endpoint online count baseline'dan en fazla %5 düşük
- [ ] Audit chain latest segment valid
- [ ] Grafana dashboard'larda hata oranı yok
- [ ] Bakım bildirimi banner'ı kaldırıldı

---

## 6. Breaking change tespiti

Yükseltme öncesi:

```bash
diff -u \
  /opt/personel/releases/current/CHANGELOG.md \
  /opt/personel/releases/next/CHANGELOG.md \
  | grep -E '^\+.*(BREAKING|BC-BREAK|proto v2|migration 00[0-9]{2})'
```

`BREAKING` veya `migration` anahtar kelimeleri varsa:

1. Staging'de önce dene
2. Customer DPO'ya bildir (KVKK madde 10 güncelleme gerekebilir)
3. Agent recompile + re-enroll gerekecekse pilot rollout planla

---

## 7. Runtime hedefleri

| Faz | Maksimum kesinti |
|---|---|
| Stateless rolling | 0 saniye (blue-green) |
| Postgres failover | < 30 saniye |
| ClickHouse shard rotation | 0 saniye (replikalı) |
| MinIO member rotation | 0 saniye (erasure coding) |
| Toplam upgrade süresi | < 90 dakika |

Kesinti bu hedefleri aşarsa: incident açın, root cause analizi yapın, runbook'a
yeni adım ekleyin.

---

## 8. Hızlı komut referansı

```bash
# Önce: baseline + backup
sudo /opt/personel/infra/scripts/backup-orchestrator.sh --full

# Stateless tier rolling (5 servis)
for svc in portal console api gateway enricher; do
  docker compose pull "${svc}" && docker compose up -d --no-deps "${svc}"
done

# Post-check
sudo /opt/personel/infra/scripts/post-install-validate.sh
```
