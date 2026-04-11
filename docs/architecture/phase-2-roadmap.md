# Faz 2 Yol Haritası — 32 Haftalık Plan

> Language: Turkish with English scheduling details.
> Status: PLANNING — version 0.1 (2026-04-10).
> Scope: Phase 2 only. Phase 3 (SaaS, K8s, SOC 2, minifilter, AB/GDPR) is not in this roadmap.
> Team assumption: **4 software engineers + 2 QA engineers**, plus fractional ML engineer support (0.5 FTE, mostly Phase 2.2 and 2.4).

## Takım Atamaları (Team Assignments)

| Role | Name (placeholder) | Primary focus |
|---|---|---|
| Senior Rust engineer | Dev-A | macOS agent lead |
| Senior Rust engineer | Dev-B | Linux agent lead |
| Senior Go engineer | Dev-C | OCR, HRIS framework, SIEM, live view recording backend |
| Frontend + React Native | Dev-D | Mobile admin app, admin console updates (DPO UI for LVR, OCR, ML, HRIS, UBA) |
| ML engineer (0.5 FTE) | Dev-E | ML classifier (Phase 2.2), UBA (Phase 2.4) |
| QA engineer | QA-1 | Agent platforms (Windows regression, macOS, Linux) |
| QA engineer | QA-2 | Backend + integrations (HRIS, SIEM, live view recording, OCR, ML) |

## High-Level Sequence

```
Weeks 1-2:   2.0  Prep + Phase 1 polish closure + CI hardening
Weeks 3-8:   2.1  macOS + Linux agents (parallel)
Weeks 9-12:  2.2  ML classifier + OCR (parallel)
Weeks 13-16: 2.3  HRIS framework + 2 adapters + SCIM/SSO
Weeks 17-20: 2.4  UBA / insider threat detection
Weeks 21-24: 2.5  Mobile admin app
Weeks 25-28: 2.6  SIEM integrations + live view recording
Weeks 29-32: 2.7  Exit criteria validation + second pilot onboarding
```

---

## Phase 2.0 — Preparation & Phase 1 Polish Closure (Weeks 1–2)

**Goal**: clean starting line. Phase 1 tech debt closed; CI is green; Phase 2 toolchain and accounts acquired.

| Week | Dev-A | Dev-B | Dev-C | Dev-D | Dev-E | QA-1 | QA-2 |
|---|---|---|---|---|---|---|---|
| 1 | Apple Developer enrollment + entitlement applications (ES, NetworkExtension, SystemExtension) filed day 1. Start macOS toolchain prep. | eBPF / libbpf-rs research. Spin up all 5 target distros as VMs. | **Phase 1 polish closure** items from CLAUDE.md §10 (DLP scripts, missing endpoints). | Same as Dev-C (paired on polish items). | — | Windows agent regression baseline on current main. | Backend regression baseline. |
| 2 | Swift ES shim skeleton (no collectors yet), Rust↔Swift IPC via UDS decided. | Rust libbpf-rs PoC: process exec events on Ubuntu 22.04 + RHEL 9. | OCR service skeleton (container, NATS subscriber stub, Tesseract install). | Mobile CI: EAS Build account + TestFlight setup. RN project scaffolded. | Procure Llama 3.2 3B weights, legal review of community license. | macOS + Linux target hardware acquired for bench. | Install HRIS sandbox accounts (BambooHR free trial; Logo Tiger demo). |

**Phase 2.0 exit gate**: all Phase 1 polish items from CLAUDE.md §10 merged to main. CI green across all Phase 1 stacks (Rust agent `cargo test`, Go `go test ./...`, Next.js `pnpm build`, integration tests against Compose stack). Phase 1 exit criteria #9 (keystroke red team) and #17 (ClickHouse replication) validated if not already. Apple entitlement request acknowledged (not necessarily approved).

---

## Phase 2.1 — macOS + Linux Agents (Weeks 3–8, 6 weeks)

**Goal**: reach Windows feature parity on macOS (Dev-A) and Linux (Dev-B) for the Phase 1 collector set, minus DLP content (which is off-by-default anyway).

