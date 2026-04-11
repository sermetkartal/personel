# Phase 2 Exit Criteria

> Language: English. Status: PLANNING v0.1 (2026-04-10).
> Format: machine-readable table. Each row is testable.
> Companion file (to be written at Phase 2 kickoff): `apps/qa/ci/phase2-thresholds.yaml` — mirror of this document as YAML for CI gating, following the Phase 1 pattern.

## Format

Each criterion has:
- **ID** — stable identifier (C.1 … C.N); grouped by feature code (A = agents, B = classifier/OCR, H = HRIS/SCIM, U = UBA, M = mobile, S = SIEM, L = live view recording, Z = overall).
- **Feature** — the Phase 2 scope item from `phase-2-scope.md`.
- **Criterion** — the requirement, stated as a pass/fail condition.
- **Measurement** — how it is tested.
- **Target** — the quantitative or qualitative pass threshold.
- **Blocking?** — whether Phase 2 may not ship to second pilot if this criterion fails.

---

## A — Endpoint Agents (macOS + Linux)

| ID | Feature | Criterion | Measurement | Target | Blocking? |
|---|---|---|---|---|---|
| **A.1** | macOS agent | CPU and RAM footprint on Apple Silicon | `footprint-bench` on M1, M2, M3 (one device each, typical workload: browser + office + teams) over 1-hour soak | **<2% CPU average**, **<150 MB RSS peak** (matches Phase 1 Windows target) | Yes |
| **A.2** | macOS agent | Feature parity with Phase 1 Windows collector set (minus DLP content, which is off by default) | E2E test matrix in `apps/qa/test/e2e/macos_parity_test.go` covering process, file, window, screenshot, clipboard metadata, keystroke metadata, network summary, DNS, USB, print | 100% of parity cases pass | Yes |
| **A.3** | macOS agent | TCC graceful degradation | Automated test: revoke Screen Recording grant mid-session, assert agent continues, assert console shows capability gap alert within 60s | Degradation handled, no crash, alert visible | Yes |
| **A.4** | macOS agent | Notarization + hardened runtime | `spctl --assess --type exec` on the signed `.pkg`, and verify hardened runtime bit set | Exits 0, hardened runtime confirmed | Yes |
| **A.5** | macOS agent | MDM profile push on Jamf/Intune sandbox | Deploy `.pkg` + config profile via sandbox tenant; verify System Extension pre-approved, user only needs Screen Recording + Input Monitoring | Deployment completes; user sees only the two expected prompts | No (runbook-only if sandbox unavailable) |
| **A.6** | Linux agent | Distro coverage | Install and smoke-test on: **Ubuntu 22.04**, **Ubuntu 24.04**, **Debian 12**, **RHEL 9**, **Fedora latest** (pinned at Phase 2 kickoff) | Install and connect to gateway on all 5; full collector set active | Yes |
| **A.7** | Linux agent | Footprint on reference hardware (Ubuntu 22.04 on mid-range laptop) | `footprint-bench` over 1-hour typical workload | **<2% CPU average**, **<150 MB RSS peak** | Yes |
| **A.8** | Linux agent | eBPF CO-RE portability | Load and run identical compiled eBPF bytecode across the 5 target distros without recompilation | All distros load cleanly | Yes |
| **A.9** | Linux agent | Wayland degradation | On Wayland session, confirm DLP-content-mode is reported unavailable (expected), confirm all other collectors work | Reports unavailable gracefully; no crash; portal explainer displays | Yes |
| **A.10** | Linux agent | fanotify → inotify fallback path | On a host without `CAP_SYS_ADMIN`, agent falls back to inotify and reports degraded state | Fallback occurs automatically; admin console shows degraded state | No (acceptance test only) |
| **A.11** | All agents | Concurrent multi-platform operation | Single gateway accepts streams from Windows + macOS + Linux endpoints simultaneously for 1 hour with no errors | Zero gateway errors; ClickHouse contains events from all three platforms | Yes |
| **A.12** | All agents | Auto-update rollback | Canary cohort on each platform, forced failure, rollback completes within 10 minutes | Rollback observed, post-rollback agent version matches pre-canary | Yes |

