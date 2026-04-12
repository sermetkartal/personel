# CLAUDE.md — Personel Platform

> **Bu dosya, Personel repository'sine giren her Claude Code oturumu (ve insan geliştirici) tarafından ilk okunması gereken dosyadır.** Projenin "neyi", "neden", "nasıl" ve "nerede" durduğunu tek sayfada özetler. Ayrıntılar için ilgili belgelere link verir — aynı içeriği tekrarlamaz.
>
> Versiyon: 1.6 — Faz 3.0.5 (Vault verify + tests + Prometheus + UI history) — 2026-04-11

---

## 1. Personel Nedir?

**Personel**, kurumsal müşteriler için tasarlanmış, on-prem çalışan bir **User Activity Monitoring (UAM) ve performans takip platformudur**. Türkiye pazarına özel (KVKK-native), on-prem-first, KVKK uyumlu bir ürün olarak konumlandırılmıştır. Teramind, ActivTrak, Veriato, Insightful ve Safetica gibi uluslararası rakiplerle doğrudan yarışır.

### Temel Değer Önerisi

1. **KVKK-native uyum**: VERBİS export, otomatik saklama matrisi, Şeffaflık Portalı, hash-zincirli audit — hiçbir rakip bunu mimari seviyesinde yapmıyor
2. **Kriptografik çalışan gizliliği**: Klavye içeriği yakalanır ama yöneticiler tarafından **kriptografik olarak** okunamaz. Sadece izole DLP motoru, önceden tanımlı kurallarla eşleşme aramak için çözebilir. Bu mimariyi ADR 0013 **varsayılan olarak KAPALI** yaptı — opt-in ceremony gerekiyor.
3. **HR-gated canlı izleme**: İkili onay kapısı (requester ≠ approver), zaman sınırı, hash-zincirli audit
4. **Düşük endpoint ayak izi**: Rust agent, hedef <%2 CPU, <150MB RAM
5. **On-prem modern stack**: Docker Compose + systemd, 500 endpoint için 2 saatlik kurulum hedefi
6. **Türkçe-first UI**: Hem admin console hem şeffaflık portalı Türkçe; İngilizce fallback

### Ne DEĞİL

- SaaS ürünü değil (Faz 3+ için planlanıyor, şu an değil)
- macOS/Linux endpoint agent değil (Faz 2)
- Açık kaynak değil (ticari ürün)
- Bir "güvenlik" aracı tek başına değil — compliance + güvenlik + productivity analytics bir arada

---

## 2. Mimari Özeti

```
┌──────────────────────────────────────────────────────────────────┐
│ ENDPOINT (Windows)                                               │
│ Rust agent → collectors → encrypted SQLite queue → gRPC bidi     │
└────────────────┬─────────────────────────────────────────────────┘
                 │ mTLS + gRPC bidi stream + key-version handshake
                 ▼
┌──────────────────────────────────────────────────────────────────┐
│ GATEWAY (Go)                                                     │
│ • mTLS auth + cert pinning                                       │
│ • Rate limit + backpressure                                      │
│ • Key-version handshake (Hello.pe_dek_version/tmk_version)       │
│ • NATS JetStream publisher                                       │
│ • Heartbeat monitor (Flow 7: employee-initiated disable)         │
│ • Live view router                                               │
└────────────────┬─────────────────────────────────────────────────┘
                 │ NATS subjects: events.raw.*, events.sensitive.*,
                 │                live_view.control.*, agent.health.*
                 ▼
┌──────────────────────────────────────────────────────────────────┐
│ ENRICHER (Go, same repo as gateway)                              │
│ • NATS JetStream consumer                                        │
│ • Sensitivity guard (ADR 0013 + KVKK m.6)                        │
│ • Tenant/endpoint metadata enrichment                            │
│ • Route to ClickHouse (events) + MinIO (blobs)                   │
└────────────────┬─────────────────────────────────────────────────┘
                 ▼
┌──────────────────────────────────────────────────────────────────┐
│ STORAGE TIER                                                     │
│ • PostgreSQL — tenants, users, endpoints, policies, DSR, audit   │
│ • ClickHouse — time-series events (1B+/day target)               │
│ • MinIO — screenshots, video, encrypted keystroke blobs          │
│ • OpenSearch — full-text audit search                            │
│ • Vault — PKI + tenant master keys + control-plane signing key   │
│ • Keycloak — OIDC/SAML auth for console & portal                 │
└────────────────┬─────────────────────────────────────────────────┘
                 ▼
┌──────────────────────────────────────────────────────────────────┐
│ ADMIN API (Go, chi + OpenAPI 3.1)                                │
│ • OIDC auth + RBAC (7 roles)                                     │
│ • DSR (KVKK m.11) workflow with 30-day SLA                       │
│ • Legal hold (DPO-only)                                          │
│ • 6-month destruction report generator (signed PDF)              │
│ • HR-gated live view state machine                               │
│ • Policy CRUD + signing with control-plane key                   │
│ • Hash-chained audit log (every mutation)                        │
│ • Reports via ClickHouse                                         │
│ • Screenshot presigned URL issuer                                │
│ • Transparency portal backend endpoints                          │
└──────┬────────────────────────────┬──────────────────────────────┘
       ▼                            ▼
┌─────────────────┐        ┌─────────────────────────┐
│ ADMIN CONSOLE   │        │ TRANSPARENCY PORTAL     │
│ (Next.js 15)    │        │ (Next.js 15)            │
│ Admin/HR/DPO/   │        │ Employee self-service   │
│ Manager/        │        │ KVKK m.10/m.11          │
│ Investigator    │        │ TR-first trust UX       │
└─────────────────┘        └─────────────────────────┘
```

### Detaylı diyagramlar

- **C4 Context**: `docs/architecture/c4-context.md`
- **C4 Container**: `docs/architecture/c4-container.md`
- **Bounded Contexts (DDD)**: `docs/architecture/bounded-contexts.md`
- **Event Taxonomy (36 event types)**: `docs/architecture/event-taxonomy.md`
- **Key Hierarchy (kriptografik)**: `docs/architecture/key-hierarchy.md`
- **Live View Protocol**: `docs/architecture/live-view-protocol.md`
- **mTLS PKI**: `docs/architecture/mtls-pki.md`
- **Data Retention Matrix**: `docs/architecture/data-retention-matrix.md`

---

## 3. Repository Layout