| Week | Dev-A (macOS) | Dev-B (Linux) | Dev-C | Dev-D | QA-1 | QA-2 |
|---|---|---|---|---|---|---|
| 3 | ES process events + file events through Swift shim → Rust core. | eBPF process exec/exit, fanotify file events. | OCR service: Tesseract integration, `tur`+`eng` models, per-screenshot job runner. | Admin console updates: DPO UI for OCR enable/disable ceremony (mirrors DLP pattern). | Write macOS agent test harness. | OCR E2E harness against Compose stack. |
| 4 | ScreenCaptureKit screen capture, TCC detection, degradation state reporting. | X11 screen capture (libxcb), Wayland portal-based capture for GNOME. | OCR governance: sensitivity guard integration, row-level encryption via pgcrypto. | Mobile BFF skeleton (stateless, OIDC, endpoint subset). | macOS functional test on M1 + M2. | OCR sensitivity flow: m.6 pattern hits in OCR text → shortened TTL bucket. |
| 5 | Network Extension setup, DNS proxy, flow summary producer. | eBPF network (sock_ops), DNS capture, USB via libudev. | OCR + ML classifier interaction boundary check: prove no cross-pipeline data flow. | Admin console updates: DPO UI for ML classifier enable, dispute flow. | Linux functional test on Ubuntu 22.04 + RHEL 9. | OCR footprint + throughput bench. |
| 6 | IOHIDManager keystroke counts, clipboard metadata. | Input via /dev/input/event* (metadata only). Clipboard: X11 and Wayland adapters. | — (ML in Phase 2.2). | Admin console: sensitivity guard visualizations. | macOS policy engine integration test. | Admin console regression. |
| 7 | pkg installer, postinstall script, MDM profile (Jamf sample). Notarization dry-run. | DEB + RPM builds, systemd units, capability model test. | Live view producer cross-platform audit (ensures Phase 1 LiveKit integration works on all three OS). | — | Notarization test. MDM push on sample Jamf tenant. | Footprint bench on macOS M1/M2/M3 and 4 Linux distros. |
| 8 | Hardening: degradation on TCC denial, watchdog, auto-update on macOS. | Hardening: SELinux policy, AppArmor profile. Distribution signing. | — | React Native: approval queue screen working against mobile-bff stub. | **Macbench + Linuxbench** sign-off: all agents <2% CPU, <150MB RAM in steady state. | Cross-platform E2E suite (Windows + macOS + Linux all connected to single gateway). |

**Phase 2.1 exit gate**: macOS agent and Linux agent both pass their respective exit criteria in `phase-2-exit-criteria.md`. A single Compose stack accepts concurrent streams from Windows, macOS, and Linux endpoints in integration test. Notarization approved; Apple entitlements approved (if not, descope: macOS agent ships to second pilot as "preview").

---

## Phase 2.2 — ML Classifier + OCR (Weeks 9–12, 4 weeks)

**Goal**: ship ML classifier (Dev-E lead, Dev-C backend support) and finalize OCR (Dev-C).

| Week | Dev-A | Dev-B | Dev-C | Dev-D | Dev-E | QA-1 | QA-2 |
|---|---|---|---|---|---|---|---|
| 9 | macOS bugs from Phase 2.1, prepare for 2.3. | Linux bugs from Phase 2.1. | ml-classifier container, NATS subject wiring, llama-server sidecar, `net_ml` isolation, preflight network check. | Admin console: ML category dispute flow implementation. | Llama 3.2 3B benchmarking on Turkish taxonomy (goal: ≥85%). | Agent post-release soak test (500 endpoints). | ML + OCR KVKK flow tests (DSR export includes new categories; dispute path works). |
| 10 | — | — | OCR final: opt-in ceremony script, transparency portal banner, DPO UI flow. | Mobile BFF: live view approval screen. | LoRA calibration pipeline: label ingestion, training job, hot-load. | — | ml-classifier load test: 1000 eps/sec batch inference on 16-core Xeon. |
| 11 | — | — | ml-classifier fallback to Phase 1 rule-based classifier on llama-server outage. | Mobile BFF: DSR queue screen. | UBA research prep: ClickHouse query patterns, feature extraction notebook. | — | ml-classifier fallback test, llama-server crash recovery. |
| 12 | — | — | ml-classifier DSR export fields; OCR text DSR export. | Mobile BFF: alert silence screen. | ML calibration end-to-end: customer labels → retrained LoRA → improved accuracy measurement. | ML accuracy validation on 100-app test set. | OCR + ML joint KVKK review prep for compliance-auditor. |

**Phase 2.2 exit gate**: ML classifier hits ≥85% accuracy on Turkish benchmark. OCR opt-in ceremony rehearsed end-to-end. Fallback paths tested.

---

## Phase 2.3 — HRIS + SCIM + SSO (Weeks 13–16, 4 weeks)

**Goal**: HRIS framework, BambooHR + Logo Tiger adapters shipping. SCIM 2.0 and OIDC/SAML SSO (carried over from Phase 1 OUT-of-scope).

