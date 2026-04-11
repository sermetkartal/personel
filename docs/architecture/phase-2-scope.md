# Faz 2 Kapsamı — Personel Platformu

> Language: Turkish executive summary + English technical sections.
> Status: DRAFT — planning artifact only. No implementation authorized by this document.
> Version: 0.1 — 2026-04-10
> Supersedes: nothing. Extends `docs/architecture/mvp-scope.md` Phase 1 scope.

---

## Bölüm A — Yönetici Özeti (Türkçe)

### Faz 2'nin Amacı

Faz 1 pilotu, Türkiye pazarında **KVKK-native, on-prem, Windows-odaklı** bir UAM ürünü olarak Personel'in mimari ve hukuki tezini kanıtlamak üzere tasarlandı. Faz 2'nin amacı pivot yapmak değil, **Faz 1 tezini genişletmektir**:

1. **Platform kapsamını genişlet**: macOS ve Linux endpoint'leri ekleyerek Türk kurumsal müşterilerinin homojen olmayan filolarında tek ürün olarak konumlanmak.
2. **Zekâ katmanını ekle**: OCR + yerel LLM + UBA; rakiplerle (Teramind, Veriato) özellik matrisinde boşlukları kapatmak. Hiçbirini bulut LLM'e yaptırmadan, veri Türkiye'de kalırken.
3. **Entegrasyon yüzeyini aç**: HRIS, SCIM, SIEM. Faz 1'deki "adası olmayan ürün" algısını kır; müşteri BT yığınının bir parçası olmayı kolaylaştır.
4. **Operasyonel olgunluk**: Mobil admin app, canlı izleme kaydı (ADR 0012'nin Faz 2 envelop'u), gelişmiş audit ve teslim kanalları.

### Faz 2 Dışında Kalanlar (Faz 3'e Bırakılanlar)

- **SaaS/multi-tenant aktif kullanımı**: Faz 2 hâlâ single-tenant on-prem'dir.
- **Kubernetes/Helm**: Faz 2 hâlâ Docker Compose + systemd'dir.
- **Kernel minifilter driver (Windows)**: Faz 3'e ertelendi (ADR 0010).
- **SOC 2 Type II, ISO 27001, ISO 27701 sertifikasyonları**: Faz 3.
- **AB/GDPR genişleme**: Faz 3.
- **Billing, reseller, white-label**: Faz 3.
- **Browser extension companion, webcam capture, geolocation, remote shell**: Faz 2-dışı non-goals (MVP scope non-goals ile aynı).
- **Cloud LLM tabanlı sınıflandırma veya analiz**: kategorik olarak hayır; KVKK tezi bunu yasaklar.

### KVKK Üzerindeki Kümülatif Etki

Faz 2 **yeni veri kategorileri yaratmaz**; ancak mevcut kategorilerin **işlenme derinliğini** artırır (OCR = ekran görüntüsü içeriğinin okunabilir metne çevrilmesi; LLM sınıflandırma = uygulama meta verisinin anlamsal kategorilere indirgenmesi; UBA = çalışan bazlı davranışsal profil). Her yeni modül için:

- KVKK m.4 ölçülülük testi yeniden yapılır (sadece gerekli olan işlenir).
- KVKK m.10 aydınlatma metni güncellenir (şeffaflık portalı "Verileriniz" ve "Neler izlenmiyor" sayfaları her modül için ayrı açıklama kartı kazanır).
- KVKK m.6 özel nitelikli veri kazara yakalama riski her yeni modül için DPIA amendment'ında değerlendirilir.
- KVKK m.11 DSR kapsamı otomatik olarak genişler: DSR export'u yeni veri kategorilerini de içermek zorundadır.

### Faz 2 Başarı Kriterleri (Yönetici Düzeyi)