```
personel/
├── CLAUDE.md                       ← bu dosya
├── README.md                       ← TR product description + EN dev quickstart
│
├── docs/                           (47 doküman)
│   ├── README.md                   ← docs index
│   ├── architecture/               (12) — C4, bounded contexts, retention, PKI, key hierarchy
│   ├── compliance/                 (8)  — KVKK framework, aydınlatma, açık rıza, DPIA, VERBİS, risk register
│   ├── security/                   (10) — threat model, anti-tamper + 7 runbook + security decisions
│   ├── product/                    (1)  — competitive analysis (Teramind vs)
│   └── adr/                        (13) — Architecture Decision Records
│
├── proto/personel/v1/              (5) — gRPC proto contracts: common, agent, events, policy, live_view
│
├── apps/
│   ├── agent/                      ← Rust Windows agent (13-crate Cargo workspace, 70 files)
│   │   ├── Cargo.toml              ← workspace deps
│   │   ├── rust-toolchain.toml     ← MSRV 1.88 (bumped from 1.75 in reality check)
│   │   └── crates/
│   │       ├── personel-core       ← types, errors, IDs, clock
│   │       ├── personel-crypto     ← AES-GCM envelope, X25519 enrollment, DPAPI keystore
│   │       ├── personel-queue      ← SQLCipher offline buffer
│   │       ├── personel-policy     ← policy engine with Ed25519 verification
│   │       ├── personel-collectors ← Collector trait + 12 collector modules
│   │       ├── personel-transport  ← tonic gRPC client + rustls
│   │       ├── personel-proto      ← tonic-build generated stubs
│   │       ├── personel-os         ← Windows (ETW, GDI, DPAPI) + stub for dev
│   │       ├── personel-updater    ← dual-signed update verification
│   │       ├── personel-livestream ← LiveKit WebRTC (stub)
│   │       ├── personel-agent      ← main Windows service binary
│   │       ├── personel-watchdog   ← sibling watchdog process
│   │       └── personel-tests      ← workspace smoke tests
│   │
│   ├── gateway/                    ← Go gRPC ingest gateway + enricher (51 files)
│   │   ├── cmd/gateway/            ← main ingest binary
│   │   ├── cmd/enricher/           ← NATS→ClickHouse/MinIO pipeline
│   │   ├── internal/grpcserver/    ← bidi stream server, auth, rate limit
│   │   ├── internal/nats/          ← JetStream publisher/consumer
│   │   ├── internal/heartbeat/     ← Flow 7 employee-disable classifier
│   │   ├── internal/liveview/      ← live view command router
│   │   └── pkg/proto/              ← generated stubs (go.mod submodule)
│   │
│   ├── api/                        ← Go chi admin API (90 files, 57-op OpenAPI)
│   │   ├── cmd/api/                ← main binary
│   │   ├── api/openapi.yaml        ← contract, consumed by console
│   │   ├── internal/httpserver/    ← chi router + middleware (audit, RBAC, OIDC)
│   │   ├── internal/httpx/         ← RFC7807 + request-id (broken out in reality check to fix cycle)
│   │   ├── internal/audit/         ← hash-chain recorder + verifier + 55 canonical actions
│   │   ├── internal/dsr/           ← KVKK m.11 workflow + 30-day SLA
│   │   ├── internal/legalhold/     ← DPO-only handlers
│   │   ├── internal/destruction/   ← 6-month signed PDF reports
│   │   ├── internal/liveview/      ← state machine with persistence
│   │   ├── internal/policy/        ← signing + NATS publisher
│   │   ├── internal/vault/         ← Vault client (+ stub mode for tests)
│   │   ├── internal/postgres/migrations/ ← embedded .sql files
│   │   └── test/integration/       ← testcontainers-go e2e tests
│   │
│   ├── console/                    ← Next.js 15 admin UI (133 files)
│   │   ├── messages/tr.json + en.json
│   │   ├── src/app/[locale]/(app)/ ← all pages for Admin/HR/DPO/Manager roles
│   │   │   ├── dashboard/
│   │   │   ├── endpoints/
│   │   │   ├── dsr/                ← KVKK m.11 DPO dashboard
│   │   │   ├── live-view/          ← request + HR approval + LiveKit viewer
│   │   │   ├── audit/              ← hash-chained log viewer
│   │   │   ├── legal-hold/
│   │   │   ├── destruction-reports/
│   │   │   ├── policies/           ← SensitivityGuard editor
│   │   │   └── settings/dlp/       ← ADR 0013 ceremony explainer (NO enable button)
│   │   └── src/components/
│   │       └── layout/dlp-status-badge.tsx  ← always-visible DLP state
│   │
│   ├── portal/                     ← Next.js 15 employee portal (62 files)
│   │   ├── messages/tr.json + en.json
│   │   ├── src/app/[locale]/       ← trust-first design
│   │   │   ├── aydinlatma/         ← KVKK m.10 legal notice
│   │   │   ├── verilerim/          ← what is monitored (11 categories)
│   │   │   ├── neler-izlenmiyor/   ← trust-building: what is NOT monitored (10 items)
│   │   │   ├── haklar/             ← KVKK m.11 rights
│   │   │   ├── basvurularim/       ← employee's DSRs
│   │   │   ├── canli-izleme/       ← policy explainer + session history
│   │   │   └── dlp-durumu/         ← ADR 0013 employee-facing state
│   │   └── src/components/
│   │       └── onboarding/first-login-modal.tsx  ← mandatory audited acknowledgement
│   │
│   ├── ml-classifier/              ← Phase 2.3 Python service (Llama 3.2 3B + fallback, ADR 0017)
│   ├── ocr-service/                ← Phase 2.8 Python service (Tesseract + PaddleOCR, KVKK redaction)
│   ├── uba-detector/               ← Phase 2.6 Python service (isolation forest, ADR compliance)
│   ├── livrec-service/             ← Phase 2.8 Go service (live view recording, ADR 0019)
│   ├── mobile-admin/               ← Phase 2.4 React Native + Expo (5 screens: home, live view approvals, DSR queue, silence, profile)
│   └── qa/                         ← QA framework (51 files)
│       ├── cmd/simulator/          ← 10K-agent traffic generator
│       ├── cmd/audit-redteam/      ← keystroke admin-blindness red team (Phase 1 exit #9)
│       ├── cmd/footprint-bench/    ← Windows CPU/RAM measurement harness
│       ├── cmd/chaos/              ← chaos drills
│       ├── test/e2e/               ← 10 end-to-end suites (enrollment, flow7, DSR, liveview, audit, rbac)
│       ├── test/load/              ← 4 load scenarios (500 steady, 10k ramp, 10k burst, chaos)
│       ├── test/security/          ← fuzz + cert pinning + keystroke red team
│       └── ci/thresholds.yaml      ← Phase 1 exit criteria as machine-readable gates
│
└── infra/                          ← On-prem deployment (76 files)
    ├── install.sh                  ← idempotent installer, 2h target
    ├── compose/
    │   ├── docker-compose.yaml     ← production stack (18 services)
    │   ├── docker-compose.override.yaml
    │   ├── vault/                  ← Shamir 3-of-5 + HCL policies
    │   ├── postgres/init.sql       ← bootstrap: audit.append_event proc, RBAC roles
    │   ├── clickhouse/             ← single-node config + macros for Phase 1 exit replication
    │   ├── nats/                   ← JetStream at-rest encryption
    │   ├── keycloak/               ← realm-personel.json (clients, roles)
    │   ├── dlp/                    ← distroless + seccomp + AppArmor (Profile 1)
    │   └── prometheus/alerts.yml   ← Flow 7, DSR SLA, Vault audit, backup alerts
    ├── systemd/                    ← personel-*.service + timers
    ├── scripts/                    ← preflight, ca-bootstrap, vault-unseal, rotate, forensic-export
    └── runbooks/                   ← install, backup, DR, upgrade, troubleshooting (TR/EN)
```

---

## 4. Tech Stack

