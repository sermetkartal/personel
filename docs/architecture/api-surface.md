# Personel API Surface — Single-Page Reference

> Dil: Karma (endpoint isimleri EN, açıklama TR). Hedef okuyucu: mimari inceleme, RBAC matrix review, yeni mühendis onboarding.
>
> Bu doküman Faz 1-9 sonrası **tüm** Admin API endpoint'lerini tek sayfada listeler. Kaynak: `apps/api/api/openapi.yaml`. OpenAPI spec ile bu doküman her zaman senkron olmalıdır (her yeni endpoint → her ikisi güncellenir).

## Roller

| Rol Kısaltması | Açılım |
|---|---|
| **A** | admin |
| **D** | dpo |
| **H** | hr |
| **M** | manager |
| **I** | investigator |
| **AU** | auditor |
| **E** | employee |
| **S** | service (API key / AppRole) |
| **PUB** | unauthenticated (public) |

## Endpoint Tablosu

### Sağlık

| Method | Path | Rol | Amaç |
|---|---|---|---|
| GET | `/healthz` | PUB | Liveness kontrolü |
| GET | `/readyz` | PUB | Readiness kontrolü |
| GET | `/metrics` | PUB* | Prometheus scrape (internal network only) |

### Kimlik ve Kullanıcı (Faz 1 + 6)

| Method | Path | Rol | Amaç |
|---|---|---|---|
| GET | `/v1/me` | A/D/H/M/I/AU/E | Mevcut kullanıcı profili |
| GET | `/v1/users` | A/D | Kullanıcı listesi |
| POST | `/v1/users` | A | Yeni kullanıcı (Keycloak üstünden) |
| PATCH | `/v1/users/{id}` | A/D | Kullanıcı güncelleme |
| DELETE | `/v1/users/{id}` | A | Deaktivasyon |
| POST | `/v1/users/{id}/roles` | A | Rol atama |

### Cihaz / Endpoint (Faz 1 + 6)

