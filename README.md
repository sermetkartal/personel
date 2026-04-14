<div align="center">

# Personel

**Kurumsal Çalışan Aktivite İzleme ve Performans Analitiği Platformu**
*Corporate Employee Monitoring & Performance Analytics Platform*

KVKK-native · On-prem-first · Kriptografik Çalışan Gizliliği · Türkçe-first

![CI](https://github.com/sermetkartal/personel/actions/workflows/ci.yml/badge.svg)
![Phase](https://img.shields.io/badge/phase-17%20%E2%80%94%20polish-brightgreen)
![Roadmap](https://img.shields.io/badge/roadmap-155%2F190-brightgreen)
![License](https://img.shields.io/badge/license-proprietary-red)
![Language](https://img.shields.io/badge/rust-1.88-orange)
![Language](https://img.shields.io/badge/go-1.22%2B-blue)
![Language](https://img.shields.io/badge/typescript-5-blue)
![Language](https://img.shields.io/badge/next.js-15-black)
![KVKK](https://img.shields.io/badge/KVKK-6698-brightgreen)
![Stars](https://img.shields.io/github/stars/sermetkartal/personel?style=social)
![Issues](https://img.shields.io/github/issues/sermetkartal/personel)

</div>

---

## 🇹🇷 Personel Nedir?

**Personel**, Türkiye pazarı için sıfırdan tasarlanmış, **KVKK-uyumlu**, **on-prem öncelikli**, kurumsal çalışan aktivite izleme (UAM) ve performans analitiği platformudur. Teramind, ActivTrak, Veriato, Insightful ve Safetica gibi uluslararası rakiplerle doğrudan yarışır — ancak onların hiçbirinin çözemediği üç cephede farklılaşır:

1. **KVKK mimari seviyesinde çözüldü**, sonradan eklenmedi
2. **Klavye içeriği kriptografik olarak yöneticilerin erişiminin dışındadır** — politika değil, matematik
3. **Her canlı ekran izleme oturumu HR çift kontrolüne tabidir** ve değiştirilemez denetim defterine yazılır

### Hedef Müşteri

- **Sektör**: Bankacılık (BDDK), telekom, manifatura, profesyonel hizmetler, kamu-bitişik kurumlar
- **Ölçek**: 200–2.000 çalışan, Türkiye merkezli
- **Alıcı persona**: BT Güvenlik Müdürü, CISO, DPO (KVKK sorumlusu), İK Direktörü, CFO
- **Deployment**: Müşterinin kendi veri merkezi (on-prem). Veri hiçbir zaman Personel firması altyapısına akmaz.

---

## ✨ Neden Personel?

### 🛡️ KVKK-Native Uyum (Regulatory Moat)

Hiçbir uluslararası rakip bunu mimari seviyede yapmıyor:

- **VERBİS export**: Veri işleme envanteri tek tıkla
- **Otomatik saklama matrisi**: 36 olay türü × KVKK maddesi × TTL × silme yöntemi
- **Şeffaflık Portalı**: m.10 aydınlatma + m.11 hak kullanımı çalışan self-service
- **Hash-zincirli audit**: m.12 hesap verebilirlik ilkesi için değiştirilemez log
- **DPIA şablonları**, **aydınlatma metni şablonu**, **imha politikası şablonu** — hepsi hazır ve Türkçe

### 🔐 Kriptografik Çalışan Gizliliği (ADR 0013)

Klavye içeriği yakalanır ama **varsayılan olarak KAPALIDIR**. Etkinleştirmek için müşteri:
1. DPIA amendment yapar
2. DPO + Hukuk + BT Güvenlik imzalı onay verir
3. Vault Secret ID issue edilir
4. DLP container başlatılır (izole, distroless + seccomp + AppArmor + read-only FS)
5. Çalışanlar şeffaflık portalında etkinleştirme banner'ını görür
6. Audit checkpoint'e hash-zincirli kayıt düşer

**Güvence**: Enable edilene kadar hiçbir süreç anahtar türetmiyor. Vault audit log'u sıfır derive çağrısı gösterir → "hiçbir zaman okunmadı" kriptografik olarak ispatlanabilir.

### 👁️ HR-Gated Canlı İzleme

Rakiplerde yönetici tek tıkla canlı izleme başlatır. Personel'de:

- Talep gerekçe kodu ile açılır (soruşturma/olay ID)
- HR rolü onaylar (**approver ≠ requester** sunucu tarafında zorlanır)
- LiveKit WebRTC bağlantısı kurulur (15 dk max, uzatma için yeni onay)
- Her durum geçişi append-only hash-zincirli audit log'a yazılır
- DPO her an sonlandırabilir
- Çalışan şeffaflık portalında kendi geçmiş oturumlarını görür (varsayılan açık)

### 📊 Düşük Endpoint Ayak İzi

Rust agent. Hedef: **<%2 CPU, <150 MB RAM** 500 endpoint'lik pilot dağıtımda.

### 🏗️ Modern On-Prem Stack

Docker Compose + systemd. ClickHouse (rakiplerin SQL Server'ının 10-30 katı sıkıştırma), Vault (anahtar yönetimi), NATS JetStream (at-rest encryption), Next.js 15 (modern UI). **2 saat hedef kurulum süresi** — rakiplerde haftalar.

---

## 🏛️ Mimari Bakış

```
┌─────────────────────────────────────────────────────────────────┐
│ ENDPOINT (Windows)                                              │
│ Rust Agent → Collectors → Encrypted SQLite Queue → gRPC bidi    │
└────────────────┬────────────────────────────────────────────────┘
                 │ mTLS + gRPC bidi + Key Version Handshake
                 ▼
┌─────────────────────────────────────────────────────────────────┐
│ INGEST TIER                                                     │
│ • Gateway (Go): mTLS auth, rate limit, backpressure, Flow 7     │
│ • Enricher (Go): NATS consumer, sensitivity routing             │
└────────────────┬────────────────────────────────────────────────┘
                 ▼
┌─────────────────────────────────────────────────────────────────┐
│ STORAGE TIER                                                    │
│ PostgreSQL │ ClickHouse │ MinIO │ OpenSearch │ Vault │ Keycloak │
└────────────────┬────────────────────────────────────────────────┘
                 ▼
┌─────────────────────────────────────────────────────────────────┐
│ CONTROL PLANE                                                   │
│ • Admin API (Go + chi): 57-op OpenAPI, RBAC, audit, DSR, live   │
│ • DLP Service (isolated, off by default — ADR 0013)             │
└─────┬──────────────────────────────────────┬────────────────────┘
      ▼                                      ▼
┌──────────────────┐                 ┌──────────────────────┐
│ Admin Console    │                 │ Transparency Portal  │
│ (Next.js 15)     │                 │ (Next.js 15)         │
│                  │                 │                      │
│ Admin/HR/DPO/    │                 │ Çalışan self-service │
│ Manager/Auditor  │                 │ KVKK m.10/m.11       │
│ Investigator     │                 │ Trust-first UX       │
└──────────────────┘                 └──────────────────────┘
```

**Detaylı diyagramlar**: [`docs/architecture/c4-context.md`](docs/architecture/c4-context.md) · [`docs/architecture/c4-container.md`](docs/architecture/c4-container.md) · [`docs/architecture/bounded-contexts.md`](docs/architecture/bounded-contexts.md)

---

## ✅ Özellikler

### Çalışan Faaliyet İzleme (36 Olay Türü)

- Süreç / uygulama kullanımı (başlama, durma, ön plan değişikliği)
- Pencere başlığı + aktif uygulama
- Ekran görüntüsü (periyodik + olay tetiklemeli) + kısa ekran video klipleri
- Dosya sistemi olayları (oluştur, oku, yaz, sil, kopyala, taşı) — ETW
- USB / harici aygıt takma/çıkarma + politika bloklamaları
- Ağ akış özetleri (WFP) + DNS + TLS SNI
- Pano metadata + şifrelenmiş içerik (DLP için)
- Klavye istatistikleri + şifrelenmiş içerik (DLP için, ADR 0013)
- Yazıcı işi metadata
- Oturum lock/unlock, idle/active, oturum süresi
- Canlı izleme denetim olayları

### KVKK İşletim

- **Veri Sahibi Başvuruları (DSR)** — m.11 hakları, 30 gün SLA, DPO dashboard
- **Legal Hold** — dar kapsamlı hukuki bekletme, DPO-only
- **Periyodik İmha Raporu** — 6 aylık otomatik, imzalı PDF (Vault control-plane key ile)
- **Forensic Export** — KVKK m.12/5 ihlal bildirimi için 72 saat hazırlığı
- **Özel Nitelikli Veri Filtresi** — m.6 için `screenshot_exclude_apps` ve `window_title_sensitive_regex` politika kontrolleri
- **Kısaltılmış retention** — hassas bayraklı kayıtlar için ayrı TTL bucket'ı

### Güvenlik

- **mTLS** + sertifika sabitleme (14 gün agent cert TTL)
- **HashiCorp Vault** — TMK `exportable: false`, Shamir 3-of-5 unseal
- **Hash-zincirli audit log** — append-only, nightly external checkpoint
- **DLP izolasyon** — distroless container + seccomp + AppArmor + ptrace_scope=3
- **Dual-signed auto-update** — Ed25519 + EV code signing + watchdog-supervised rollback
- **Anti-tamper** — watchdog + DPAPI + TPM-bound keystore + self-integrity check
- **RBAC** — 7 rol (Admin, Manager, HR, DPO, Investigator, Auditor, Employee)
- **Row-Level Security** — PostgreSQL RLS for multi-tenant isolation

### Admin Console

- Dashboard (Flow 7 agent silence, DSR queue, live view approvals)
- Endpoint listesi + detay + timeline + ekran görüntüleri
- KVKK DSR workflow (DPO dashboard, SLA timeline, artifact upload)
- Legal Hold yönetimi
- Canlı izleme request + HR approval + LiveKit viewer (dual-control enforced)
- Hash-zincirli audit log viewer + chain integrity status
- Policy editor (SensitivityGuard + DLP state)
- DLP ceremony explainer sayfası (**enable butonu yok** — bypass imkansız)
- 6-aylık imha raporları
- Reports (productivity, top apps, idle-active, blocking events)
- TR/EN tam i18n

### Transparency Portal (Çalışan Self-Servisi)

- **Anasayfa** — KVKK bilgilendirme özeti
- **Aydınlatma metni** — iki sütun (hukuki dil + sade Türkçe açıklama)
- **Verilerim** — 11 veri kategorisi kartı + bu ayın toplanan veri özeti
- **Neler izlenmiyor** — 10 somut madde (güven inşa eden sayfa, rakiplerde yok)
- **Canlı izleme politika + geçmiş oturum listesi**
- **KVKK m.11 hak başvurusu** formu (erişim, düzeltme, silme, itiraz)
- **Başvurularım** — SLA timeline ile
- **DLP durumu** — ADR 0013 anlatımı (kapalı/açık)
- **İletişim** — DPO, Kurul başvuru yolu
- İlk giriş zorunlu audit'li bilgilendirme modalı

---

## 🧰 Teknoloji Stack'i

| Katman | Teknoloji |
|---|---|
| Endpoint Agent | **Rust** 1.88+ · tokio · tonic · rustls · rusqlite (SQLCipher) · aes-gcm · x25519-dalek · ed25519-dalek · windows crate |
| Ingest / Server | **Go** 1.22+ · gRPC · NATS JetStream · ClickHouse · koanf · chi · testcontainers-go |
| Storage | **PostgreSQL** 16 · **ClickHouse** 24 · **MinIO** · **OpenSearch** 2.x · **HashiCorp Vault** 1.15.6 |
| Identity | **Keycloak** 24 (OIDC/SAML/SCIM) |
| Real-Time | **LiveKit** (WebRTC SFU, self-hosted) |
| Admin UI | **Next.js** 15 · React 18 · TypeScript 5 · Tailwind 3 · shadcn/ui · TanStack Query · next-intl |
| Observability | OpenTelemetry · Prometheus · Grafana · structured slog |
| Deployment | Docker Compose v2 · systemd · idempotent bash installer |

---

## 🚀 3-Command Quick Start (pilot host)

```bash
git clone https://github.com/sermetkartal/personel.git && cd personel
sudo infra/install.sh                                       # ~2h idempotent bring-up
# Then on a Windows endpoint:
msiexec /i personel-agent.msi ENROLL_TOKEN="$(curl -s -X POST $API/v1/endpoints/enroll -H "Authorization: Bearer $JWT" | jq -r .token)"
```

Validation after install:

```bash
infra/scripts/final-smoke-test.sh --api-url=http://$HOST:8000 \
  --admin-token="$ADMIN_JWT"
```

Hit `overall: pass`? You are ready to run Senaryo 1 of
[`docs/operations/pilot-walkthrough.md`](docs/operations/pilot-walkthrough.md).

## 🚀 Quickstart (English, component-by-component)

### Prerequisites

- **Go 1.22+** (tested with 1.26)
- **Rust 1.88+** via rustup (pinned in `apps/agent/rust-toolchain.toml`)
- **Node 20+** and **pnpm 9+**
- **Docker 25+** and Docker Compose v2
- **protoc** (Protocol Buffers compiler)

### Clone

```bash
git clone https://github.com/sermetkartal/personel.git
cd personel
```

### Generate Proto Stubs (Go)

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.33.0
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.3.0

mkdir -p apps/gateway/pkg/proto/personel/v1
protoc \
  --proto_path=proto \
  --go_out=apps/gateway/pkg/proto \
  --go_opt=paths=source_relative \
  --go-grpc_out=apps/gateway/pkg/proto \
  --go-grpc_opt=paths=source_relative \
  proto/personel/v1/*.proto
```

### Build Each Component

```bash
# Go services
(cd apps/gateway && go mod tidy && go build ./...)
(cd apps/api     && go mod tidy && go build ./...)
(cd apps/qa      && go mod tidy && go build ./...)

# Rust agent (cross-platform library crates)
(cd apps/agent   && cargo check -p personel-core -p personel-crypto \
                               -p personel-queue -p personel-policy)

# Next.js apps
(cd apps/console && pnpm install && pnpm build)
(cd apps/portal  && pnpm install && pnpm build)
```

### Run Dev Servers

```bash
# Admin console on :3000 (default)
(cd apps/console && pnpm dev)

# Transparency portal on :3001 (default)
(cd apps/portal  && pnpm dev)
```

> **Note**: In dev mode, OIDC login will fail without the full stack running. Public pages (aydınlatma, neler-izlenmiyor, dlp-durumu, haklar) are browsable without authentication.

### Full Stack (Docker Compose) — planned

```bash
cd infra/compose
cp .env.example .env
# Edit .env — fill every CHANGEME value

sudo infra/install.sh   # idempotent; runs preflight, Vault unseal
                        # ceremony, migrations, smoke tests
```

> ⚠️ The full installer has not been validated end-to-end yet. See [CLAUDE.md §10](CLAUDE.md#10-known-tech-debt-faz-1-polish-listesi) for the known-issues list.

---

## 📁 Repository Structure

```
personel/
├── CLAUDE.md                ← MUST READ: full project context
├── docs/                    ← 47 documents
│   ├── architecture/        ← C4, bounded contexts, key hierarchy, retention
│   ├── compliance/          ← KVKK framework, templates, DPIA, VERBİS
│   ├── security/            ← threat model, 7 runbooks, security decisions
│   ├── product/             ← competitive analysis
│   └── adr/                 ← 13 Architecture Decision Records
├── proto/personel/v1/       ← 5 protobuf contracts (source of truth)
├── apps/
│   ├── agent/               ← Rust Windows agent (13-crate workspace)
│   ├── gateway/             ← Go ingest gateway + enricher
│   ├── api/                 ← Go admin API (57-op OpenAPI 3.1)
│   ├── console/             ← Next.js 15 admin console
│   ├── portal/              ← Next.js 15 employee transparency portal
│   └── qa/                  ← QA framework + 10K-agent simulator
└── infra/                   ← Docker Compose + systemd on-prem deploy
```

**Detailed layout with per-directory descriptions**: see [`CLAUDE.md §3`](CLAUDE.md#3-repository-layout).

---

## 🗺️ Roadmap / Phase Status

**190-item production roadmap — 155 items complete as of 2026-04-14**
(see CLAUDE.md §0 for per-item tracking).

| Faz | Durum | Kapsam |
|---|---|---|
| **Phase 0** — Mimari omurga | ✅ Complete | 11 arch docs, 13 ADRs, 5 protos |
| **Phase 0.5** — KVKK + Security + Competitive | ✅ Complete | 8 compliance + 7 runbook + competitive teardown |
| **Phase 0.6** — ADR 0013 DLP-off-default | ✅ Complete | Propagation across 11 docs + proto |
| **Phase 1** — Setup + critical bring-up (#1-6) | ✅ Complete | PKI, enroll ceremony, mTLS + event flow |
| **Phase 2** — Agent collectors (#7-20) | ✅ Complete | 14 Windows collectors, 131 new unit tests |
| **Phase 3** — Screen capture hardening (#21-28) | ✅ Complete | Multi-monitor, adaptive freq, WebP, delta |
| **Phase 4** — Agent stability (#29-40) | ✅ Complete | Anti-tamper, OTA, throttle, GPO, signing scaffold |
| **Phase 5** — Backend hardening (#41-58) | ✅ Deployed | vm3+vm5 cluster: postgres replica, NATS R=2, MinIO mirror, ClickHouse 2-node + 2 keeper, OpenSearch 2-node, Keycloak HA |
| **Phase 6** — API completeness (#62-72) | ✅ Complete | Enroll refresh, wipe, bulk, audit stream, search, DSR workflow, rate limit |
| **Phase 7** — Data pipeline (#73-80) | ✅ Complete | Schema versioning, DLQ, replay, tiering, compression, dedup, DQM |
| **Phase 8** — ML / Analytics (#81-89) | ✅ Complete | Fallback classifier, OCR pipeline, UBA, reports |
| **Phase 9** — Console UI (#90-102) | ✅ Complete | Endpoint mgmt, live view, audit search, DSR, settings, real-time, i18n |
| **Phase 10** — Employee Portal (#103-108) | ✅ Complete | Şeffaflık portalı final |
| **Phase 11** — KVKK / Compliance (#109-120) | ✅ Complete | VERBİS, DPIA, retention enforcement, DSR e2e |
| **Phase 12** — Security (#121-132) | ✅ Scaffolded | SBOM, Trivy, SAST, branch protection (pentest + audit AWAITING 3rd party) |
| **Phase 13** — Infrastructure (#133-145) | ✅ Complete | install.sh hardening, monitoring stack, firewall, bastion, VPN |
| **Phase 14** — Testing (#146-156) | ✅ Complete | Unit + integration + E2E + load + chaos + smoke + regression |
| **Phase 15** — Documentation (#157-167) | ✅ Complete | Install guide, ops runbook, API docs, user manuals, IR playbook |
| **Phase 16** — CI/CD (#168-176) | ✅ Complete | Matrix builds, image signing, MSI sign, release automation |
| **Phase 17** — Customer Success (#177-190) | ✅ Complete | Sales materials, POC, demo deck, pilot walkthrough, final smoke test |
| **Phase 1 Pilot Launch** | 🔜 Ready | 500 endpoint customer pilot |

**Still pending** (human-in-the-loop blockers, see CLAUDE.md §0 "AWAITING
CUSTOMER ACTION"): EV code signing cert purchase, third-party pentest
contract, code audit contract, DPA review lawyer, VERBİS registration,
Faz 5 operator deployment handoff, restore drill RTO/RPO measurement.

---

## 📚 Documentation

### İlk Okuma (Role-Based)

- **Yeni katkıda bulunan (developer)**: [`CLAUDE.md`](CLAUDE.md) → `docs/architecture/c4-container.md` → `apps/api/api/openapi.yaml`
- **Güvenlik mühendisi**: `docs/compliance/kvkk-framework.md` → `docs/security/threat-model.md` → `docs/architecture/key-hierarchy.md` → `docs/adr/0013-dlp-disabled-by-default.md`
- **DevOps / SRE**: `infra/runbooks/install.md` → `docs/security/runbooks/pki-bootstrap.md` → `docs/security/runbooks/vault-setup.md`
- **Rust agent geliştirme**: `docs/architecture/agent-module-architecture.md` → `docs/architecture/key-hierarchy.md` → `docs/security/anti-tamper.md`
- **Frontend**: `apps/api/api/openapi.yaml` → `apps/console/messages/tr.json` → `docs/compliance/calisan-bilgilendirme-akisi.md`

### Ana Belgeler

| Belge | Açıklama |
|---|---|
| [`CLAUDE.md`](CLAUDE.md) | Tüm proje context'i, build komutları, tech debt, agent workflow |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | Branch + commit + PR + review disiplini |
| [`docs/README.md`](docs/README.md) | 80+ belgelik ikili dilde doküman indeksi |
| [`docs/architecture/overview.md`](docs/architecture/overview.md) | Türkçe yönetici özeti |
| [`docs/compliance/kvkk-framework.md`](docs/compliance/kvkk-framework.md) | 15 bölümlü KVKK uyum çerçevesi |
| [`docs/operations/pilot-walkthrough.md`](docs/operations/pilot-walkthrough.md) | 6 senaryo pilot demo akışı (90 dk) |
| [`docs/operations/installation-guide.md`](docs/operations/installation-guide.md) | Kurulum kılavuzu |
| [`docs/operations/ops-runbook.md`](docs/operations/ops-runbook.md) | İşletim runbook'u |
| [`docs/product/competitive-analysis.md`](docs/product/competitive-analysis.md) | Teramind/ActivTrak/Safetica vs teardown |
| [`docs/adr/`](docs/adr/) | 13 Architecture Decision Record |
| [`docs/policies/`](docs/policies/) | ISO 27001 / SOC 2 Type II policy suite |
| [`infra/runbooks/final-smoke-test.md`](infra/runbooks/final-smoke-test.md) | Sürüm öncesi 10 dakikalık tam yığın doğrulaması |

---

## 🔒 Güvenlik ve Uyum Beyanı

Personel **KVKK 6698 sayılı Kanun** ile tam uyum için tasarlanmıştır. Ürünün her mühendislik kararı şu prensiplere bağlıdır:

- **Veri ikametgahı**: On-prem dağıtım. Veri hiçbir zaman Personel firması altyapısına akmaz.
- **Veri sorumlusu / yazılım sağlayıcı ayrımı**: Personel firması KVKK anlamında **veri işleyen değildir**; yalnızca yazılım sağlayıcıdır. Müşteri kurum veri sorumlusudur. (Bkz. `docs/compliance/kvkk-framework.md` §3.)
- **Meşru menfaat**: Açık rıza yerine meşru menfaat (m.5/2-f) + sözleşmenin ifası (m.5/2-c) temel hukuki dayanaklardır. İşveren-çalışan güç asimetrisinde açık rızanın hukuken zayıf olduğu kabul edilmiştir.
- **Özel nitelikli veri (m.6)**: Amaçlı toplanmaz. Kazara toplama riski `screenshot_exclude_apps` ve pencere başlığı regex filtreleri ile azaltılır. Kısaltılmış retention + dört göz erişim kontrolü uygulanır.
- **Şeffaflık**: Her çalışanın kendi verisine erişimi, düzeltme ve silme talep etme hakkı (KVKK m.11) şeffaflık portalı üzerinden aktiftir.

**Güvenlik ihlali bildirimi**: `security@personel.example` (placeholder). 72 saat içinde yanıt.

---

## 🧑‍💻 Contributing

Bu repository **özel (proprietary)** ticari bir ürünün kaynağıdır. Şu anda
dış katkıya kapalıdır. Takım üyeleri için tam rehber:
[`CONTRIBUTING.md`](CONTRIBUTING.md).

Özetle:

1. Her PR'da ilgili ADR'ye referans ver. Yeni mimari karar → yeni ADR.
2. `go vet` + `cargo clippy` + `pnpm lint` + `pnpm type-check` geçmeli.
3. **Audit-before-side-effect** kuralı: her admin endpoint mutasyonu önce `audit.append_event` çağırmalı.
4. **Keystroke içeriği dönen hiçbir RPC veya endpoint eklenemez** — CI linter kuralı zorlar.
5. **DLP ceremony UI'dan bypass edilemez** — "Enable DLP" butonu console'a eklenemez.
6. Her major değişiklik öncesi `CLAUDE.md §13`'ü oku.

---

## 📜 License

**Proprietary — All rights reserved.**

© 2026 Sermet Kartal. Bu yazılımın tüm hakları saklıdır. İzinsiz kopyalama, dağıtma, tersine mühendislik yapma veya ticari olarak kullanma yasaktır.

**Sales & Licensing**: `sales@personel.local` *(placeholder — update when real domain exists)*

---

<div align="center">

**Personel** — *KVKK-first UAM. Built for Turkish enterprises, built to compete globally.*

[Documentation](CLAUDE.md) · [Architecture](docs/architecture/) · [KVKK Framework](docs/compliance/kvkk-framework.md) · [ADRs](docs/adr/)

</div>