| Katman | Teknoloji | Sürüm | Gerekçe |
|---|---|---|---|
| Agent dili | Rust | MSRV 1.88 | Bellek güvenli, düşük ayak izi, tek binary |
| Agent Windows API | `windows` crate + ETW user-mode | 0.54 | User-mode first; minifilter Faz 3 |
| Agent queue | rusqlite + bundled-sqlcipher | 0.31 | AES-256 page encryption, no DLL dep |
| Agent crypto | aes-gcm, x25519-dalek, hkdf, ed25519-dalek | RustCrypto | FIPS-aligned primitives |
| Gateway | Go + tonic-gateway | 1.22+ (user has 1.26) | High concurrency, simple ops |
| Admin API | Go + chi + koanf + golang-migrate | 1.22+ | Stdlib slog, no zap/logrus/viper |
| Event bus | NATS JetStream | 2.10+ | At-rest encryption, simpler than Kafka |
| Time-series | ClickHouse | 24.x | 10-30x compression vs SQL Server |
| Metadata DB | PostgreSQL | 16 | RLS for multi-tenancy |
| Object store | MinIO | latest | S3-compatible, lifecycle policies |
| Full-text | OpenSearch | 2.x | Apache 2.0, Elastic licensing trap avoided |
| PKI / secrets | HashiCorp Vault | 1.15.6 | Transit engine for TMK, `exportable: false` |
| Auth | Keycloak | 24 | OIDC/SAML/SCIM |
| Live view | LiveKit (self-hosted) | latest | WebRTC SFU, Apache 2.0 |
| Admin UI | Next.js 15 + TanStack Query + shadcn/ui + Tailwind 3 | 15.1 | App Router, server components first |
| Employee portal | Next.js 15 (distinct design from console) | 15.2 | Trust-first palette, smaller deps |
| i18n | next-intl | 3.26 | TR-first, EN fallback |
| Observability | OpenTelemetry + Prometheus + Grafana | latest | Vendor-neutral |
| Deployment | Docker Compose + systemd | compose v2 | On-prem; K8s deferred |

---

## 5. Phase Status

### Faz 0 — Mimari Omurga (✅ TAMAM)

- 11 architecture doc + 13 ADR + 5 proto + 2 security doc
- Pilot architect (microservices-architect agent) tarafından tek seferde üretildi
- Revision round 1 ile 3 çakışma + 13 gap kapatıldı
- Revision round 2 ile ADR 0013 (DLP off-by-default) propage edildi

### Faz 0.5/0.6 — KVKK + Güvenlik + Rakip (✅ TAMAM)

- KVKK compliance framework (compliance-auditor): 8 doc
- Güvenlik runbook'ları (security-engineer): 7 runbook + security decisions
- Rakip analizi (competitive-analyst): 8.9K kelime, Teramind/ActivTrak/Veriato/Insightful/Safetica teardown

### Faz 1 — İmplementasyon (✅ BUILD CLEAN)

| Bileşen | Dosya | Build | Test |
|---|---|---|---|
| Rust agent (cross-platform crates) | 70 | ✅ `cargo check` clean | ❌ unit tests not run |
| Rust agent (Windows crates) | (same) | ⚠️ stub code — needs Windows | ❌ |
| Go gateway + enricher | 51 | ✅ `go build ./...` clean | ❌ integration tests not run |
| Go admin API | 90 | ✅ `go build ./...` clean | ❌ integration tests not run |
| Go QA framework | 51 | ✅ `go build ./...` clean | ❌ (it IS the tests) |
| Next.js console | 133 | ✅ `pnpm build` clean | ❌ Playwright not written |
| Next.js portal | 62 | ✅ `pnpm build` clean | ❌ |
| On-prem infra | 76 | ✅ `docker compose config` valid | ❌ full stack not started |

**Faz 1 Exit Criteria durumu**: 18 kriterden hiçbiri doğrulanmadı. Tüm kod build edilebilir durumda ama hiçbir entegrasyon/load/security testi koşmadı. Gerçek pilot hazırlığı için:

1. Full Docker Compose stack'i çalıştır, PKI bootstrap ceremony yap
2. 500 synthetic endpoint ile load test
3. Keystroke admin-blindness red team testi (en kritik Phase 1 exit)
4. Pilot müşteri KVKK DPO review

### Faz 1 Reality Check (2026-04-11)

Phase 1 kodları build edilemiyordu. 36 gerçek hata bulunup düzeltildi. Detay: commit 2b601cc.

### Faz 2 (2026-04-11 — actively in progress, scaffold phase)

**Phase 2.0 — Forward-compat gap closures** (commit 55e4f15):
  - Migration 0023: users table HRIS fields (hris_id, department, manager_user_id, etc)
  - Generalized GET /v1/system/module-state (replaces dlp-state-only)
  - EventMeta proto tag reservation (6 fields for category/confidence/sensitivity/hris/ocr)
  - AgentError::Unsupported variant + personel-os stub cleanup

**Phase 2.1 — macOS + Linux agent scaffolds** (commit b31f1c0):
  - personel-os-macos crate: Endpoint Security Framework bridge, ScreenCaptureKit,
    TCC, Network Extension, IOHIDManager, launchd plist generator, Keychain
  - personel-os-linux crate: fanotify, libbpf-rs eBPF loader, X11/Wayland dual
    adapters (Wayland permanently Unsupported per ADR 0016), systemd notify
  - Both compile on all 3 OSes via target_os stubs

**Phase 2.2 — personel-platform facade** (commit 9dad897):
  - Compile-time target_os dispatch between windows/macos/linux backends
  - Only unifies truly common surfaces (input::foreground_window_info,
    service::is_service_context); specialized platform APIs remain direct
  - personel-collectors + personel-agent now depend on facade

**Phase 2.3 — ML category classifier** (commit 15cd77d):
  - apps/ml-classifier/ Python FastAPI + Llama 3.2 3B Instruct (llama-cpp-python)
  - FallbackClassifier with 50+ Turkish + international rules
  - /v1/classify + /v1/classify/batch + readyz health
  - Strict JSON output, ADR 0017 confidence threshold (0.70 → unknown)
  - Multi-stage Dockerfile, distroless-like hardening, net_ml isolated network

**Phase 2.3b — Go regex fallback classifier** (commit bee92bc):
  - apps/gateway/internal/enricher/classifier.go + ml_client.go
  - Turkish business software rules (Logo Tiger, Mikro, Netsis, Paraşüt, BordroPlus)
  - 50ms timeout on ML service with graceful fallback; 27 test cases passing

**Phase 2.4 — Mobile admin app** (commit bee92bc):
  - apps/mobile-admin/ Expo 52 + React Native 0.76 + TypeScript strict
  - 5 screens: sign-in, home, live view approvals, DSR queue, silence
  - OIDC PKCE via expo-auth-session, zustand+MMKV session, expo-notifications
  - Push payloads PII-free per ADR 0019 (type + count + deep_link only)
  - EAS profiles: development, preview, production

**Phase 2.5 — HRIS connector framework** (commit bee92bc):
  - apps/api/internal/hris/ + 2 adapter scaffolds (BambooHR, Logo Tiger)
  - Compile-time Factory registry (ADR 0018 security: no runtime plugins)
  - Employee canonical type mapping to Phase 2.0 users columns
  - sync.Orchestrator with TestConnection + startup auth paging + polling loop
  - ChangeKind events for webhook-driven adapters; fallback polling for Logo Tiger

**Phase 2.6 — UBA / insider threat detector** (commit 15cd77d):
  - apps/uba-detector/ Python service using scikit-learn isolation forest
  - 7 features: off_hours, app_diversity, data_egress, screenshot_rate,
    file_access_rate, policy_violations, new_host_ratio
  - Turkish TRT UTC+3 business hour awareness
  - KVKK m.11/g advisory-only disclaimer enforced in every response
  - 6 ClickHouse materialized views defined (DDL ready for DBA provisioning)

**Phase 2.7 — SIEM exporter framework** (commit 15cd77d):
  - apps/api/internal/siem/ + 2 adapter scaffolds (Splunk HEC, Microsoft Sentinel)
  - In-process Bus with per-exporter bounded buffers (non-blocking publish;
    drops under backpressure; audit chain is authoritative per ADR 0014)
  - OCSF schema alignment with class_uid + severity_id
  - 10 EventType taxonomy covering audit, login, DSR, live view, DLP, tamper, silence