---

## B — OCR + ML Classifier

| ID | Feature | Criterion | Measurement | Target | Blocking? |
|---|---|---|---|---|---|
| **B.1** | OCR service | Opt-in ceremony executable end-to-end | Script-driven: DPIA amendment check → enable script → container start → state endpoint flip → portal banner → audit entry on WORM | Completes in <30 minutes | Yes |
| **B.2** | OCR service | Turkish OCR accuracy | 100-screenshot test set with known ground truth text, mix of Turkish + English, measure CER (Character Error Rate) | **CER ≤ 8%** | No (degraded-accept) |
| **B.3** | OCR service | Sensitivity integration | OCR output containing m.6 patterns (TCKN, IBAN, etc.) lands in the sensitive-flagged bucket with shortened TTL | Test synthetic scenario; verify TTL column | Yes |
| **B.4** | OCR service | DSR export includes OCR text | Synthetic DSR m.11 access request returns OCR text for retained screenshots | Export contains OCR field | Yes |
| **B.5** | ML classifier | Accuracy on Turkish taxonomy | 100-app test set (Turkish business apps + common Turkish web apps + distractions) | **≥85% top-1 accuracy** | Yes |
| **B.6** | ML classifier | Latency per classification | Batch size 16, 10000 events, measure p95 | **p95 ≤ 100 ms** per event (at tenant-scale expected throughput) | Yes |
| **B.7** | ML classifier | Network isolation | Attempt egress from `llama-server` container to `8.8.8.8` during operation | **Connection refused / no route** (verified by preflight and live ping during test) | Yes |
| **B.8** | ML classifier | Fallback to rule-based on outage | Kill `llama-server`, verify `ml-classifier` falls back and events get `fallback_used=true` | Classification continues; no event loss | Yes |
| **B.9** | ML classifier | Dispute flow round-trip | Employee disputes a category via portal → admin console sees `category_dispute` entry → disputed events excluded from reports | E2E test passes | Yes |
| **B.10** | ML classifier | LoRA calibration for custom app | Submit 50 custom labels → retrain → classifier improves on held-out custom-app test set | Improvement ≥ 15 points over base | No |

---

## H — HRIS + SCIM + SSO

| ID | Feature | Criterion | Measurement | Target | Blocking? |
|---|---|---|---|---|---|
| **H.1** | BambooHR adapter | Sandbox sync performance | BambooHR sandbox with 1000 synthetic employees, run `ListEmployees`-based full sync | **≤ 5 minutes** wall-clock | Yes |
| **H.2** | BambooHR adapter | Webhook delivery | Create an employee in the sandbox, verify Personel receives webhook and ingests within 30s | Ingestion observed in logs + DB | Yes |
| **H.3** | Logo Tiger adapter | Demo instance sync | Connect to Logo demo, run full sync, verify Turkish fields (AD, SOYAD, DEPARTMAN) map correctly | Sync completes; field mapping verified | Yes |
| **H.4** | Logo Tiger adapter | Synthetic email handling | Employee without email in Logo gets synthetic email tagged `email_synthetic=true` | Field present in Personel users table | Yes |
| **H.5** | HRIS conflict rules | Termination propagation | Mark employee terminated in HRIS; Personel marks inactive, retention countdown starts, audit entries written | Expected state transitions verified | Yes |
| **H.6** | HRIS conflict rules | Platform-state preservation | Terminated employee retains DSR history, audit trail, legal-hold status intact | No data loss on termination | Yes |
| **H.7** | SCIM | Keycloak provisioning round-trip | Create + update + delete user via Keycloak SCIM client | Each op reflected in Personel users | Yes |
| **H.8** | SSO (OIDC/SAML) | Login flow | Configure Keycloak as IdP, login via OIDC and SAML for admin roles | Login succeeds for Admin, HR, DPO, Investigator, Manager roles | Yes |
| **H.9** | HRIS sync audit | Each sync operation logged | Verify `hris.sync_started`, `hris.sync_completed`, `hris.employee_updated` entries | Entries present and hash-chained | Yes |