1. İkinci pilot müşteri (ideal: farklı sektör — bankacılık pilotu yanında kamu veya üretim) 2000+ endpoint üzerinde 30 gün stabil çalışır.
2. macOS ve Linux agent'ları Windows ile **aynı footprint hedeflerini** (< 2% CPU, < 150MB RAM) karşılar.
3. En az 2 HRIS konektörü (BambooHR + Logo Tiger) sandbox sync'te doğrulanır.
4. ML sınıflandırıcı Türkçe iş uygulamaları taksonomisi üzerinde ≥ %85 doğruluk elde eder.
5. Canlı izleme kaydı (ADR 0019) end-to-end doğrulanır ve KVKK DPO onayı alır.
6. Mobil admin app TestFlight + Play Store internal testing aşamasına geçer.
7. İkinci pilot DPO'su Faz 2 özellik setini KVKK uyumlu bulur ve devreye alır.

---

## Section B — Phase 2 Scope Items (English technical)

Each IN item below lists: **(i)** business rationale, **(ii)** KVKK implications, **(iii)** dependencies on Phase 1, **(iv)** effort estimate (S ≤ 2 dev-weeks, M ≤ 5, L ≤ 10, XL ≤ 20).

### B.1 — macOS Endpoint Agent

**Business rationale.** The Turkish finance, creative, and technology sectors have significant Apple Silicon (M1/M2/M3) penetration. No competitor in Segment 3 (Teramind/Veriato) ships a first-class macOS agent that respects the Apple privacy model without kernel extensions, which are deprecated on Apple Silicon. Shipping a native macOS agent that uses Endpoint Security Framework, ScreenCaptureKit, and the Network Extension API — all user-authorized via TCC — is an architectural differentiator.

**KVKK implications.** TCC (Transparency, Consent, Control) is the macOS-level embodiment of KVKK m.10 (aydınlatma): macOS itself prompts the user for each capability (screen recording, accessibility, input monitoring, full disk access, network extension). This creates an **OS-enforced parallel consent layer** on top of Personel's organizational consent. Our KVKK narrative becomes stronger: "even if your DPO misunderstands the aydınlatma text, macOS itself will not allow Personel to capture your screen without a system-level prompt." We must also handle the case where a user denies a TCC grant: the policy engine must degrade gracefully (skip that collector) rather than block the entire agent.

**Dependencies on Phase 1.** Collector trait (`personel-collectors`) abstraction, policy engine, queue, transport — all cross-platform in Phase 1 by design (see `apps/agent/crates/personel-os` which holds platform-specific code). Blocks nothing in Phase 1; parallel with Linux agent.

**Effort.** **XL** (10–14 dev-weeks for one senior engineer to reach Windows parity; includes TCC UX, pkg installer, notarization, hardened runtime, MDM deployment profile, QA on 3 Apple Silicon families plus one Intel reference).

---

### B.2 — Linux Endpoint Agent

**Business rationale.** Turkish public sector and defense enterprises run mixed RHEL/Debian/Ubuntu fleets for developer workstations and privileged administrators. The insider threat surface is disproportionately concentrated here. No competitor ships a modern eBPF-based Linux UAM agent; incumbents use LD_PRELOAD shims or require kernel modules. A CO-RE eBPF + fanotify + Network Extension-equivalent agent is both more capable and more maintainable.

**KVKK implications.** Linux desktops are frequently used by engineers who are themselves aware of privacy tooling; their trust in the agent is easily lost if the agent behaves intrusively (e.g., visible kernel module, high CPU, visible syscall interception). A transparent user-mode design that avoids `LD_PRELOAD`, kernel modules, and ptrace-style techniques is both more operationally robust and more ethically defensible. Wayland vs X11 distinction creates KVKK-relevant asymmetry (Wayland is more privacy-preserving; our capture model must respect that — see ADR 0016).

**Dependencies on Phase 1.** Same as macOS. Parallel with macOS agent.

**Effort.** **XL** (10–14 dev-weeks, one senior engineer with eBPF experience; Wayland is the highest-risk subsystem).