| Method | Path | Rol | Amaç |
|---|---|---|---|
| GET | `/v1/endpoints` | A/D/H/M/I/AU | Liste + cursor pagination |
| GET | `/v1/endpoints/{id}` | A/D/H/M/I/AU | Detay |
| POST | `/v1/endpoints/enroll` | A | Opaque enroll token üret (24h, tek kullanım) |
| POST | `/v1/agent-enroll` | PUB* | CSR imzalama (token ile auth) |
| POST | `/v1/endpoints/{id}/wipe` | A/D | Uzaktan wipe komutu (KVKK m.7) |
| POST | `/v1/endpoints/{id}/refresh-token` | A | Cert refresh token |
| POST | `/v1/endpoints/{id}/deactivate` | A | Pasifleştirme |
| POST | `/v1/endpoints/{id}/revoke` | A/D | Cert revoke (Vault) |
| POST | `/v1/endpoints/bulk` | A | Toplu işlem (500 limit) |
| GET | `/v1/endpoints/{id}/commands` | A/D/S | Komut kuyruğu |
| POST | `/v1/internal/commands/{id}/ack` | S | Komut ACK (agent'tan) |

### Politika (Faz 1)

| Method | Path | Rol | Amaç |
|---|---|---|---|
| GET | `/v1/policies` | A/D/M | Liste |
| POST | `/v1/policies` | A | Oluştur |
| GET | `/v1/policies/{id}` | A/D/M/AU | Detay |
| PUT | `/v1/policies/{id}` | A | Güncelle |
| DELETE | `/v1/policies/{id}` | A | Soft delete |
| POST | `/v1/policies/{id}/push` | A | İmzalı push + CC8.1 evidence |

### Canlı İzleme (Faz 1)

| Method | Path | Rol | Amaç |
|---|---|---|---|
| GET | `/v1/liveview/requests` | A/D/H/I/AU | Liste (rolüne göre filtre) |
| POST | `/v1/liveview/requests` | A/I | Talep oluştur |
| POST | `/v1/liveview/requests/{id}/approve` | H | Dual-control onay |
| POST | `/v1/liveview/requests/{id}/reject` | H | Ret |
| POST | `/v1/liveview/sessions/{id}/terminate` | A/H/I | Sonlandır + CC6.1 evidence |
| GET | `/v1/liveview/sessions/{id}` | A/D/H/AU | Oturum detayı |

### DSR (KVKK m.11, Faz 1 + 6)

| Method | Path | Rol | Amaç |
|---|---|---|---|
| GET | `/v1/dsr` | A/D | Liste |
| POST | `/v1/dsr` | PUB/E | Başvuru (genelde portal'dan) |
| GET | `/v1/dsr/{id}` | A/D/E* | Detay (E sadece kendi) |
| POST | `/v1/dsr/{id}/fulfill-access` | D | m.11/b-c-d karşılama |
| POST | `/v1/dsr/{id}/fulfill-erasure` | D | m.11/e-f karşılama |
| POST | `/v1/dsr/{id}/respond` | D | Cevap + P7.1 evidence |
| POST | `/v1/dsr/{id}/reject` | D | Ret (gerekçeli) |

### Legal Hold (Faz 1)

| Method | Path | Rol | Amaç |
|---|---|---|---|
| GET | `/v1/legal-holds` | D/AU | Liste |
| POST | `/v1/legal-holds` | D | Koyma (gerekçe zorunlu) |
| POST | `/v1/legal-holds/{id}/release` | D | Kaldırma |

### Audit Log (Faz 1 + 6)

| Method | Path | Rol | Amaç |
|---|---|---|---|
| GET | `/v1/audit` | A/D/AU | Liste (tenant scope) |
| GET | `/v1/audit/{id}` | A/D/AU | Detay |
| POST | `/v1/audit/verify` | D/AU | Hash chain doğrulama |
| GET | `/v1/audit/stream` | A/D/AU | Canlı WebSocket stream |
| GET | `/v1/search/audit` | A/D/AU | Full-text OpenSearch arama |

### Search (Faz 6)

| Method | Path | Rol | Amaç |
|---|---|---|---|
| GET | `/v1/search/events` | A/D/M/I/AU | Event full-text |

### Reports (Faz 6 + 8)

| Method | Path | Rol | Amaç |
|---|---|---|---|
| GET | `/v1/reports/ch/top-apps` | A/M/AU | En çok kullanılan uygulamalar |
| GET | `/v1/reports/ch/idle-active` | A/M/AU | Boşta/aktif zaman |
| GET | `/v1/reports/ch/productivity` | A/M/AU | Prodüktivite skoru |
| GET | `/v1/reports/ch/app-blocks` | A/D/M/AU | App bloklama istatistikleri |
| GET | `/v1/reports/ch/productivity-breakdown` | A/M/AU | Detaylı dağılım |
| GET | `/v1/reports/ch/risk-score` | A/D/AU | UBA risk (advisory) |
| GET | `/v1/reports/trends` | A/M/AU | Haftalık/aylık/çeyrek trend |
| POST | `/v1/reports/export` | A/M/D/AU | PDF/Excel export |

### Pipeline Ops (Faz 7)

| Method | Path | Rol | Amaç |
|---|---|---|---|
| GET | `/v1/pipeline/dlq` | A/AU | Dead letter queue |
| POST | `/v1/pipeline/replay` | A | Event replay |
| GET | `/v1/pipeline/schemas` | A/AU | Schema registry |

### API Keys (Faz 6)

| Method | Path | Rol | Amaç |
|---|---|---|---|
| GET | `/v1/apikeys` | A | Liste |
| POST | `/v1/apikeys` | A | Oluştur (key bir kez gösterilir) |
| DELETE | `/v1/apikeys/{id}` | A | Revoke |

### Evidence Locker — SOC 2 (Faz 3.0)

| Method | Path | Rol | Amaç |
|---|---|---|---|
| GET | `/v1/system/evidence-coverage` | D/AU | Coverage matrix + gap |
| GET | `/v1/dpo/evidence-packs` | D | İmzalı ZIP export |
| POST | `/v1/system/access-reviews` | A/D | CC6.3 kanıt |
| POST | `/v1/system/incident-closures` | A/D | CC7.3 kanıt |
| POST | `/v1/system/bcp-drills` | A | CC9.1 kanıt |
| POST | `/v1/system/backup-runs` | A/S | A1.2 kanıt |

### System (Faz 1 + 6)

| Method | Path | Rol | Amaç |
|---|---|---|---|
| GET | `/v1/system/module-state` | A/D/AU | DLP/ML/OCR/UBA durum |
| GET | `/v1/system/config` | A | Config (secret redacted) |
| POST | `/v1/system/bootstrap` | A | İlk kurulum tetikle |

### Destruction Reports (Faz 1)

| Method | Path | Rol | Amaç |
|---|---|---|---|
| GET | `/v1/destruction-reports` | D/AU | 6 aylık imza PDF listesi |
| POST | `/v1/destruction-reports/generate` | D | Yeni rapor üret |

### Mobile BFF (Faz 2.9-2.10)

| Method | Path | Rol | Amaç |
|---|---|---|---|
| POST | `/v1/mobile/push-token` | A/D/H/I | Push notification token register |
| GET | `/v1/mobile/summary` | A/D/H/I | Dashboard özeti |
| GET | `/v1/mobile/liveview/pending` | H/I | Bekleyen canlı izleme |
| GET | `/v1/mobile/dsr/pending` | D | Bekleyen DSR |
| POST | `/v1/mobile/silence` | A/D | Tenant silence mode |

## Toplam Sayım

| Kategori | Endpoint Sayısı |
|---|---|
| Sağlık | 3 |
| Kimlik/User | 6 |
| Cihaz | 11 |
| Politika | 6 |
| Canlı İzleme | 6 |
| DSR | 7 |
| Legal Hold | 3 |
| Audit/Search | 5 |
| Reports | 8 |
| Pipeline | 3 |
| API Keys | 3 |
| Evidence | 6 |
| System | 3 |
| Destruction | 2 |
| Mobile | 5 |
| **TOPLAM** | **77** |

(Faz 1 baseline ~57 operation + Faz 6-8 yeni ~20 operation.)

## RBAC Özeti

Her endpoint için role kısıtlaması Go kaynağında `apps/api/internal/httpserver/middleware/rbac.go` ile tanımlanır. Bu doküman single source of truth **değildir** — kaynak kod otoritedir. Bu tablo review amaçlıdır.

## Rate Limit

| Kategori | Default (per tenant) |
|---|---|
| Read endpoints | 1000 rps |
| Write endpoints | 100 rps |
| Export endpoints | 10 rps |
| Bulk endpoints | 1 concurrent |
| Agent gateway (ayrı servis) | 10 000 rps |

Aşıldığında `429 Too Many Requests` + `Retry-After` header.

## Audit Coverage

Her **mutating** endpoint otomatik olarak audit log'a yazar (middleware). `apps/api/internal/audit/actions.go` dosyasında 55+ canonical action tanımı.

Faz 3.0 ile bazı endpoint'ler ek olarak **evidence locker**'a kayıt düşürür:
- `POST /v1/policies/{id}/push` → CC8.1
- `POST /v1/dsr/{id}/respond` → P7.1
- `POST /v1/liveview/sessions/{id}/terminate` → CC6.1
- `POST /v1/system/access-reviews` → CC6.3
- `POST /v1/system/incident-closures` → CC7.3
- `POST /v1/system/bcp-drills` → CC9.1
- `POST /v1/system/backup-runs` → A1.2

## Versiyon

| Sürüm | Tarih | Değişiklik |
|---|---|---|
| 1.0 | 2026-04-13 | Faz 15 #163 — İlk sürüm; Faz 1-9 + SOC 2 3.0 endpoint'leri |