---

## U — UBA / Insider Threat

| ID | Feature | Criterion | Measurement | Target | Blocking? |
|---|---|---|---|---|---|
| **U.1** | uba-service | Flag generation | Nightly job runs, produces flags on a synthetic dataset with 10 known anomalies | **≥ 8/10** known anomalies flagged | Yes |
| **U.2** | uba-service | False positive rate | Same dataset with 100 normal users; measure unwarranted flags | **≤ 5%** false positive rate | Yes |
| **U.3** | uba-service | Explainability | Every flag includes top-3 contributing features | 100% of flags have features attached | Yes |
| **U.4** | uba-service | Read-only ClickHouse role | Attempt an INSERT from `uba-service` role | **Permission denied** | Yes |
| **U.5** | uba-service | Dispute flow | Employee disputes a flag via portal DSR; disputed flag excluded from Investigator dashboard | End-to-end test passes | Yes |
| **U.6** | uba-service | No autonomous action | Verify no code path triggers policy change, access revocation, or notification outside the Investigator dashboard | Code review + negative test | Yes |

---

## M — Mobile Admin App

| ID | Feature | Criterion | Measurement | Target | Blocking? |
|---|---|---|---|---|---|
| **M.1** | Mobile app | TestFlight + Play Store internal build | Submit builds; verify acceptance by both stores | Both accepted for internal testing | Yes |
| **M.2** | Mobile app | iOS OS coverage | Functional test on **iOS 17 + iOS 18** (iPhone simulator + at least one real device) | Pass on both | Yes |
| **M.3** | Mobile app | Android OS coverage | Functional test on **Android 14 + Android 15** (emulator + at least one real device) | Pass on both | Yes |
| **M.4** | Mobile app | Biometric required on sensitive actions | Attempt approve-live-view without biometric; verify blocked | Blocked; audit entry for failed attempt | Yes |
| **M.5** | Mobile app | Crash rate baseline | 10K synthetic sessions via Detox/Maestro | **<0.1%** crash rate | Yes |
| **M.6** | mobile-bff | No PII in push payloads | Inspect APNs/FCM payloads in test environment | Payloads contain ticket ID only | Yes |
| **M.7** | mobile-bff | Push auth + HMAC | Replay attack test | Replay rejected | Yes |
| **M.8** | Mobile app | Live view approval round-trip | From app: approve a live view request → console shows approved state | <5s end-to-end | Yes |

---

## S — SIEM Integrations

| ID | Feature | Criterion | Measurement | Target | Blocking? |
|---|---|---|---|---|---|
| **S.1** | siem-exporter | OCSF schema conformance | Generated events validated against OCSF JSON schema | 100% validate | Yes |
| **S.2** | siem-exporter | Splunk HEC delivery | Test tenant delivers 1000 events to Splunk HEC sandbox | All 1000 received in Splunk | Yes |
| **S.3** | siem-exporter | Microsoft Sentinel delivery | Same but to Sentinel DCR endpoint | Events visible in Sentinel | No (sandbox access dependent) |
| **S.4** | siem-exporter | Elastic ECS fallback | Same but to Elastic | Events visible in Elastic | No |
| **S.5** | siem-exporter | Backpressure | Force SIEM endpoint to 5xx for 5 minutes; verify queuing and retry | No event loss; exporter retries until recovered | Yes |
| **S.6** | siem-exporter | HMAC signing of webhook payloads | Verify HMAC in receiver | Signature validates | Yes |

---

## L — Live View Recording