| Week | Dev-A | Dev-B | Dev-C (lead) | Dev-D | Dev-E | QA-1 | QA-2 |
|---|---|---|---|---|---|---|---|
| 13 | Agent bug queue. | Agent bug queue. | HRIS connector interface + registry. hris-connector container scaffold. Vault KV credential flow. | Admin console: HRIS settings page (adapter picker, credential input via Vault-backed form, sync status dashboard). | — | — | SCIM conformance tests (Okta sandbox). |
| 14 | — | — | BambooHR adapter: OAuth2, ListEmployees, GetEmployee, WatchChanges, webhook receiver. | Admin console: user page shows HRIS-linked fields (read-only if HRIS-owned). | — | — | BambooHR E2E: sandbox sync of 1000 employees in <5 minutes. |
| 15 | — | — | Logo Tiger adapter: session-ticket auth, ListEmployees, incremental sync via updated_at, synthetic email handling. | Admin console: conflict resolution UI (show HRIS vs Personel divergence, action buttons). | UBA prep: time-series feature extraction notebook complete. | Windows/macOS/Linux agents: SCIM user provisioning integration test. | Logo Tiger E2E against demo instance. |
| 16 | — | — | SCIM 2.0 endpoint for Keycloak-sourced provisioning; OIDC/SAML SSO in Keycloak realm. Conflict resolution rules implemented and tested. | Admin console: SSO setup wizard. | UBA: isolation forest baseline model on synthetic data. | — | HRIS + SCIM + SSO E2E integration test. |

**Phase 2.3 exit gate**: BambooHR sandbox sync 1000 employees ≤ 5 min. Logo Tiger demo sync works. SCIM provisioning works through Keycloak. Conflict rules enforced in automated test.

---

## Phase 2.4 — UBA / Insider Threat (Weeks 17–20, 4 weeks)

**Goal**: uba-service shipping with isolation forest + LSTM, explainable anomaly flags, dispute flow.

| Week | Dev-A | Dev-B | Dev-C | Dev-D | Dev-E (lead) | QA-1 | QA-2 |
|---|---|---|---|---|---|---|---|
| 17 | Agent polish: auto-update rollback tests on macOS, Linux. | Agent polish. | uba-service scaffold (Python FastAPI). ClickHouse read-only role. Postgres uba_flags table. | Admin console: Investigator dashboard with anomaly flag list. | Isolation forest: per-user feature vectors (USB insertion rate, off-hours access, print volume, app diversity). | Agent soak test. | UBA flag schema + DSR export test. |
| 18 | — | — | uba-service nightly scheduler, flag lifecycle (new → reviewed → dismissed → disputed). | Admin console: drill-down from anomaly flag to underlying events (no new data exposure; uses existing event viewer). | LSTM: sequence model on per-user activity time series. | — | UBA flag KVKK review. |
| 19 | — | — | UBA DSR integration: employees can see their flags, dispute them. | Portal: "Neden bu bayrak verildi?" explainer component. | Explainability: top-3 contributing features per flag. | — | Dispute flow E2E. |
| 20 | — | — | — | — | UBA accuracy tuning. False positive rate target <5%. | Full-stack load test (500 endpoints) with UBA enabled. | UBA false positive rate measurement on staging. |

**Phase 2.4 exit gate**: UBA generates explainable flags. False positive rate <5% on test set. Dispute flow works end-to-end.

---

## Phase 2.5 — Mobile Admin App (Weeks 21–24, 4 weeks)

**Goal**: React Native app shipping to TestFlight + Play Store internal track.

| Week | Dev-A | Dev-B | Dev-C | Dev-D (lead) | Dev-E | QA-1 | QA-2 |
|---|---|---|---|---|---|---|---|
| 21 | Agent bug queue. | Agent bug queue. | mobile-bff finalization: push cert + FCM key management in Vault. | RN app: OIDC + biometric auth, live view approval queue. | UBA tuning, calibration. | — | mobile-bff security test (JWT validation, HMAC push auth). |
| 22 | — | — | — | RN app: DSR queue, alert silencing. | — | — | TestFlight build submission. |
| 23 | — | — | — | RN app: push notifications end-to-end. | — | — | Play Store internal track build. |
| 24 | — | — | — | RN app: crash reporting (Sentry self-hosted), final QA polish. | — | Mobile QA: iOS 17/18 + Android 14/15 matrix. | Mobile BFF E2E. |

**Phase 2.5 exit gate**: Mobile app on TestFlight and Play Store internal track. Crash rate baseline established. No security findings in mobile-bff.

---

## Phase 2.6 — SIEM + Live View Recording (Weeks 25–28, 4 weeks)