---

### B.3 — OCR on Screenshots

**Business rationale.** Faz 1 stores screenshots in MinIO but does not extract text. Competitors (Teramind, Veriato) advertise "full-text search across screen history"; Personel currently cannot. OCR unlocks: (a) DLP rule coverage for screen-only leaks (e.g., someone photographs a TCKN shown on screen and pastes nothing), (b) full-text search in Investigator workflows, (c) retroactive redaction (OCR → redact bounding boxes of sensitive tokens). OCR runs **server-side**, never on the endpoint, because (i) endpoint compute budget is <2% CPU, (ii) OCR models are large, (iii) keeping the heavy inference server-side lets us run it in the same isolated profile as the DLP service.

**KVKK implications.** OCR materially increases the sensitivity of screenshot retention. A screenshot with OCR text becomes **searchable plaintext of the screen contents** — this is a new KVKK m.6 risk surface. Mitigations:

1. OCR output inherits the same retention TTL as the parent screenshot (default 30 days).
2. OCR output goes through `SensitivityGuard` (Phase 1 m.6 DLP signal pack) and sensitive-flagged OCR rows get the shortened TTL bucket.
3. OCR is **off by default** at the tenant level, following the ADR 0013 pattern for DLP. Enable via `GET/PUT /v1/system/ocr-state`; requires DPO action; audit-logged; transparency portal banner.
4. OCR output is stored in a separate Postgres table with row-level encryption (pgcrypto + Vault-wrapped per-tenant key) and is **not** indexed in OpenSearch by default; only privileged Investigator/DPO roles can trigger a retrieval that joins OCR text with the screenshot.

**Dependencies on Phase 1.** Blocked by Phase 1 screenshot storage contract (`screenshots/<tenant>/<endpoint>/<ts>.webp`). Parallel with ML classifier; shares infrastructure around isolated containers.

**Effort.** **M** (4–6 dev-weeks, one backend engineer; Tesseract integration is 1 week, Turkish+English language models are well-tested; the governance layer is the majority of effort).

**Engine choice.** Tesseract 5.x with `tur` + `eng` trained data. PaddleOCR is faster on CJK but Turkish support is less mature. Evaluate PaddleOCR in Phase 3 if Turkish vertical text (rare) becomes relevant. Both engines ship as binaries, no GPU required for interval-based screenshots at realistic throughputs.

---

### B.4 — ML Category Classifier (Local LLM)

**Business rationale.** "Productivity scoring" is Segment 2 (ActivTrak, Insightful) table stakes. Personel explicitly refuses SaaS LLM categorization because it would violate the KVKK thesis. A **local LLM** running on-prem — customer hardware, no network egress — lets Personel ship productivity analytics without compromising. Categories: `work | personal | distraction | unknown`, with the ability for customers to add custom categories via a calibration pipeline. See ADR 0017.

**KVKK implications.** Classification input is `(app_name, window_title, optional_url)` — already captured under m.5/2-f legitimate interest in Phase 1. No new capture, only enrichment. The LLM itself is deployed in an **isolated network segment with no egress** (enforced by Docker network + host firewall), so classification cannot leak data. Output is stored alongside events in ClickHouse with the same TTL. DSR export must include the category label because it is derived personal data.

**Dependencies on Phase 1.** Requires rule-based classifier (which already exists as a fallback in Phase 1 policy engine) to continue operating when the LLM container is unavailable. Parallel with OCR; shares isolated-profile infrastructure.

**Effort.** **L** (6–10 dev-weeks: 2 weeks containerization, 2 weeks benchmarking on Turkish taxonomy, 2 weeks LoRA calibration pipeline, 2 weeks integration with enricher and KVKK audit).

---

### B.5 — UBA / Insider Threat Detection

