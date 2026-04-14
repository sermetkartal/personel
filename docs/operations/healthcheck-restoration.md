# Healthcheck Restoration Runbook

> Roadmap #56 — Pilot bring-up sırasında dev kısayolu olarak `service_started`
> kullanıldı. Üretim ortamına geçmeden önce strict `service_healthy` modu
> aktif edilmeli. Bu runbook 5 dakikalık bir geçiş prosedürüdür.

## Arka plan

`infra/compose/docker-compose.dev.yaml` (dev override) bazı servislerin
`healthcheck` bloklarını `disable: true` yapıyor (api, console, keycloak)
çünkü dev image'ları curl/wget tooling'i barındırmıyor veya healthcheck
subcommand'ları bug'lı (apps/api/cmd/api/main.go içinde TODO bırakılmış).

Pilot Ubuntu sunucusunda (192.168.5.44) servisler şu an dev override altında
çalışıyor. Müşteri prod kurulumu için `docker-compose.healthcheck-override.yaml`
override'una geçilmeli.

## Geçiş

### 1. Dev override'u durdur (data kaybı YOK — bind-mount volume'ler korunur)

```bash
cd /opt/personel/infra/compose
docker compose -f docker-compose.yaml -f docker-compose.dev.yaml down
```

### 2. Healthcheck override ile yeniden başlat

```bash
docker compose \
  -f docker-compose.yaml \
  -f docker-compose.healthcheck-override.yaml \
  -f postgres/docker-compose.tls-override.yaml \
  up -d
```

Postgres TLS henüz aktif değilse `-f postgres/docker-compose.tls-override.yaml`
satırını çıkar (Roadmap #42 önce tamamlanmalı; bkz. `postgres-tls-migration.md`).

### 3. Tüm servisler healthy olana kadar bekle

```bash
watch -n 2 'docker compose ps --format "table {{.Service}}\t{{.Status}}"'
```

Beklenen sıra (start_period değerlerine göre):

| Adım | Servis | Süre |
|---|---|---|
| 1 | vault | <60s |
| 2 | postgres | <30s |
| 3 | clickhouse, nats, minio | <60s |
| 4 | opensearch | <90s |
| 5 | keycloak | <120s |
| 6 | gateway, enricher, api, dlp | <30s |
| 7 | console, portal | <30s |
| 8 | envoy | <15s |

Tüm satırlar `Up X minutes (healthy)` durumuna gelmeli. Herhangi biri
`(unhealthy)` olarak kalırsa:

```bash
docker compose logs --tail=50 <service>
docker inspect personel-<service> --format '{{json .State.Health}}' | jq
```

## Geri alma (Rollback)

Herhangi bir servis healthy olamıyor ve düzeltilmesi vakit alacaksa:

```bash
cd /opt/personel/infra/compose
docker compose -f docker-compose.yaml -f docker-compose.healthcheck-override.yaml down
docker compose -f docker-compose.yaml -f docker-compose.dev.yaml up -d
```

Volume'ler bind-mount olduğu için tüm DB içeriği korunur. Dev override
healthcheck'leri devre dışı bıraktığından stack tekrar açılır.

## Doğrulama (Smoke Test)

Healthcheck override aktifken:

```bash
# 1. Tüm servis healthy mi?
docker compose ps | grep -v healthy && echo "FAIL: unhealthy services" || echo "PASS"

# 2. API /healthz 200 mü?
curl -fsS http://127.0.0.1:8000/healthz | jq

# 3. Strict dependency ordering çalıştı mı?
#    (postgres TLS ile birlikte)
docker compose exec api psql \
  "host=postgres sslmode=verify-full sslrootcert=/etc/personel/tls/root_ca.crt" \
  -U app_admin_api -d personel -c "SELECT 1;"

# 4. Audit chain doğrulama
sudo /opt/personel/infra/scripts/verify-audit-chain.sh
```

## Bilinen tooling sorunları (gelecek polish)

| Servis | Sorun | Geçici çözüm | Kalıcı çözüm |
|---|---|---|---|
| keycloak | distroless image — curl/wget yok | bash `/dev/tcp` ile TCP probe | sidecar healthcheck container veya keycloak base image değişimi |
| api | `/personel-api healthcheck` subcommand main.go'da NO-OP | binary ile aynı port'a bağlanmaya çalışır → "address in use" | main.go'ya gerçek HTTP GET healthcheck subcommand ekle |
| gateway | aynı sorun | aynı | aynı |
| enricher | aynı sorun | aynı | aynı |

Yukarıdaki tablo CLAUDE.md §10 tech debt listesine eklenmeli — Roadmap
#39 (performance benchmark harness) ile birleştirilebilir.

## KVKK / SOC 2 etkisi

Strict healthcheck mode CC7.2 (system monitoring) ve A1.2 (availability)
kontrolleri için canlı kanıt sağlar:

- Prometheus `up{job=...}` metrikleri healthcheck durumunu yansıtır
- `personel-compose.service` systemd unit'i healthy olmayan servisi
  restart eder
- Backup script (Roadmap #58) çalıştırmadan önce tüm bağımlılıkların
  healthy olduğunu doğrular

Bu geçiş tamamlandıktan sonra `infra/runbooks/install.md` içindeki
"production checklist" bölümü güncellenmeli (`-f
docker-compose.healthcheck-override.yaml` flag'ı eklenmeli).

---

## Son kontrol — 2026-04-14 (Wave 9 Sprint 5)

- Runbook içeriği Wave 1 deploy kuyruğunda AWAITING operator action
  olarak korunuyor. vm3 hâlâ `docker-compose.dev-override.yaml` ile
  çalışıyor — tüm `service_healthy` deps'leri `service_started` olarak
  gevşetilmiş durumda.
- Geçişten önce `postgres-tls-migration.md` + `all-services-tls-migration.md`
  + `nats-prod-auth-migration.md` tamamlanmalı. Aksi halde strict
  healthcheck TLS handshake timing'i nedeniyle crash loop tetikler.
- Prosedür 2026-04-13 sürümünden değişmedi; 5 dakikalık bütçe hâlâ
  geçerli, healthcheck override compose `infra/compose/docker-compose.healthcheck-override.yaml`
  içinde.