| Week | Dev-A | Dev-B | Dev-C (lead) | Dev-D | Dev-E | QA-1 | QA-2 |
|---|---|---|---|---|---|---|---|
| 25 | Agent polish cycle. | Agent polish cycle. | siem-exporter scaffold, OCSF schema mapping, generic webhook adapter. | Admin console: SIEM settings page, per-adapter config. | — | — | siem-exporter contract tests. |
| 26 | — | — | Splunk HEC + Sentinel DCR + Elastic ECS adapters. | Admin console: live view recording DPO toggle (tenant-level). | — | — | SIEM adapter E2E with each target. |
| 27 | — | — | live-view-recorder: Vault LVMK setup scripts, session DEK derivation, chunked encryption, MinIO upload. Postgres schema. | Admin console: live view session list with recording badge for DPO+Investigator roles. JS playback prototype (in-memory decrypt). | — | — | Live view recording E2E (record, upload, playback). |
| 28 | — | — | Playback state machine: dual-control approval, pre-signed URL issuance, DEK delivery over SSE. DPO export (chain-of-custody ZIP). | Admin console: playback approval flow. Transparency portal: session history shows "kayıt alındı" + playback counts. | — | — | Live view recording + KVKK flow full test. DPO export signature verification. |

**Phase 2.6 exit gate**: SIEM adapters deliver events end-to-end. Live view recording full flow works, including DPO-only export and dual-control playback.

---

## Phase 2.7 — Exit Criteria Validation + Second Pilot (Weeks 29–32, 4 weeks)

**Goal**: validate all Phase 2 exit criteria. Deploy to second pilot customer. Gather 30-day stability data.

| Week | Activity |
|---|---|
| 29 | Full Phase 2 exit criteria suite runs against staging. Issues triaged and assigned. |
| 30 | Blockers from week 29 fixed. Dry-run install at second pilot. Compliance-auditor final review of all Phase 2 KVKK touchpoints. |
| 31 | Second pilot goes live (2000 endpoints). Daily stability monitoring. |
| 32 | 14 days of stable operation achieved (target). Phase 2 retrospective + sign-off. Phase 3 planning handoff. |

**Phase 2.7 exit gate**: second pilot has run stably for ≥14 consecutive days with Phase 2 feature set enabled. At least one KVKK DPO review completed on the second pilot site. Phase 2 exit criteria all validated or explicitly waived by product owner.

---

## Dependencies and Critical Path

**Critical path**: Phase 2.0 → 2.1 (agents) → 2.6 (live view recording uses cross-platform agent live view producer) → 2.7.

**Apple entitlement approval** is the single biggest external dependency. If not approved by week 6, macOS agent ships as "preview" to second pilot and we fall back to notarized-but-unentitled signing (limits ES access — severe feature cut, would force descope decision in week 6).

**llama.cpp license + Llama 3.2 license legal review** must complete by week 4 or ml-classifier is blocked.

**HRIS sandbox access** (BambooHR free trial + Logo Tiger demo) must be provisioned in Phase 2.0 week 1–2.

**Second pilot customer contract** must be signed by week 20 at the latest, preferably earlier, to allow pre-deployment discovery.

---

## Slack and Risk Budget

The plan consumes **exactly** the 32 weeks × 4 devs × 0.7 utilization = 89.6 dev-weeks budgeted in `phase-2-scope.md`. There is **no slack** for unexpected work.

Risk mitigation:
- **If behind by week 12**: descope UBA to Phase 3 (weeks 17–20 reallocated to catch-up).
- **If behind by week 16**: descope mobile admin app to Phase 3 (weeks 21–24 reallocated).
- **If behind by week 20**: descope SIEM integrations to Phase 2.8 / Phase 3. Live view recording must stay (it's the ADR 0012 commitment).
- **If Apple entitlements denied at week 8**: macOS agent ships as preview; plan unchanged otherwise.
- **If both agents slip into week 10+**: revisit whole plan; honest conversation with product owner.

Explicit protected items (will not be descoped short of a product-owner override):
- macOS agent MVP (even if feature-partial).
- Linux agent MVP.
- HRIS framework + at least one adapter (BambooHR preferred for pilot-international, Logo Tiger for Turkish pilot).
- Live view recording (ADR 0012 commitment).

---

## Compliance-Auditor Parallel Work

Compliance-auditor is not on the core dev roster but is required at these checkpoints:

- Week 2: sign off that Phase 1 polish closures don't introduce new compliance gaps.
- Week 7: macOS + Linux aydınlatma updates reviewed.
- Week 10: OCR + ML classifier KVKK review.
- Week 15: HRIS connector KVKK review (data mapping, deletion semantics).
- Week 19: UBA KVKK review (profiling, explainability, dispute path).
- Week 25: SIEM integration review (cross-system data flows).
- Week 28: Live view recording final compliance review.
- Week 30: Full Phase 2 DPIA amendment sign-off.

Each of these is a ~4-hour meeting + 1-day follow-up review by the compliance-auditor agent or human DPO.

---

*End of phase-2-roadmap.md v0.1*