**Business rationale.** Veriato's marketing lead is "predictive insider threat scoring." Personel can deliver a credible, KVKK-defensible version by running **isolation forest + LSTM** on time-series features extracted from ClickHouse. Critically, we do **not** produce a single opaque "risk score" that would fail KVKK m.4 transparency; instead we produce **explainable anomaly evidence** (e.g., "unusual USB device insertion at 02:00", "off-hours access to legal-holds folder", "print volume 5σ above rolling baseline") that Investigators review in context.

**KVKK implications.** Anomaly detection on per-employee data is a **profil çıkarımı** under KVKK interpretation. Mitigations:

1. UBA runs only over events already retained under the Phase 1 retention matrix.
2. UBA output is itself personal data with its own retention TTL (default 90 days, regenerable).
3. Employees can request UBA output via KVKK m.11 DSR (`request_type=access`) and dispute it (`request_type=object`).
4. UBA never autonomously triggers adverse action (termination, discipline). It only flags events for human review. This is documented in the transparency portal.
5. UBA models are trained on customer-local data only; no cross-customer training.

**Dependencies on Phase 1.** Requires ClickHouse schema stability (Phase 1 event taxonomy). Parallel with mobile app.

**Effort.** **L** (8–10 dev-weeks for one ML engineer; the explainability layer and KVKK-compliant output framing is harder than the models themselves).

---

### B.6 — HRIS Integrations (Connector Framework + 2 Adapters)

**Business rationale.** The #1 operational pain point surfaced in the Phase 1 pilot is manual user/endpoint provisioning. Every HR change (hire, termination, department move) currently requires an admin to edit Personel. HRIS integration automates this and creates a sales wedge: Personel becomes the "HRIS-driven" UAM for Turkish HR departments. See ADR 0018 for the connector interface.

**First adapters to ship.**
1. **BambooHR** — OAuth2, REST API, well-documented. Represents the "international HRIS" path.
2. **Logo Tiger** — Turkish market leader in SMB/midmarket ERP/HR. REST-ish API via Logo Object. Represents the "local Turkish HRIS" path; earns credibility with Turkish buyers.

**KVKK implications.** HRIS → Personel data flow brings in **employment status, department, manager, hire date, termination date** as canonical upstream data. KVKK treats this as processing by the customer (not by Personel); Personel is still not a veri işleyen under the locked jurisdiction decision. Deletion propagation: when HRIS marks terminated, Personel marks the user `inactive` and starts the retention countdown (default: terminated + 2 years per Turkish labor law for forensic readiness, capped by any active legal hold).

**Dependencies on Phase 1.** Requires Phase 1 user/endpoint data model. Blocked by nothing. Sequential with SCIM (B.7) because the HRIS connector interface and SCIM share the identity-sync layer.

**Effort.** **L** total: Framework **M** (4 dev-weeks), BambooHR adapter **S** (2 dev-weeks), Logo Tiger adapter **M** (4 dev-weeks — local testing overhead).

---

### B.7 — SCIM Provisioning

**Business rationale.** SSO vendors (Okta, Azure AD, Keycloak itself) expect SCIM 2.0 for user lifecycle. Without SCIM, customers cannot automate provisioning from their existing SSO. This is often a procurement checkbox. Phase 1 shipped LDAP/AD only; Phase 2 adds OIDC/SAML SSO (listed in Phase 1 OUT-of-scope as a Phase 2 item) and SCIM together because they are co-sold.

**KVKK implications.** SCIM introduces another source of truth for user identity. Conflict resolution rules (see ADR 0018) make HRIS > SCIM > manual, so SCIM is secondary to HRIS where both are configured. Audit: every SCIM sync action is hash-chained.

**Dependencies on Phase 1.** Benefits from but does not block on HRIS framework; both use the same internal identity-sync service. Ships with SSO (OIDC/SAML) which is listed as a Phase 2 scope item but not pulled into the detailed ADR set here (minor effort, ADR 0001-style config decision only).

