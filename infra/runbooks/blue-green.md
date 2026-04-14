# Blue-Green Deployment Runbook

Faz 16 #174 — Zero-downtime deployment for Personel stateless tier.

## Kapsam

Bu runbook **yalnızca stateless servisleri** kapsar:

- `api` (admin API, Go)
- `gateway` (gRPC ingest, Go)
- `enricher` (NATS → ClickHouse/MinIO, Go)
- `console` (Next.js 15 admin UI)
- `portal` (Next.js 15 çalışan portalı)

Aşağıdaki stateful servisler **ASLA** blue-green değildir; onlar için
rolling upgrade ve manual maintenance window kullanılır (runbook:
`infra/runbooks/upgrade.md`, Faz 13 #136):

- `postgres` — tek kopya (replica Faz 5 #43 altında)
- `clickhouse` — 2-node cluster, birim-birim upgrade
- `nats` — JetStream cluster, birim-birim
- `minio` — distributed erasure, birim-birim
- `vault`, `keycloak`, `opensearch` — aynı prensip

## Mimari

```
         HTTPS 443 / gRPC 9443
                 │
                 ▼
      ┌──────────────────────┐
      │  bluegreen-router    │  nginx 1.27
      │  active-color.conf   │  (SIGHUP reload, no restart)
      └──────────┬───────────┘
                 │
         ┌───────┴────────┐
         │                │
         ▼                ▼
    ┌─────────┐      ┌──────────┐
    │  BLUE   │      │  GREEN   │
    │ api     │      │ api      │
    │ gateway │      │ gateway  │
    │ console │      │ console  │
    │ portal  │      │ portal   │
    │ enricher│      │ enricher │
    └────┬────┘      └────┬─────┘
         │                │
         └───────┬────────┘
                 ▼
          ┌──────────────┐
          │  SHARED      │
          │  postgres    │
          │  clickhouse  │
          │  minio       │
          │  nats        │
          │  vault       │
          │  keycloak    │
          └──────────────┘
```

İki renk de **aynı** postgres'e, aynı vault'a, aynı clickhouse'a bağlanır.
Bu yüzden migration **öncelikle** forward-compatible olmalıdır (yeni kod
eski şemayı okuyabilmeli, eski kod yeni şemayı okuyabilmeli). Backward-
breaking migration için ya `expand-contract` iki aşamalı deploy, ya
maintenance window kullan.

## Renkler

| Dosya | Anlam |
|---|---|
| `docker-compose.yaml` | Mavi (blue) — baseline pilot stack |
| `docker-compose.blue-green.yaml` | Overlay — yeşil (green) kopyaları + router |
| `infra/compose/bluegreen/active-color.conf` | Router'ın şu an okuduğu hedef upstream |
| `infra/compose/bluegreen/nginx.conf` | Statik router config (upstream include'ları) |
| `infra/scripts/blue-green-switch.sh` | Atomik switch aracı |

## Switch prosedürü

### 0) Ön koşullar

- Şu an blue çalışıyor (pilot state)
- Yeşil image'lar `ghcr.io/.../personel-*:v1.X.Y` tag'i ile publish edilmiş
  ve `infra/ci-scaffolds/image-verify.yml` geçmiş (cosign verified)
- Migration 0034 veya sonrası varsa `forward_compatible: true` bayrağı
  commit mesajında görünüyor

### 1) Green'i ayağa kaldır

```bash
cd infra/compose
export PERSONEL_IMAGE_TAG=v1.5.0
docker compose \
  -f docker-compose.yaml \
  -f docker-compose.blue-green.yaml \
  --profile blue-green \
  up -d api-green gateway-green enricher-green console-green portal-green
```

### 2) Green'i smoke test et

```bash
# Her stateless servisin distinct portunu kullanıyor
curl -fk http://127.0.0.1:18001/healthz       # api-green
curl -fk http://127.0.0.1:13000/healthz       # console-green
curl -fk http://127.0.0.1:13001/healthz       # portal-green
# gRPC için:
grpcurl -plaintext 127.0.0.1:19443 grpc.health.v1.Health/Check
```

**Tüm testler yeşil olmadan asla switch atma.** Red ise `docker logs`
bak, blue yine üretimi taşıyor.

### 3) Switch komutu (atomik)

```bash
sudo infra/scripts/blue-green-switch.sh green
```

Bu script:
1. Green'in sağlığını yeniden doğrular
2. Aktif bir DSR SLA penceresinde işlem yok mu kontrol eder
3. `SWITCH green` teyit tokenını ister
4. `active-color.conf`'u yeniden yazar (atomik: `.new` → `mv -f`)
5. `personel-bluegreen-router`'a `SIGHUP` yollar (container restart YOK)
6. Post-switch smoke test koşturur
7. `/var/log/personel/bluegreen.log`'a satır yazar

### 4) Green'i izle (15 dk)

- Prometheus: `personel_api_requests_total{color="green"}` yükseliyor mu
- Grafana → "Personel / Blue-green" paneli
- `docker logs -f personel-api-green`
- `/v1/audit/stream` WebSocket'i açık tut — anomaly varsa hemen görünür

### 5) Blue'yi durdurma (yalnızca 24 saat stabil ise)

24 saat geçtikten sonra:

```bash
docker compose \
  -f docker-compose.yaml \
  -f docker-compose.blue-green.yaml \
  --profile blue-green \
  stop api gateway enricher console portal
```

Bu blue container'larını durdurur; image hâlâ çekilmiş durumdadır — gerekirse
rollback (§6) anında ayağa kalkabilir.

## Rollback

Green sorun çıkarırsa **tek komutla** blue'ya dön:

```bash
sudo infra/scripts/blue-green-switch.sh blue
```

Eğer blue container'ları daha önce durdurulduysa önce tekrar `up -d`:

```bash
docker compose -f docker-compose.yaml up -d api gateway enricher console portal
sudo infra/scripts/blue-green-switch.sh blue
```

## Dikkat edilecek noktalar

### Enricher duplication

Enricher aynı NATS stream'inden okuyan bir consumer. İki kopya **aynı**
durable ile koşarsa NATS mesajları iki kez tüketilir. Overlay'de
green'e `ENRICHER_DURABLE=enricher-green` verilir, böylece her renk
kendi cursor'ını taşır. Switch sonrası operatör blue durable'ını
**drain** etmek için NATS CLI kullanmalıdır:

```bash
docker exec personel-nats nats consumer rm events_raw enricher-blue
```

Durmazsa blue durable'ı her yeni event'i ClickHouse'a ikinci kez yazar.

### Migration kuralı

Blue-green deploy, **forward-compatible migration** şartına bağlıdır.

| Migration türü | Blue-green uygun mu? |
|---|---|
| Yeni tablo / yeni kolon (nullable) | ✅ evet |
| Yeni index | ✅ evet |
| Kolon tip genişletme (INT → BIGINT) | ⚠️ DBA + test |
| Kolon silme | ❌ hayır — iki deploy (expand-contract) |
| Kolon yeniden adlandırma | ❌ hayır — iki deploy |
| Constraint ekleme (NOT NULL) | ❌ hayır — önce backfill |

Breaking migration için `infra/runbooks/upgrade.md` maintenance window
prosedürünü izle.

### Certificate/secret

TLS sertifikaları router'da terminates — her iki renk arkasındaki
servisler aynı içsel mTLS sertifikasını paylaşır (Vault PKI). Renk
değişikliği sertifika rotation gerektirmez.

### Vault AppRole secret ID

Blue ve green aynı AppRole'ü kullanır. AppRole secret ID'nin süresi
dolduğunda iki renk de etkilenir — rotasyon `infra/scripts/rotate-secrets.sh`
(Faz 5 #55) ile yapılır, blue-green'den bağımsızdır.

## Test

Pilot öncesi staging'de şu testleri koş:

1. Green'i ayağa kaldır, smoke test geç
2. Switch → green, `curl -fk https://host/v1/healthz` geçiyor mu
3. 5 dakika trafik izle, metric yeşil mi
4. Switch → blue (rollback tatbikatı)
5. NATS durable'larını say: `enricher-blue` ve `enricher-green` ayrı mı

Bu testin sonucu `docs/operations/phase-1-exit-criteria.md` #ZeroDowntimeDeploy
maddesine bağlanır.

## Sorun giderme

| Belirti | Muhtemel sebep | Çözüm |
|---|---|---|
| `nginx: [emerg] upstream not found` | active-color.conf yanlış yazılmış | Script çıktısını kontrol, elle düzelt + `docker kill -s HUP` |
| Post-switch smoke 502 | green container ready değil | `docker logs personel-api-green`, hazır olmadıysa blue'ya dön |
| NATS duplicate events | drain edilmemiş blue enricher durable | `nats consumer rm events_raw enricher-blue` |
| DSR SLA breach | Switch 30 gün penceresinde gecikmeye neden | Script `--force` olmadan bloklar; manuel DPO onayı gerekli |

## Değişiklik kaydı

| Tarih | Değişiklik | Yazan |
|---|---|---|
| 2026-04-13 | İlk versiyon — Faz 16 #174 | devops-engineer |