**Phase 2.8 — OCR service + live view recording** (commit fc1c7e0 partial):
  - apps/ocr-service/ Python Tesseract + PaddleOCR with Turkish + English
  - KVKK m.6 redaction: TCKN (official algorithm), IBAN, credit card (Luhn),
    Turkish phone, email → replaced with [TAG] before response encoding
  - apps/livrec-service/ Go service (still building at this CLAUDE.md update;
    will be in next commit): per-session WebM recording with independent LVMK
    Vault key hierarchy, dual-control playback, 30-day retention, DPO-only export

**Phase 2.9 — Mobile BFF endpoints on admin API** (commit 05e920a):
  - apps/api/internal/mobile/ with 5 endpoints under /v1/mobile/*
  - Decided against separate mobile-bff service (operational simplicity)
  - Push token registration with pgcrypto-sealed storage, sha256 hash logged

**Phase 2.10 — Real mobile summary aggregation** (commit fc1c7e0):
  - Migration 0024: mobile_push_tokens table with RLS + tenant isolation
  - Real DSR/liveview/silence/dlp delegation in mobile.Service.GetSummary
  - Fault-tolerant: per-query failures degrade individually, not the summary

**Phase 3.0 kickoff — Evidence Locker dual-write** (commit a98366f):
  - Migration 0025: evidence_items table with RLS + append-only (REVOKE UPDATE, DELETE)
  - `apps/api/internal/evidence/store.go` real implementation:
    WORM bucket PUT first → Postgres INSERT second; WORM failure short-circuits
  - `audit.WORMSink` extended with PutEvidence + GetEvidence (shares audit-worm
    bucket, 5-year Compliance mode retention, key `evidence/{tenant}/{period}/{id}.bin`)
  - `evidence.EvidenceWORM` narrow interface keeps packages decoupled + testable
  - 4 unit tests covering: nil WORM rejection, unsigned item rejection,
    WORM-failure short-circuit (no Postgres touch), canonicalize determinism
  - Wired into cmd/api/main.go with graceful degradation when WORM sink is
    unavailable at startup (domain collectors see nil Recorder and must handle)

**Phase 3.0.1 — Vault signer + first collector (liveview)** (commit f574786):
  - NoopSigner replaced by `vault.Client` — the existing `Sign(ctx, payload)`
    method already matches `evidence.Signer` by interface shape; a compile-time
    assertion (`var _ evidence.Signer = (*vaultclient.Client)(nil)`) catches
    signature drift at build time. Evidence items are now signed with the same
    control-plane Ed25519 key used by daily audit checkpoints.
  - **First domain collector: liveview.** `liveview.Service.terminateSession`
    emits a `KindPrivilegedAccessSession` evidence item mapped to control
    `CC6.1` for every terminated HR-approved session. Payload captures
    requester, approver, endpoint, reason code + full justification text,
    requested vs actual duration, final state, and the termination audit ID.
  - New `ItemKind`: `KindPrivilegedAccessSession` (existing kinds don't fit
    time-bounded dual-controlled screen view).
  - Optional wiring pattern: `Service.SetEvidenceRecorder(r)` — constructor
    signature stayed stable so existing tests and all callers unchanged.
  - Emission is best-effort: Recorder errors are logged (loud) but never
    propagate to the session termination path. Observability carries the
    coverage gap signal, not user-facing error surfaces.
  - 4 new liveview unit tests: happy path (CC6.1, correct payload JSON,
    720s actual duration vs 900s requested), nil-recorder no-op, nil-approver
    defence-in-depth skip, Recorder-error swallow.

**Design pattern established**: domain services gain evidence via optional
setter injection. Every future collector (backup run, vendor review, etc.)
follows the same shape:
  1. Import `internal/evidence`
  2. Add `evidenceRecorder evidence.Recorder` field + `SetEvidenceRecorder`
  3. Emit in the post-success path of the relevant method
  4. Swallow errors, log loudly, cite the relevant audit log ID(s)
  5. Wire in `cmd/api/main.go` under the `if wormSink != nil` block

**Phase 3.0.5 — Production hardening (Vault verify + tests + Prometheus + UI history)**:
  - `vault.Client.Verify` real implementation: parses `name:vN` combined
    key version, reconstructs Vault's `vault:vN:<base64>` wire format,
    calls `transit/verify/{key}`, checks `valid:true`. Stub client also
    implements `overrideVerify` for in-process tests. Compile-time
    assertion `var _ evidence.Verifier = (*vaultclient.Client)(nil)` in
    main.go catches drift at build time.
  - Unit tests for `parseKeyVersion` (10 cases covering embedded colons,
    malformed input, v0 rejection) + stub Sign→Verify round-trip +
    tamper detection + unknown version rejection.
  - `accessreview.Service`, `incident.Service`, `bcp.Service` now have
    unit test coverage: validation rejection matrix, dual-control
    enforcement (vault_root + break_glass require distinct second
    reviewer), tally helpers, payload shape snapshot, 72h KVKK compliance
    calculation, tier_results preservation.
  - Prometheus gauge `personel_evidence_items_total{tenant_id,control,period}`:
    implements `prometheus.Collector`; runs a single GROUP BY per tenant
    at scrape time (no background refresh, no staleness). Registered in
    `main.go` alongside Go + process collectors; tenant list sourced
    from the same list the audit verifier uses.
  - Two new alert rules in `infra/compose/prometheus/alerts.yml`:
    * `SOC2EvidenceCoverageGap` (warning) — 24h zero window fires after 1h
    * `SOC2EvidenceCoverageCritical` (critical) — 7d zero window fires after 6h
  - `infra/runbooks/soc2-manual-evidence-submission.md`: Turkish DPO
    runbook with curl + JSON templates for all four manual-submit
    endpoints (access-reviews, incident-closures, bcp-drills, backup-runs).
  - Console `/tr/evidence` page: added **12-month coverage history
    heatmap** below the current-period matrix. Uses `useQueries` to
    fetch all 12 months in parallel; `heatClass()` maps count → Tailwind
    shade (amber gap / green intensities). Tooltips per cell.

**Phase 3.0.4 — Coverage closure: CC6.3 + CC7.3 + CC9.1 + rotation test + e2e**:
  - **Integration test** `apps/api/test/integration/evidence_test.go`: real
    Postgres testcontainer + in-memory WORM fake; exercises dual-write,
    migration 0025 RLS, CountByControl, ListByPeriod with control filter,
    and PackBuilder end-to-end (verifies ZIP shape, per-item + manifest
    signatures, key version file, WORM key scheme). Three scenarios:
    happy path (3 items, 3 controls), nil-WORM rejection, RLS tenant
    isolation (A's items invisible to B under distinct session vars).
  - **CC6.3 collector** `apps/api/internal/accessreview/`: `RecordReview`
    validates scope, single-vs-dual-control reviewer rules, tallies
    retained/revoked/reduced decisions. Seven scopes including
    `vault_root` + `break_glass` mandate `second_reviewer_id`. Emits
    `KindAccessReview` on CC6.3. `POST /v1/system/access-reviews`
    DPO/Admin-gated.
  - **CC7.3 collector** `apps/api/internal/incident/`: `RecordClosure`
    captures 5-tier severity, detection → containment → closure
    lifecycle, KVKK 72h + GDPR Art. 33 notification compliance
    booleans (late notification still recorded), root cause, and
    remediation action list. Emits `KindIncidentReport` on CC7.3.
    `POST /v1/system/incident-closures` DPO/Admin-gated.
  - **CC9.1 collector** `apps/api/internal/bcp/`: `RecordDrill` captures
    live-vs-tabletop type, scenario tag, per-tier RTO target vs actual
    with `met_rto` flag, drill duration, facilitator, lessons learned.
    Emits `KindBackupRestoreTest` on CC9.1.
    `POST /v1/system/bcp-drills` admin-gated.
  - **Vault key rotation verification** `apps/api/internal/evidence/verify.go`
    + 5 unit tests: `evidence.Verifier` interface + `VerifyItem` function
    that re-canonicalises + calls Verify. Tests use a `keyedSigner`
    mimicking Vault transit key history: sign with v1, rotate to v2 + v3,
    verify both old v1-signed item AND new v3-signed item, verify
    tampered payload fails, verify missing key version fails loudly.
    This is the 5-year-retention invariant: signatures must survive
    rotation.
  - New audit actions: `access_review.completed`, `incident.closed`,
    `bcp_drill.completed`.
  - `evidence.expectedControls()` comments updated to reflect all 9
    controls now have wired collectors — no ❌ gaps remain in the
    expected set.

**Phase 3.0.3 — Console UI + runbook + backup collector**:
  - `/tr/evidence` console sayfası: coverage matrix tablosu + gap uyarı
    kartı + DPO rol gated "Paketi İndir (ZIP)" butonu + dönem seçici
  - `apps/console/src/lib/api/evidence.ts`: `getEvidenceCoverage` +
    `buildEvidencePackURL`; rbac'a `view:evidence` + `download:evidence-pack`
    izinleri; sidebar'a SOC 2 Kanıt Kasası navigation item
  - `infra/runbooks/soc2-evidence-pack-retrieval.md`: aylık pack üretimi +
    imza doğrulama + PGP teslimatı + acil durum senaryolarını içeren
    DPO operasyonel runbook'u (Türkçe)
  - `apps/api/internal/backup/` yeni paketi: `backup.Service.RecordRun` +
    `POST /v1/system/backup-runs` (admin-only); out-of-API cron runner'ı
    backup dump sonrası bu endpoint'e SHA256 + size + duration + target
    path gönderir, service A1.2 + KindBackupRun kanıtı üretir
  - `audit.ActionBackupRun` eklendi; expectedControls() listesinde A1.2
    artık "wired" durumda
  - 4 yeni backup unit test: eksik alan reddi, negatif süre reddi,
    payload şekli snapshot'ı, safePrefix helper

**Phase 3.0.2 — Collectors B→A→D→C** (commit ba044d9):
  - **Collector B (policy.Push → CC8.1)**: every successful signed-policy
    push emits a `KindChangeAuthorization` item capturing actor, target
    endpoint (or `*` for broadcast), policy version, and the full rules
    JSON. Auditor can trace back to the exact deployed bundle.
  - **Collector A (dsr.Respond → P7.1)**: every KVKK m.11 fulfilment emits
    a `KindComplianceAttestation` item with lifecycle metadata:
    created_at, sla_deadline, closed_at, `within_sla` bool,
    `seconds_before_deadline` (negative for overdue), response artifact
    MinIO key, and control_tags `[P5.1, P7.1]`. Overdue DSRs still emit —
    auditors need the overdue record for CC7.3 incident evidence.
  - **Coverage endpoint D (`GET /v1/system/evidence-coverage`)**:
    DPO/Auditor-only. Query param `period=YYYY-MM`. Returns item count per
    expected TSC control + explicit `gap_controls` array of zero-item
    controls. `evidence.expectedControls()` is the CODE source of truth
    for "complete coverage" — adding a control here without a collector
    deliberately creates a gap alert.
  - **Pack export C (`GET /v1/dpo/evidence-packs`)**: DPO-only. Streams a
    signed ZIP: `manifest.json` + per-item JSON + per-item `.signature` +
    `manifest.signature` + `manifest.key_version.txt`. Canonical bytes are
    NOT re-packed — auditors pull them from `audit-worm` via the
    `worm_object_key` in each manifest row. Two independent verification
    gates: (1) manifest signature over the list, (2) each item's own
    signature over its canonical WORM payload.
  - 10 new unit tests (policy, dsr, evidence pack + handlers); full API
    suite green.

### Phase 3.0 endpoint surface (net new)

| Method | Path | Role | Purpose |
|---|---|---|---|
| GET | `/v1/system/evidence-coverage?period=YYYY-MM` | DPO, Auditor | SOC 2 coverage matrix + gap list |
| GET | `/v1/dpo/evidence-packs?period=YYYY-MM&controls=...` | DPO | Signed ZIP export |

### Expected controls (evidence.expectedControls)

| Control | Status | Collector |
|---|---|---|
| CC6.1 | ✅ wired | `liveview.Service.terminateSession` |
| CC6.3 | ✅ wired | `accessreview.Service.RecordReview` (Phase 3.0.4) |
| CC7.1 | ✅ indirect | policy push (shared with CC8.1) |
| CC7.3 | ✅ wired | `incident.Service.RecordClosure` (Phase 3.0.4) |
| CC8.1 | ✅ wired | `policy.Service.Push` |
| CC9.1 | ✅ wired | `bcp.Service.RecordDrill` (Phase 3.0.4) |
| A1.2 | ✅ wired | `backup.Service.RecordRun` (Phase 3.0.3) |
| P5.1 | ✅ secondary | DSR respond (tag) |
| P7.1 | ✅ wired | `dsr.Service.Respond` |

**All 9 expected controls now have wired collectors.** Phase 3.0 data plane
is complete; the observation window can begin producing full-coverage
evidence for every control in the SOC 2 Type II Trust Services Criteria
that Personel commits to.

### Faz 2 remaining work (future commits)

- Real Phase 2 implementations (all current work is scaffolds):
  * BambooHR + Logo Tiger real API calls (Phase 2.11)
  * Splunk HEC + Sentinel DCR real publishing (Phase 2.11)
  * Llama GGUF model download + real inference benchmarking (Phase 2.12)
  * Tesseract OCR real extraction pipeline (Phase 2.12)
  * UBA ClickHouse real feature extraction (Phase 2.12, requires DBA writes)
  * Live view recording WebM chunking (Phase 2.12)
  * Mobile recent audit entries endpoint + module-state integration
- macOS + Linux agent real ETW/Endpoint Security / eBPF implementations
- Canlı izleme WebRTC recording (ADR 0019)
- OCR on screenshots (apps/ocr-service)
- ML-based category classifier (apps/ml-classifier)
- UBA / insider threat detection (apps/uba-detector)
- HRIS entegrasyonları: adapter framework ready, real calls Phase 2.11
- SCIM provisioning
- Mobile admin app (apps/mobile-admin — real implementations needed)
- SIEM entegrasyonları: framework ready, real calls Phase 2.11
- Windows minifilter driver (forensic DLP) — Phase 3

### Faz 3.0 — SOC 2 Type II observation window kickoff (🚧 in progress)

2026-04-11 itibarıyla başladı. Observation window'un başlayabilmesi için
design-level control substrate şart — bu sprint o altyapıyı kuruyor.

**Tamamlananlar:**
- ISO 27001 / SOC 2 policy suite (6 doküman, commit 30c96a4):
  risk register + access review + change management + incident response
  + vendor management + BCDR, Türkçe gövde + İngilizce auditor özeti
- Evidence Locker (commit a98366f): dual-write implementation,
  migration 0025, RLS, append-only, WORM anchor, 4 unit test
- Vault signer + ilk collector (commit f574786): `liveview` → `CC6.1`
- Collectors B+A + coverage + pack export (commit ba044d9):
  * `policy.Push` → `CC8.1` change authorization
  * `dsr.Respond` → `P7.1` KVKK m.11 fulfilment (within/overdue her ikisi)
  * `GET /v1/system/evidence-coverage` → tenant × period matrix + gap list
  * `GET /v1/dpo/evidence-packs` → signed ZIP stream (manifest + per-item
    JSON + per-item + manifest Ed25519 signatures + key version)

**Phase 3.0 kalan iş:**
- Üçüncü tur collector'ları (CC6.3 access review, CC7.3 incident detection,
  CC9.1 BCP drill, A1.2 backup run)
- Konsol `/dpo/evidence` UI (coverage tablosu + pack download düğmesi)
- `infra/runbooks/soc2-evidence-pack-retrieval.md` DPO operasyonel runbook
- Vault transit anahtarlarının key-rotation testi (5 yıllık retention için
  historical signature verification path)
- HRIS → Keycloak 4h revocation automation (ADR 0018 scaffold → real)
- DPA template + sub-processor registry (yasal dokümantasyon; research
  agent iş)

### Faz 3.1+ (planlandı, başlamadı)

- Multi-tenant SaaS deployment (K8s)
- ISO 27001 + ISO 27701 sertifikasyonu (SOC 2 Type II sonrası)
- GDPR genişleme (AB pazarı)
- Sektörel benchmark (anonim havuzlu)
- White-label / reseller portalı
- Billing (Stripe / iyzico)

---

## 6. Locked Decisions

7 decision locked 2026-04-11 (`docs/compliance/... + docs/architecture/*` boyunca referans):

1. **Jurisdiction**: Turkey only (KVKK 6698)
2. **Deployment**: On-prem first; Docker Compose + systemd; K8s ertelendi
3. **Windows agent**: User-mode only Faz 1-2; minifilter Faz 3
4. **Keystroke content**: Şifreli, admin kriptografik olarak okuyamaz, sadece DLP motoru — **ADR 0013 ile "default OFF, opt-in ceremony required"**
5. **Live view**: HR dual-control + reason code + hash-chained audit + 15/60 dk cap + no recording Faz 1
6. **MVP OS**: Windows only
7. **Workflow**: Pilot architect → specialist team; revision round discipline

ADR listesi: `docs/adr/0001..0013` (index: `docs/README.md`).

**ADR 0013 özellikle önemli**: DLP varsayılan KAPALI. Enable etmek için:
- DPIA amendment (customer DPO)
- Signed opt-in form (DPO + IT Security + Legal)
- Vault Secret ID issuance (`infra/scripts/dlp-enable.sh`)
- Container start via `docker compose --profile dlp up -d`
- Transparency portal banner
- Audit checkpoint

---

## 7. Build & Run

### Prerequisites

- Go 1.22+ (tested with 1.26)
- Rust 1.88+ via rustup (toolchain pinned in `apps/agent/rust-toolchain.toml`)
- Node 20+ and pnpm 9+
- Docker 25+ and Docker Compose v2
- protoc (Protocol Buffers compiler)
- Optional: buf (or use protoc + protoc-gen-go/protoc-gen-go-grpc)

### Proto Stub Generation

```bash
# Install Go proto plugins if not already
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.33.0
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.3.0

# Generate gateway stubs
cd /path/to/personel
mkdir -p apps/gateway/pkg/proto/personel/v1
protoc \
  --proto_path=proto \
  --go_out=apps/gateway/pkg/proto \
  --go_opt=paths=source_relative \
  --go-grpc_out=apps/gateway/pkg/proto \
  --go-grpc_opt=paths=source_relative \
  proto/personel/v1/*.proto
```

### Go Workspaces

```bash
# Gateway
cd apps/gateway && go mod tidy && go build ./...

# Admin API
cd apps/api && go mod tidy && go build ./...

# QA framework
cd apps/qa && go mod tidy && go build ./...
```

### Rust Agent

```bash
cd apps/agent
# Cross-platform crates (macOS/Linux dev)
cargo check -p personel-core -p personel-crypto -p personel-queue -p personel-policy

# Full Windows build (requires Windows + MSVC)
cargo build --release
```

### Next.js Apps

```bash
# Admin console
cd apps/console
pnpm install
pnpm dev   # → http://localhost:3000 (with default locale redirect /tr)
# or
pnpm build && pnpm start

# Transparency portal
cd apps/portal
pnpm install
pnpm dev   # → http://localhost:3001
```

### Full Stack (Docker Compose)

```bash
cd infra/compose
cp .env.example .env
# Edit .env — fill all CHANGEME values

# Validate
docker compose config

# Start (requires application images to be built first)
sudo infra/install.sh   # idempotent, runs preflight, Vault unseal ceremony, migrations, smoke test
```

⚠️ **install.sh is not tested end-to-end yet.** First real run will likely hit several issues — see "Known Issues" below.

---

## 8. Testing

### Unit & Integration

```bash
# Go integration tests (requires testcontainers-go to pull Docker images)
cd apps/api && go test -tags integration ./test/integration/...

# QA framework smoke
cd apps/qa && go test ./...
```

### End-to-End (planned, not yet runnable against live stack)

```bash
cd apps/qa
./ci/scripts/run-e2e.sh
./ci/scripts/run-load-500.sh
./ci/scripts/run-security-suite.sh
./ci/scripts/generate-phase1-exit-report.sh
```

### Phase 1 Exit Criteria (18 items)

Machine-readable: `apps/qa/ci/thresholds.yaml`. Highlights:
- <2% CPU, <150MB RAM on endpoint (Windows footprint bench)
- p95 dashboard query <1s
- 99.5% uptime over 30 days
- 500 endpoint pilot stable
- **#9 (BLOCKING)**: Keystroke admin-blindness red team must pass — admin cannot decrypt keystroke content via any API/role
- **#17**: ClickHouse replication staging rig validated
- **#18**: DLP opt-in ceremony end-to-end in <1 hour

---

## 9. Agent Team Workflow

Bu proje **multi-agent Claude Code workflow** ile inşa edildi. Önemli ders: her agent'ın brief'i yeterince detaylı ve context-rich olmalı. Kısa prompt'lar halüsinasyona yol açar.

### Kullanılan uzman agentlar

| Sprint | Agent | Sorumluluk |
|---|---|---|
| Faz 0 Pilot | `microservices-architect` | Mimari omurga — single source of truth |
| Faz 0 | `compliance-auditor` | KVKK 8-doc framework |
| Faz 0 | `security-engineer` | 7 güvenlik runbook'u |
| Faz 0 | `competitive-analyst` | UAM pazarı teardown |
| Faz 1 | `rust-engineer` | Agent workspace (13 crate) |
| Faz 1 | `golang-pro` | Gateway + enricher |
| Faz 1 | `backend-developer` | Admin API |
| Faz 1 | `nextjs-developer` | Admin console |
| Faz 1 | `frontend-developer` | Transparency portal |
| Faz 1 | `devops-engineer` | On-prem compose + systemd |
| Faz 1 | `test-automator` | QA framework + simulator |

### Revision rounds

Agent'lar ilk turda %85-95 doğru üretir. **Revision round discipline** ile kalan %5-15 kapatılır:
1. Tüm specialist çıktılarını oku
2. Çakışmaları identify et (cert TTL, DLP isolation mode, recording retention vb)
3. Gap'leri bayrakla (compliance §13, security concerns §4, proto gaps)
4. Architect'i tek briefle "propagate this decision" moduna sok
5. Her edit'i Edit tool ile minimal diff olarak iste

### Reality Check

Reality check ESAS TEST. Agent'lar `cargo build` / `pnpm build` / `go build` çalıştırmadan kod yazarsa compile-level hatalar kaçar. **Commit öncesi her stack'i gerçek makinada build et**. Bu commit'teki 36 hata buradan doğdu.

### Önemli agent davranışları

- **Research agent'lar** (competitive-analyst, compliance-auditor) bazen Write tool'una sahip olmaz ve içeriği inline döndürür. Parent agent bunu kaydeder. Brief yazarken bunu bekle.
- **Reasoning agent'lar** (architect, security-engineer) invariants önerir (cryptographic, structural). Bunları ADR'a yazıp CI linter ile zorla.
- **Hallucinated packages** sık karşılaşılan hata paterni: `@radix-ui/react-badge`, `@radix-ui/react-sheet` gibi benzer ama var olmayan paketler. Reality check yakalar.

### ZORUNLU: Uzman Agent Delegasyonu (2026-04-12 kuralı)

Bu proje çok katmanlı (Rust agent + Go backend + TypeScript console + Python ML
+ on-prem infra + KVKK compliance + SOC 2 evidence). Tek bir general-purpose
oturumun her katmana derinlemesine girmesi hem yavaş hem kalitesiz. Bu yüzden
Personel üzerinde çalışan her Claude Code oturumu için şu kural zorunludur:

> **Non-trivial bir iş gelirse, o işe uygun uzman agent'ı spawn et.**
> "Non-trivial" = 3+ dosya değişikliği VEYA domain bilgisi isteyen tasarım
> kararı VEYA başka bir katmanı etkileyen mimari değişim.
>
> Tek satır typo fix, config değer güncellemesi, markdown rötuşu gibi
> trivial işlerde delegasyon ZORUNLU DEĞİL — direkt yap.

#### Katman → Agent eşlemesi

| Dokunulan yer | Delegasyon |
|---|---|
| `apps/agent/` (Rust) | `voltagent-lang:rust-engineer` |
| `apps/api/` Go handler/service/domain | `voltagent-core-dev:backend-developer` veya `voltagent-lang:golang-pro` |
| `apps/gateway/` + `apps/enricher/` | `voltagent-lang:golang-pro` |
| `apps/console/` + `apps/portal/` (Next.js 15) | `voltagent-lang:nextjs-developer` |
| Reusable React komponenti / shadcn | `voltagent-lang:react-specialist` |
| `apps/ml-classifier/`, `apps/ocr-service/`, `apps/uba-detector/` | `voltagent-lang:python-pro` + `voltagent-data-ai:ai-engineer` |
| `apps/mobile-admin/` (Expo RN) | `voltagent-lang:expo-react-native-expert` |
| `infra/compose/`, Dockerfile, systemd | `voltagent-infra:devops-engineer` veya `voltagent-infra:docker-expert` |
| Vault, PKI, mTLS, anti-tamper | `voltagent-qa-sec:security-auditor` + `voltagent-infra:security-engineer` |
| KVKK compliance, DPIA, aydınlatma | `voltagent-qa-sec:compliance-auditor` |
| SOC 2 Type II control, evidence, policy | `voltagent-qa-sec:compliance-auditor` + `voltagent-meta:workflow-orchestrator` |
| ClickHouse schema / query optimize | `voltagent-data-ai:database-optimizer` veya `voltagent-data-ai:postgres-pro` |
| Postgres migration / index / perf | `voltagent-data-ai:postgres-pro` |
| Mimari karar / ADR / bounded context | `voltagent-core-dev:microservices-architect` |
| API contract / OpenAPI | `voltagent-core-dev:api-designer` |
| Test suite / QA framework | `voltagent-qa-sec:qa-expert` + `voltagent-qa-sec:test-automator` |
| Threat model / pentest / red team | `voltagent-qa-sec:penetration-tester` + `voltagent-qa-sec:security-auditor` |
| Performance / load test / bottleneck | `voltagent-qa-sec:performance-engineer` |
| Kod review (PR, commit öncesi) | `pr-review-toolkit:code-reviewer` + `pr-review-toolkit:silent-failure-hunter` |
| Refactor, code smell, duplication | `voltagent-dev-exp:refactoring-specialist` veya `code-simplifier:code-simplifier` |
| Dashboard / görsel UI tasarım | `voltagent-core-dev:ui-designer` veya `voltagent-core-dev:design-bridge` |
| Broad araştırma / codebase exploration | `Explore` agent (quick/medium/very thorough) |
| Çok katmanlı plan (3+ domain) | `voltagent-meta:agent-organizer` — uygun team'i seçip koordine eder |

#### Paralel delegasyon

Birden fazla bağımsız sorun varsa tek mesajda paralel spawn et. Örnek: bir
feature hem Go API hem Next.js console hem postgres migration gerektiriyorsa
üç agent'ı aynı anda brief'le, sonuçları topla, entegre et.

#### Ne zaman delegasyon YAPMA

- Bildiğin dosyada tek satır değişiklik
- Build hatası mesajını direkt fix'leme
- git commit / push
- Bash komutu çalıştırma / docker restart
- Kullanıcıyla diyalog, plan tartışması, progress raporu
- Kullanıcı açıkça "kendin yap" dediğinde

#### Brief yazma disiplini

Agent'ın senin konuşmanı görmediğini unutma. Brief'te şunlar olmalı:

1. **Goal + why**: ne yapılacak ve neden önemli (KVKK? compliance? pazar?)
2. **Context**: hangi dosyalar, hangi sistem, hangi katman
3. **Constraints**: locked decisions (§6), ADR'lar, KVKK kuralları (§11)
4. **Deliverable shape**: dosya listesi / fonksiyon imzası / test kriteri
5. **Kısa yanıt sınırı**: gerektiğinde "rapor 200 kelime altı" de

Kötü brief: "api'ye endpoint ekle"
İyi brief: "apps/api/internal/user/employee_detail.go içine GET
/v1/employees/{id}/detail handler'ı ekle. Dönüşü: profile + today's daily
stats + 24h hourly array + last 7 days + assigned endpoints. RBAC:
canViewEmployees listesi (admin/dpo/hr/manager/it_manager/it_operator/
investigator/auditor). Postgres source: employee_daily_stats +
employee_hourly_stats (migration 0027). Testler integration/ altında.
Commit atma — parent yapacak."

---

## 10. Known Tech Debt (Faz 1 Polish Listesi)

Faz 1 Reality Check sonrası kalan açık maddeler — polish sprint için:

### Compliance & Legal

- [ ] `docs/compliance/dlp-opt-in-form.md` — ADR 0013'ün referans ettiği imzalı form template'i, compliance-auditor tarafından yazılmalı
- [~] **Postgres audit trigger bypass riski**: DBA superuser `ALTER TABLE ... DISABLE TRIGGER` yapabilir. `audit.WORMSink` (MinIO Object Lock Compliance mode) Phase 3.0'da devreye alındı; daily checkpoint'ler WORM'a yazılıyor, evidence locker da aynı bucket'ı paylaşıyor. Kalan açık: audit_log *entry-level* WORM mirror (şu an sadece günlük checkpoint; ara saatlerde DBA manipülasyonu bir sonraki checkpoint'e kadar tespit edilemez). Entry-level mirror veya daha sık checkpoint cadence kararı bekliyor.
- [ ] Schema ownership dokümantasyonu: API migration 0001 `init.sql` baseline varsayıyor mu yoksa idempotent mi oluşturuyor? README netleştirmesi.

### Backend (Admin API)

### Infra

- [ ] `infra/scripts/dlp-enable.sh` — ADR 0013 opt-in ceremony script (write, rollback semantics per A3)
- [ ] `infra/scripts/dlp-disable.sh` — ADR 0013 opt-out (A4: don't destroy ciphertext, let TTL age out)
- [ ] `infra/compose/docker-compose.yaml`: DLP service'e `profiles: [dlp]` ekle
- [ ] `infra/install.sh`: Vault AppRole oluşturulur ama Secret ID issue edilmez (A2)
- [ ] NATS JetStream at-rest encryption baseline doğrulama (security-engineer open concern #6)
- [ ] Reproducible build pipeline for Rust agent on Windows
- [ ] Vault Enterprise budget kararı (HSM unseal gerekirse)

### QA

- [ ] Phase 1 exit criterion #17 test: ClickHouse replication staging rig end-to-end
- [ ] Phase 1 exit criterion #18 test: DLP opt-in ceremony end-to-end (yeni, ADR 0013)
- [ ] `apps/qa/test/e2e/dlp_opt_in_test.go` — yeni test dosyası
- [ ] Keystroke admin-blindness red team testinin gerçek stack ile koşturulması

### UI Polish

- [ ] Portal `/public/fonts/inter-var.woff2` self-hosted font dosyası commit edilmemiş (frontend-developer bayrakladı)
- [ ] `exactOptionalPropertyTypes: true` geri alınması ve tüm call-site'ların düzgün düzeltilmesi (reality check'te `false` yapıldı — pragmatik tech debt)
- [ ] Next.js `typedRoutes` geri alınması ve typed route helpers yazılması
- [ ] Inter font self-host, portal `globals.css` placeholder düzelt
- [ ] `next-intl` ve `next` güvenlik patch güncelleme (CVE-2025-66478)

### Rust Agent

- [ ] `missing_docs` lint'in `deny`'e geri alınması ve her pub field'a doc eklenmesi
- [ ] Windows personel-os crate'lerinin gerçek Windows CI runner'da build testi
- [ ] ETW collectors gerçek implementation (şu an stub)
- [ ] DXGI screen capture gerçek implementation
- [ ] WFP user-mode network flow monitoring gerçek implementation
- [ ] Phase 2: macOS/Linux stub implementations → real
- [ ] Policy engine: ADR 0013 `dlp_enabled=false AND keystroke.content_enabled=true` invariant'ı runtime ve sign-time reject

### Cross-stack

- [ ] ADR 0013 A1-A5 amendment item'larının tam implementasyonu (PE-DEK bootstrap, rollback, rules enforcement)
- [ ] Compliance docs ile architecture docs arasında kalan küçük tutarsızlıkların taranması
- [ ] Secret rotation otomasyonu (GPG backup key, signing keys)

---

## 11. Hukuki Bağlam — KVKK

Personel'in her mühendislik kararı KVKK bağlamıyla entegre tasarlanmıştır. Yeni kod yazarken bu dokümanları mutlaka oku:

| Konu | Doküman |
|---|---|
| **Ana çerçeve** | `docs/compliance/kvkk-framework.md` (15 bölüm, TR) |
| **Çalışan aydınlatma metni** | `docs/compliance/aydinlatma-metni-template.md` |
| **Açık rıza (sınırlı kullanım)** | `docs/compliance/acik-riza-metni-template.md` |
| **DPIA şablonu** | `docs/compliance/dpia-sablonu.md` |
| **VERBİS kayıt rehberi** | `docs/compliance/verbis-kayit-rehberi.md` |
| **Saklama ve imha politikası** | `docs/compliance/iltica-silme-politikasi.md` |
| **Hukuki risk register** | `docs/compliance/hukuki-riskler-ve-azaltimlar.md` (13 risk) |
| **Bilgilendirme akışı** | `docs/compliance/calisan-bilgilendirme-akisi.md` (state machine) |

### Kritik kurallar (kod yazarken her zaman geçerli)

1. **Hiçbir endpoint ham klavye içeriği döndüremez** — `apps/api/` CI linter bu kuralı zorlamalı
2. **Her admin mutasyonu audit log'a yazılmalı** — `internal/audit/recorder.go` zorunlu middleware
3. **Screen capture özel nitelikli veri filtrelerini respect etmeli** — `screenshot_exclude_apps` policy (Gap 1)
4. **DLP varsayılan KAPALI** — enable sadece `infra/scripts/dlp-enable.sh` ile, UI'dan bypass yok (ADR 0013)
5. **Live view dual-control enforced** — hem API hem UI tarafında `approver ≠ requester` check
6. **Hash-chain audit append-only** — app role `INSERT + SELECT` only; `UPDATE/DELETE` revoke edilmeli

---

## 12. İlk Kez Bu Repo'ya Giriyor musun?

İş önceliğine göre önerilen okuma sırası:

### Ürün / Strateji / Karar verme
1. Bu dosya (`CLAUDE.md`)
2. `docs/product/competitive-analysis.md`
3. `docs/architecture/overview.md` (Turkish exec summary)
4. `docs/adr/0013-dlp-disabled-by-default.md` (en güncel kritik karar)

### Backend geliştirme
1. Bu dosya
2. `docs/architecture/c4-container.md`
3. `docs/architecture/bounded-contexts.md`
4. `docs/architecture/event-taxonomy.md`
5. `apps/api/api/openapi.yaml` (API contract)
6. `proto/personel/v1/*.proto`

### Frontend geliştirme
1. Bu dosya
2. `apps/api/api/openapi.yaml` (contract)
3. `apps/console/messages/tr.json` (localization model)
4. `docs/compliance/calisan-bilgilendirme-akisi.md` (UI akış state machine)

### Güvenlik / Compliance
1. Bu dosya
2. `docs/compliance/kvkk-framework.md`
3. `docs/security/threat-model.md`
4. `docs/security/runbooks/dlp-service-isolation.md`
5. `docs/architecture/key-hierarchy.md`
6. `docs/adr/0009-keystroke-content-encryption.md` + `0013-dlp-disabled-by-default.md`

### DevOps / SRE
1. Bu dosya
2. `infra/runbooks/install.md`
3. `docs/security/runbooks/pki-bootstrap.md`
4. `docs/security/runbooks/vault-setup.md`
5. `docs/security/runbooks/incident-response-playbook.md`

### Rust agent geliştirme
1. Bu dosya
2. `docs/architecture/agent-module-architecture.md`
3. `docs/architecture/key-hierarchy.md`
4. `docs/security/anti-tamper.md`
5. `apps/agent/Cargo.toml` + crate README'leri

---

## 13. Önemli Not — Gelecek Claude Oturumları İçin

Bu repo'da çalışırken:

1. **Her zaman önce mimari/ADR'yi oku**. Kod agent'lar için yazıldı ama kararlar insan için alındı.
2. **Locked decisions'a dokunma** — değiştirmek yeni ADR gerektirir. 7 decision + ADR 0013 kutsaldır.
3. **KVKK compliance'ı sonradan düşünme** — her yeni feature için ilk soru "bu KVKK m.5/m.6/m.11 açısından ne anlama geliyor?" olmalı.
4. **Reality check'i ihmal etme** — agent çıktısı %85-95 doğru, kalan %5-15 build time'da ortaya çıkar. `go build`, `cargo check`, `pnpm build` koşmadan PR kapatma.
5. **Tech debt listesini güncelle** — yeni borç oluşturduysan bu dosyanın §10'una ekle.

---

*Versiyon 1.3 — Faz 3.0 evidence substrate complete: dual-write locker + Vault
signer + 3 collector (liveview, policy, DSR) + coverage endpoint + pack export.
Güncelleme: her major milestone sonrası.*