**Effort.** **M** (4–5 dev-weeks; SCIM 2.0 is well-specified, most work is schema mapping).

---

### B.8 — Mobile Admin App (On-Call Scope)

**Business rationale.** Phase 1 pilot revealed that DPOs and on-call admins need to approve live view requests, acknowledge DSR SLA warnings, and silence P0 alerts **away from a desk**. Building a full mobile mirror of the admin console is out of scope; a **narrow on-call app** is the right answer. See scope below.

**Framework choice: React Native via Expo.** Rationale:
- Existing console codebase is Next.js/React/TypeScript; React Native lets us share i18n strings, API types (OpenAPI-generated), and some utility logic.
- Expo managed workflow reduces native-code burden for Personel's 4-dev team (vs. Flutter which requires Dart expertise no one on the team has).
- EAS Build + Submit lets us ship to TestFlight and Play Store Internal Testing without maintaining Xcode/Android Studio locally.
- Flutter would give better performance but we do not need 60fps UI for approval screens.

**In-scope features (on-call only).**
- OIDC login with same Keycloak realm as console (biometric auth required).
- Live view approval queue: list, approve, reject, with reason code.
- DSR queue: list at-risk + overdue, view detail, reassign, extend deadline.
- Alert silencing: list firing alerts, silence with duration and reason.
- Push notifications via APNs + FCM, delivered from a new `mobile-bff` container (never direct from API to Apple/Google with customer data; the BFF sanitizes payloads).

**Out-of-scope (will be rejected in PRs).** Policy editing, user management, live video playback, keystroke DLP review, destruction reports, reports. Full console remains the primary tool.

**KVKK implications.** Mobile devices are higher-risk carriers for admin credentials. Mitigations:
1. Biometric auth required on every sensitive action (approve live view, extend DSR).
2. No data at rest on the device beyond session tokens (no offline caching of employee data).
3. Remote wipe supported via Keycloak session revocation.
4. Push notification payloads contain ticket IDs only, no PII.
5. The app is not available outside the customer VPN by default; the customer can choose to expose `mobile-bff` through their corporate VPN or a reverse proxy.

**Dependencies on Phase 1.** Requires Phase 1 Admin API + existing live view and DSR endpoints. No new backend endpoints beyond the `mobile-bff` container.

**Effort.** **L** (8–10 dev-weeks: 4 weeks React Native implementation, 2 weeks `mobile-bff`, 2 weeks store submission and TestFlight / Play Store internal setup, 2 weeks QA across iOS 17/18 and Android 14/15).

---

### B.9 — SIEM Integrations (Splunk, Sentinel, Elastic Security)

**Business rationale.** Security teams at banks, insurance companies, and regulated industries expect their UAM platform to feed SIEM. Phase 1 ships audit and events to ClickHouse/OpenSearch only. Phase 2 adds a `siem-exporter` container that emits in **OCSF schema** (Open Cybersecurity Schema Framework), which all three target SIEMs now support or will support by 2026.

**Integration modes.**
- **Push (webhook)**: `siem-exporter` posts to customer-supplied HTTPS endpoint with HMAC signature and configurable batching.
- **Splunk HEC**: HTTP Event Collector with OCSF mapping.
- **Microsoft Sentinel**: Azure Monitor Logs ingestion via DCR or Log Analytics API (customer VPN required because API is cloud; same exception justification as OIDC/Keycloak, documented for KVKK review).
- **Elastic Common Schema fallback**: Elastic customers can prefer ECS over OCSF; adapter supports both.

**KVKK implications.** SIEM receives **events and audit entries** (not raw screenshots, not OCR text, not encrypted keystroke blobs). This is a **cross-system data transfer** within the customer environment — both systems are owned by the same customer, so no m.8/m.9 yurtdışı aktarım issue. Exception: Microsoft Sentinel is Azure-hosted; if the customer uses Sentinel in an EU region they need to assess m.9 themselves. We document this in the runbook.