| ID | Feature | Criterion | Measurement | Target | Blocking? |
|---|---|---|---|---|---|
| **L.1** | live-view-recorder | LVMK cryptographic separation | `live-view-recorder` Vault AppRole cannot derive from TMK; `dlp-service` AppRole cannot derive from LVMK | Both attempts fail with permission denied | Yes |
| **L.2** | live-view-recorder | End-to-end 30-minute recording | Record a 30-minute live view session, upload to MinIO, verify chunked encryption, play back, verify integrity | Full round-trip passes; SHA256 stable | Yes |
| **L.3** | live-view-recorder | Per-session toggle enforcement | Start a session with `recording=false`, verify no recording artifact exists | No `live_view_recordings` row created | Yes |
| **L.4** | live-view-recorder | Dual-control playback | Playback request by user X cannot be approved by user X | Blocked; audit entry | Yes |
| **L.5** | live-view-recorder | 30-minute approval validity | Approved playback not started within 30 minutes → approval expires | Expiration observed; new request required | Yes |
| **L.6** | live-view-recorder | Transparency portal visibility | Employee sees "kayıt alındı: Evet" for recorded sessions and playback count | UI test passes | Yes |
| **L.7** | live-view-recorder | DPO-only export | Admin (non-DPO) cannot invoke export; DPO can | RBAC test passes | Yes |
| **L.8** | live-view-recorder | Chain-of-custody signature | Export ZIP verifies under control-plane public key | Verification succeeds | Yes |
| **L.9** | live-view-recorder | 30-day TTL + legal hold | TTL destroys recording; legal hold suspends TTL | Both observed in test | Yes |
| **L.10** | live-view-recorder | Audit chain linkage | Every recording lifecycle event appears in audit chain and WORM sink | Chain verifier passes | Yes |

---

## Z — Overall Phase 2 Exit

| ID | Feature | Criterion | Measurement | Target | Blocking? |
|---|---|---|---|---|---|
| **Z.1** | Second pilot | Stable operation at ≥ 2000 endpoints | 14 consecutive days of stable operation with Phase 2 feature set enabled | 99.5% uptime over 14 days | Yes |
| **Z.2** | KVKK compliance | Full DPIA amendment approved by compliance-auditor | Document review | Signed off | Yes |
| **Z.3** | KVKK compliance | Transparency portal updated with all Phase 2 modules (macOS, Linux, OCR, ML, UBA, HRIS, SIEM, mobile, live view recording) | Portal audit | All cards present | Yes |
| **Z.4** | Security | No critical/high CVEs in new container images | Trivy/Grype scan | Zero critical/high | Yes |
| **Z.5** | Performance | Phase 1 exit criteria (agent footprint, end-to-end latency, dashboard p95) **still met** with Phase 2 feature set enabled | Rerun Phase 1 benchmarks | No regressions | Yes |
| **Z.6** | Documentation | Phase 2 runbooks (macOS install, Linux install, HRIS onboarding, SIEM setup, LVR opt-in ceremony) published | Doc audit | All present and reviewed | Yes |
| **Z.7** | Rollback | Any Phase 2 module can be independently disabled without breaking Phase 1 functionality | Chaos drill: disable each module one at a time | Phase 1 flows continue | Yes |
| **Z.8** | Second pilot | Second pilot DPO sign-off on KVKK posture | DPO meeting minutes | Signed | Yes |

---

## Descope Priority Order (if the team runs out of time)

If Phase 2.7 arrives with unfinished items, descoping decisions should follow this priority, **worst to best to keep**:

1. UBA (U.*) — defer to Phase 2.8 / Phase 3.
2. Mobile admin app (M.*) — defer; on-call admins keep using the web console.
3. SIEM integrations (S.*) — defer.
4. OCR (B.1–B.4) — defer.
5. Second HRIS adapter (Logo Tiger) — defer, ship with BambooHR only.
6. ML classifier (B.5–B.10) — defer.
7. Live view recording (L.*) — **do not descope** (ADR 0012 commitment; engineering must find a way).
8. Linux agent — **do not descope** (commercial commitment to second pilot).
9. macOS agent — **do not descope** (commercial commitment).

---

*End of phase-2-exit-criteria.md v0.1*