**Dependencies on Phase 1.** Requires Phase 1 audit and event data models. Blocked by nothing. Parallel with live view recording.

**Effort.** **M** (4–6 dev-weeks; OCSF schema mapping + three adapters + tests).

---

### B.10 — Live View Recording (ADR 0019 Implementation)

**Business rationale.** ADR 0012 defined the Phase 2 design envelope for recording; Phase 2 now implements it. The operational driver is twofold: (a) customers asked for playback of approved sessions for post-hoc forensic review (still within KVKK legal defensibility), (b) SIEM integration (B.9) needs a recording reference to produce complete investigation context.

**KVKK implications.** See ADR 0019. Highlights: off by default, dual-control playback (identical to initiation), LVMK cryptographic separation from TMK, 30-day default retention, DPO-only export with chain-of-custody package, transparency portal visibility.

**Dependencies on Phase 1.** Requires Phase 1 live view state machine and LiveKit integration. Parallel with SIEM.

**Effort.** **L** (6–10 dev-weeks; the cryptographic separation + playback approval state machine + export signing are the heavy parts).

---

## Section C — Aggregate Effort Estimate (Rough)

| Item | Size | Dev-weeks |
|---|---|---|
| B.1 macOS agent | XL | 12 |
| B.2 Linux agent | XL | 12 |
| B.3 OCR | M | 5 |
| B.4 ML classifier | L | 8 |
| B.5 UBA | L | 9 |
| B.6 HRIS framework + 2 adapters | L | 10 |
| B.7 SCIM (incl. SSO) | M | 5 |
| B.8 Mobile admin app | L | 9 |
| B.9 SIEM integrations | M | 5 |
| B.10 Live view recording | L | 8 |
| Phase 2 exit criteria validation + second pilot | — | 6 |
| **Total** | | **89 dev-weeks** |

With a 4-developer team + 2 QA running in parallel at realistic utilization (70% for core dev, 85% for QA), the 32-week Phase 2 window provides roughly **4 devs × 32 weeks × 0.7 = 89.6 dev-weeks** of capacity — mathematically exactly the budget. This is tight. See `phase-2-roadmap.md` for risk mitigation sequencing and `phase-2-exit-criteria.md` for how to descope if slippage requires it.

---

## Section D — KVKK Cumulative Delta Summary

| Phase 2 item | New data | New KVKK m.11 DSR scope | Transparency portal update |
|---|---|---|---|
| macOS agent | Same categories, OS-enforced consent | Same | New aydınlatma card: "macOS üzerinde Personel" |
| Linux agent | Same categories, Wayland asymmetry | Same | New aydınlatma card: "Linux üzerinde Personel" |
| OCR | Screenshot → searchable text | +`ocr_text` export field | New "Ekran metnine dönüştürme" card |
| ML classifier | Category label per activity | +`category` + `confidence` fields | New "Otomatik kategorilendirme" card |
| UBA | Anomaly flags per user | +UBA flag export + dispute flow | New "Olağandışı davranış tespiti" card |
| HRIS | Upstream identity data (customer-sourced) | Sync surface documented | New "İK sistemi entegrasyonu" card |
| SCIM | Provisioning events | Audit entries | No employee-facing change |
| Mobile admin app | No new data, new access path | No change | No change (admin-facing tool) |
| SIEM | No new data, new egress path | No change | New "SIEM entegrasyonu" card |
| Live view recording | Video blobs + LVMK entries | +Recording playback history | Updated "Canlı İzleme" card (opt-in ceremony) |

Each row requires a companion compliance deliverable: DPIA amendment section, aydınlatma text update, and an entry in `docs/compliance/iltica-silme-politikasi.md` retention matrix. These are flagged as **compliance-auditor follow-up work** not blocking Phase 2 engineering but required before each module ships to a customer.

---

*End of phase-2-scope.md v0.1*
