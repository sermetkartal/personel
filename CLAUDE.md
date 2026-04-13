# CLAUDE.md — Personel Platform

> **Bu dosya, Personel repository'sine giren her Claude Code oturumu (ve insan geliştirici) tarafından ilk okunması gereken dosyadır.** Projenin "neyi", "neden", "nasıl" ve "nerede" durduğunu tek sayfada özetler. Ayrıntılar için ilgili belgelere link verir — aynı içeriği tekrarlamaz.
>
> Versiyon: 2.2 — Faz 1+2+3+4 COMPLETE; Faz 5 Wave 1 partial: 9/21 (items 41,42,47,50,53,54,55,56,58). Cluster items (43-46, 48, 51, 52) AWAITING ikinci makine. Toplam 49/190. — 2026-04-13

---

## 0. MEVCUT DURUM + 190-MADDE PRODUCTION ROADMAP (2026-04-13)

### 🛑 KRİTİK GÜVENLİK KURALI — EN BÜYÜK LİMİT

> **SADECE iki makinede işlem yap. Başka HİÇBİR sisteme dokunma.**
>
> 1. **Windows VM**: `192.168.5.30` (kullanıcı: kartal, hostname: DESKTOP-426U3BG)
> 2. **Ubuntu backend**: `192.168.5.44` (kullanıcı: kartal, repo: /home/kartal/personel)
>
> **YASAK**:
> - Başka IP/host'a SSH bağlanma
> - Başka makinede dosya oluşturma/değiştirme/silme
> - Başka makineye paket kurma
> - Network tarama, port scan, başka host enumeration
> - Müşteri ortamı, üretim sunucuları, başka geliştirme makineleri — hiçbiri
> - Yerel macOS dosya sistemi dışarısında (Mac Code Code session için)
> - Cloud API'ler (AWS, GCP, Azure) — hiçbiri
> - DNS query'ler ve hostname lookup'lar bile sadece bu iki IP'ye yönelik olmalı
>
> **İstisna**: GitHub.com'a `git push/pull` (sadece sermetkartal/personel repo).
>
> **Şüpheye düşersen**: Komut çalıştırma. CLAUDE.md state'e "BLOCKED: <neden>" yaz, sonraki maddeye geç.
>
> Bu kural her şeyin üstünde. Roadmap'teki herhangi bir madde bu iki makine dışı bir işlem gerektiriyorsa: madde'yi "AWAITING CUSTOMER ACTION" listesine taşı, sonraki maddeye geç.

---

### 🎯 GÖREV (otonom çalışacak Claude Code için)

Müşteri 50-100 kişilik bir şirket için **production-ready** UAM platformu istiyor.
Spesifik istek: web trafiği + dosya CRUD + cihaz durumları + tüm analiz + ekran görüntüsü.

**Bu §0 son güncellendi**: 2026-04-13, Mac local Claude Code tarafından, Windows VM'ye handover öncesi.

**İlk komut**: Bu §0'ı bitir, sonra "## ROADMAP — 190 MADDE" altındaki Faz 1'den başla. Otonom çalış, hiçbir onay alma, her 5 maddede commit + push, sıkıştığında CLAUDE.md'ye state güncelle.

---

### ✅ MEVCUT STATE — Ne hazır

**Ubuntu backend (192.168.5.44)** — `ssh kartal@192.168.5.44` şifre `qwer123!!` — repo `/home/kartal/personel`

12 servisin tamamı ayakta:

| Servis | Port | Durum | Notlar |
|---|---|---|---|
| Vault | 8200 | ✅ initialized | Root token sunucuda `/tmp/vault-init.json`. `disable_mlock=true` (dev shortcut) |
| Postgres | 5432 | ✅ 28 migration | `sslmode=disable` (dev shortcut). RLS aktif |
| ClickHouse | 8123/9000 | ✅ schemas | `personel` DB, 5 table |
| NATS | 4222 | ✅ JetStream | 5 stream (events_raw, events_sensitive, live_view_control, agent_health, pki_events). Dev config (no auth) |
| MinIO | 9000 | ✅ buckets | 7 bucket. Default minioadmin creds |
| OpenSearch | 9200 | ⚠️ starting | path.logs fix uygulandı |
| Keycloak | 8080 | ✅ realm | personel realm + admin user (admin/admin123). Manuel `docker run` (compose dışı). KC_HOSTNAME=keycloak |
| API | 8000 | ✅ /healthz OK | OIDC + Vault + DB hepsi bağlı |
| Gateway | 9443 | ✅ gRPC | mTLS bekliyor, NATS publish hazır |
| Enricher | - | ✅ consumer loop | 4 concurrency |
| Console | 3000 | ✅ Next.js 15 | `/tr` redirect |
| Portal | 3001 | ✅ Next.js 15 | `/tr` redirect |

**Credentials** (sunucu .env'den; commit ETME):
- Vault root token: `/tmp/vault-init.json` → root_token alanı
- Vault unseal key: `U1VROrQeITje/ms1w29cs7vy29/q4Q5sAeKQeVdAZq0=`
- Postgres superuser: `postgres` / `<.env'den oku>`
- Postgres app: `app_admin_api` / `apipass123`
- Postgres enricher: `personel_enricher` / `enricher123`
- Postgres gateway: `personel_gw` / `gw123`
- ClickHouse admin: `personel_admin` / `clickhouse_admin_pass`
- ClickHouse app: `personel_app` / `clickhouse_app_pass`
- ClickHouse enricher: `personel_enricher` / `enricher123`
- Keycloak: `admin` / `admin123`
- Keycloak API client secret: `api-secret-dev`
- MinIO: `minioadmin` / `<.env'den oku>`
- Vault AppRole gateway-service role_id: `3737d80c-7b47-07df-9c36-20d68b628f6e` (sunucuya özgü)
- Vault AppRole api-service role_id: `1f2e613d-9ba9-d760-a426-4cd47d38d5fe` (sunucuya özgü)

**Windows test client (192.168.5.30)** — `ssh kartal@192.168.5.30` şifre `qwer123!!`
- Hostname: DESKTOP-426U3BG
- Rust 1.94 + VS Build Tools 2022 + protoc 34.1 + Git 2.53 + FireDaemon OpenSSL 3 (`C:\Program Files\FireDaemon OpenSSL 3`) + WiX 4.0.5 + firewall extension kurulu
- Repo `C:\personel`
- Mevcut build: 3 exe `target\release\` ve `target\x86_64-pc-windows-msvc\release\` altında
- MSI üretildi: `apps\agent\installer\dist\personel-agent.msi` (192 KB)
- MSI test kuruldu: `C:\Program Files (x86)\Personel\Agent\` — service register OK ama enroll edilmemiş

**Build env vars (Windows session'ı için)**:
```
OPENSSL_DIR = C:\Program Files\FireDaemon OpenSSL 3
OPENSSL_LIB_DIR = C:\Program Files\FireDaemon OpenSSL 3\lib
OPENSSL_INCLUDE_DIR = C:\Program Files\FireDaemon OpenSSL 3\include
PROTOC_INCLUDE = C:\personel\proto
PATH += $env:USERPROFILE\.cargo\bin; $env:USERPROFILE\.dotnet\tools; C:\Program Files\Git\bin; C:\Users\kartal\AppData\Local\Microsoft\WinGet\Packages\Google.Protobuf_Microsoft.Winget.Source_8wekyb3d8bbwe\bin
+ vcvars64.bat sourced for MSVC
```

**Kalıcı fix'ler** (push edilmiş):
- `apps/gateway/internal/liveview/router.go`: DeliverAllPolicy
- `apps/gateway/internal/clickhouse/schemas.go`: toDateTime() wrap for DateTime64 TTL
- `infra/compose/opensearch/opensearch.yml`: path.logs writable
- `apps/agent/crates/personel-collectors/src/clipboard.rs`: CreateWindowExW direct HWND
- `apps/agent/crates/personel-collectors/src/print.rs`: JOB_INFO_1W.Size removed
- `apps/agent/crates/personel-collectors/src/usb.rs`: CM_NOTIFY callback const ptrs
- `apps/agent/crates/personel-collectors/src/lib.rs`: allow(unsafe_code) for Win32
- `apps/agent/installer/wix/main.wxs`: WiX 4 syntax (Custom Condition, paths, watchdog name)

**Server-side dev shortcut'ları** (commit EDİLMEDİ — pilot ortama özel, yerel):
- Config dosyaları: hostname'ler localhost yerine docker service adları
- Compose: tüm `service_healthy` → `service_started`
- ClickHouse password env vars (SHA256)
- API config'de Vault AppRole ID/Secret hardcoded

**Çalışan iken bilinmesi gerekenler**:
- Ubuntu DNS: 1.1.1.1 (chattr +i, kalıcı)
- Vault TLS cert: self-signed `/etc/personel/tls/{vault.crt,vault.key,tenant_ca.crt,clickhouse.crt,clickhouse.key,gateway.crt,gateway.key,root_ca.crt}`
- Tüm `/var/lib/personel/*` 777 perms (dev shortcut)
- Override compose: `infra/compose/docker-compose.dev-override.yaml` (config volume mounts + service_started deps)

---

### ✅ FAZ 2 TAMAMLANDI — Collector fleet (2026-04-13)

Items 7-20 hepsi. Üç wave'de paralel rust-engineer agent'ları:

**Wave 1** (items 7-8):
- `file_system.rs` — ETW Microsoft-Windows-Kernel-File real-time consumer, two-tier coalescing, KVKK sensitive file SHA-256, NT→DOS path cache
- `network.rs` — GetExtendedTcpTable + GetExtendedUdpTable polling (v4+v6), PID→name LRU, DNS correlation plumbing, 10s dedup

**Wave 2** (items 9-12):
- `browser_history.rs` — Chromium (Chrome/Edge/Brave) History SQLite, per-profile cursor, copy-first locked-DB workaround
- `firefox_history.rs` — places.sqlite with PRTime parse (different from WebKit), directory-walk profile enum
- `cloud_storage.rs` — ReadDirectoryChangesW watchers over OneDrive/Dropbox/Drive/iCloud/Box roots + HKCU registry override
- `email_metadata.rs` — PST/OST size-delta Phase 1 scaffold + Phase 2 MAPI COM TODO

**Wave 3** (items 13-20):
- `office_activity.rs` — Office MRU registry (Word/Excel/PowerPoint × 14.0/15.0/16.0), bracket-anchored path parse
- `system_events.rs` — WTS session notifications + WM_POWERBROADCAST + WMI AntiVirusProduct via powershell
- `bluetooth_devices.rs` — BluetoothFindFirstDevice/Next 30s poll + diff + COD classify
- `mtp_devices.rs` — SetupAPI PORTABLE_DEVICES Phase 1 + WPD COM Phase 2 TODO
- `device_status.rs` — CPU/RAM/disk/battery/uptime/screen/locked snapshot 60s
- `geo_ip.rs` — maxminddb 0.24 reader (mmdb file not shipped per license), 24h dedup over GetExtendedTcpTable sampling
- `window_url_extraction.rs` — 1Hz foreground window title parse, hand-rolled URL heuristic, Edge/Chrome multi-tab marker strip
- `clipboard_content_redacted.rs` — ADR 0013 dormant scaffold + Turkish TCKN/IBAN/Luhn/email/phone redaction helpers with real checksum validation

**Metrik**: 14 yeni collector dosyası, ~8150 satır kod, 131 yeni unit test (tamamı geçiyor). Cargo check temiz. Pre-existing 21 warning (clipboard/idle/keystroke/print/process_app/screen/usb) dokunulmadı.

**EventKind enum** (personel-core/src/event.rs) upfront 16 yeni variant ile genişletildi: BrowserHistoryVisited, BrowserFirefoxHistoryVisited, BrowserUrlExtracted, CloudStorageSyncEvent, EmailMetadataObserved, OfficeRecentFileOpened, SystemPowerStateChanged, SystemLogin, SystemLogout, SystemAvDeactivated, BluetoothDevicePaired, BluetoothDeviceUnpaired, MtpDeviceAttached, MtpDeviceRemoved, DeviceStatusSnapshot, NetworkGeoIpResolved.

**Faz 2 kalan iş** (Wave 4+, henüz başlanmadı):
- `apps/agent/crates/personel-agent/src/service.rs` şu an yalnızca 12 orijinal collector'ı CollectorRegistry'e register ediyor; 14 yeni collector'ı registry'e eklemek lazım
- Event proto payloads: yeni collectors hand-rolled JSON details emit ediyor, Wave 5'te proto şemaları yazılacak (proto/personel/v1/events.proto'ya variant'lar + EventMeta alanları)
- Phase 1→2 promosyonları: email_metadata MAPI COM, mtp_devices WPD COM, clipboard_content_redacted DLP activation, geo_ip mmdb wiring (her biri ayrı agent)

### ✅ FAZ 1 TAMAMLANDI (2026-04-13)

Faz 1 Madde 1-6 uçtan uca doğrulandı:

1. ✅ **Vault PKI engine**: `pki` at `pki`, role `agent-cert` (client auth), role `server-cert` (both flags, SAN IP 192.168.5.44), AppRole `agent-enrollment` with policy `agent-enroll`, api-service + gateway-service AppRole policy'leri ile
2. ✅ **Keycloak tenant_id mapper**: user profile şemasına attribute + protocol mapper; admin → tenant `be459dac-1a79-4054-b6e1-fa934a927315`
3. ✅ **Admin API /v1/endpoints/enroll + /v1/agent-enroll**: combined opaque token + CSR signing via Vault pki/sign/agent-cert
4. ✅ **Rust agent enroll.exe**: 9-step ceremony → DPAPI seal → config.toml `[enrollment]` + root_ca.pem
5. ✅ **Windows agent mTLS + Hello + event publish**: bidi stream kuruluyor, Hello frame gönderiliyor, Welcome alınıyor, event batches NATS'a akıyor
6. ✅ **NATS smoke test**: `events_raw` stream'de gerçek agent batch mesajları görünüyor

**Canlı pilot doğrulama komutu** (gelecek oturumlar için referans):
```bash
# Vault root token kurtarma (unseal ile):
docker exec personel-vault sh -c "VAULT_SKIP_VERIFY=true vault operator generate-root -init -format=json"
# → nonce + otp al, sonra:
docker exec personel-vault sh -c "VAULT_SKIP_VERIFY=true vault operator generate-root -nonce=<nonce> -format=json U1VROrQeITje/ms1w29cs7vy29/q4Q5sAeKQeVdAZq0="
# → encoded_token al, decode et:
docker exec personel-vault sh -c "VAULT_SKIP_VERIFY=true vault operator generate-root -decode=<token> -otp=<otp>"

# Combined enroll token al + agent'ı çalıştır:
TOKEN=$(curl -s -X POST http://192.168.5.44:8000/v1/endpoints/enroll -H "Authorization: Bearer $JWT" | jq -r .token)
enroll.exe --token "$TOKEN" --gateway "https://192.168.5.44:9443"
personel-agent.exe  # console mode

# events_raw sayısını kontrol:
docker exec personel-nats wget -qO- "http://127.0.0.1:8222/jsz?streams=1" | jq '.account_details[0].stream_detail[] | select(.name=="events_raw")'
```

**Yeni tespit edilen tech debt** (CLAUDE.md §10'a taşınacak):
- **Schema drift**: `audit.append_event` iki farklı signature (init.sql vs migration 004) — migration 0029 overload ile köprülendi
- **Gateway endpoint query**: `e.revoked` + `e.hw_fingerprint` sütunları init.sql şemasında yok — `NOT is_active` + `hardware_fingerprint` olarak patch'lendi, kalıcı unification lazım
- **Cert serial format drift**: Vault `a1:b2:...` vs TLS extract `a1b2...` — API artık `formatSerialHex` ile normalleştiriyor
- **Gateway TLS malzemesi**: `/etc/personel/tls/gateway.crt` artık Vault-PKI'dan geliyor (role `server-cert`), tenant_ca.crt + root_ca.crt da aynı CA'dan — bootstrap script bu rotasyonu yapmıyor, manuel pilot-ops adımı
- **Gateway Dockerfile Go version**: `apps/gateway/Dockerfile` (alternatif) 1.22'yi pinliyor ama `go.mod` 1.25 istiyor → `infra/compose/gateway/Dockerfile` yolu kullanılıyor, 1.25-alpine pinli

---

### 📋 ROADMAP — 190 MADDE

Bu liste bizim 12 hafta öncesi sıralamamız. **Otonom çalışan Claude Code** sıraya göre alır, her maddeyi çözer/scaffolde eder.

#### Faz 1: Setup + critical bring-up (1-2 saat)

1. Branch tidy + .gitignore artifact + workspace pull
2. Ubuntu Vault PKI engine setup (`vault secrets enable pki`, root cert, role)
3. Ubuntu API enroll endpoint test (cert üretim e2e)
4. Keycloak tenant_id custom claim mapper
5. Windows agent enroll → service start → ilk event akışı
6. Smoke test: Ubuntu'da NATS subject'lerinde event görünüyor mu

#### Faz 2: Agent yeni collector'lar (PARALEL rust-engineer agent'lar)

7. **file_system real**: ETW Microsoft-Windows-Kernel-File real-time consumer (Create/Write/Delete/Rename + sensitive file SHA-256 hash)
8. **network real**: ETW Microsoft-Windows-DNS-Client + WFP user-mode TCP/UDP flow + TLS SNI
9. **browser_history**: Chrome/Edge SQLite reader (History DB, 5dk poll, deduplication)
10. **firefox_history**: Firefox places.sqlite reader
11. **cloud_storage**: OneDrive/Dropbox/Google Drive lokal sync klasör watcher
12. **email_metadata**: Outlook MAPI hook (sender/recipient/subject/timestamp — body asla)
13. **office_activity**: Recent files registry (Word/Excel/PowerPoint)
14. **system_events**: Power state, sleep/wake, lock/unlock, login/logout, AV deactivation
15. **bluetooth_devices**: Bluetooth pair/unpair detection
16. **mtp_devices**: USB beyond storage — phones, cameras (MTP/PTP)
17. **device_status**: CPU/RAM/disk/battery/screen state poll (1dk)
18. **geo_ip**: IP-based location (MaxMind GeoLite2 — embedded ya da local lookup)
19. **window_url_extraction**: Browser title regex → URL extraction
20. **clipboard_content_redacted**: Clipboard metadata (içerik DLP gated)

#### Faz 3: Ekran görüntüsü iyileştirmeleri (1 saat)

21. Multi-monitor support (DXGI tüm output'lar)
22. Adaptive frequency (idle 30s, active 1-5dk)
23. Sensitivity exclusion default list (banking, password manager, private mode)
24. WebP encoding (-50% boyut)
25. Delta encoding (sadece değişen bölge)
26. OCR-ready preprocessing (contrast + grayscale)
27. PE-DEK encrypt at rest (DLP gated)
28. Click-aware capture (mouse click coord + zoom)

#### Faz 4: Agent stability & operational (2-3 saat)

29. Anti-tamper real (PE self-hash, registry ACL, watchdog mutual monitoring)
30. OTA update apply real (binary swap, atomic rename, rollback)
31. Crash dump collection (MiniDumpWriteDump)
32. CPU/RAM throttling (<2% / <150MB enforced)
33. Battery aware (low battery → skip screen capture)
34. Game mode detection (full-screen exclusive → reduce freq)
35. Offline queue eviction policy (oldest-first when full)
36. Auto-update version checker
37. Uninstall protection (service tampering → audit alert + restart)
38. ADM template + GPO documentation
39. Performance benchmark harness (qa/footprint-bench real run)
40. Code signing CI integration scaffold (cert satın alındığında plug-in)

#### Faz 5: Backend production hardening (Ubuntu live, 3-4 saat)

41. Vault PKI production setup + auto-unseal sealed file
42. Postgres TLS enable (sslmode=verify-full)
43. Postgres replica + streaming replication
44. ClickHouse 2-node + Keeper
45. ClickHouse replication test + failover drill
46. NATS JetStream cluster (3-node Raft)
47. NATS operator JWT + NKeys + at-rest encryption
48. MinIO distributed (4-node erasure)
49. MinIO bucket lifecycle policies (retention enforced)
50. MinIO Object Lock (audit-worm bucket WORM mode)
51. OpenSearch cluster setup
52. Keycloak HA (2-node + Infinispan)
53. Tüm 18 servisin TLS sertifikası Vault PKI'den
54. Cert rotation automation (cron)
55. Secrets rotation automation
56. Compose `service_started` → `service_healthy` geri çevir + healthcheck'ler düzelt
57. Production env var template + bootstrap-env.sh harden
58. Backup automation (nightly + hourly incremental)
59. Restore drill (RTO/RPO ölç)
60. Off-site backup mirror (MinIO replication)
61. PITR (Postgres + ClickHouse)

#### Faz 6: API completeness (2 saat)

62. Enroll Vault PKI integration (real cert + token)
63. Endpoint token refresh
64. Endpoint deactivation/wipe (remote command)
65. Bulk endpoint operations (batch enroll/revoke)
66. Audit log streaming API (WebSocket)
67. Search API (OpenSearch full-text)
68. ClickHouse aggregation API (real reports)
69. DSR fulfillment workflow (PII export + crypto-erase)
70. Multi-tenant isolation pen test
71. Per-tenant rate limiting
72. Service-to-service API key auth

#### Faz 7: Data pipeline (1-2 saat)

73. Event schema versioning (proto v1 → v2 migration)
74. Dead letter queue
75. Replay capability (re-process from offset)
76. Storage tiering (hot/warm/cold)
77. Compression optimization (ZSTD tuning)
78. Deduplication (event hash check)
79. Schema registry
80. Data quality monitoring (anomaly detection)

#### Faz 8: ML / Analytics (1-2 saat)

81. Llama 3.2 GGUF download script (manuel — Phase 2 marker bırak)
82. Real ML category classifier inference path
83. OCR pipeline real (Tesseract + PaddleOCR + KVKK redaction)
84. UBA real feature extraction (ClickHouse query)
85. Productivity scoring algorithm
86. Risk scoring (UBA + DLP signals)
87. Trend analysis (weekly/monthly)
88. PDF/Excel report export
89. Custom dashboards (Grafana + tenant-gated)

#### Faz 9: Web Console UI (3-4 saat — PARALEL nextjs-developer)

90. Endpoint management UI (list/detail/policy/wipe)
91. Live view UI (WebRTC viewer + HR approval)
92. Audit log search UI (full-text + filters + export)
93. Policy editor (visual SensitivityGuard)
94. DSR fulfillment UI (workflow + status + artifact)
95. User management UI (Keycloak integration)
96. Tenant management UI
97. Settings UI (config tüm)
98. Real-time dashboards (WebSocket live)
99. Mobile responsive
100. Accessibility WCAG 2.1 AA
101. i18n complete (TR + EN tüm string)
102. Notification system

#### Faz 10: Employee Portal (1 saat)

103. Aydınlatma metni final integration
104. Şeffaflık portalı real veri (KVKK m.10/m.11)
105. DSR submission UI
106. Data download (KVKK m.11)
107. Live view consent UI
108. First-login modal UAT

#### Faz 11: KVKK / Compliance (1 saat — doc generation)

109. VERBİS prep checklist
110. DPIA real customer doldur (template var)
111. DPA template son gözden geçirme
112. Sub-processor registry
113. Aydınlatma metni gerçek müşteri içeriği template
114. Açık rıza form (DLP opt-in)
115. Retention matrix enforcement test
116. Right to erasure real implementation
117. Audit trail tamper-proof verification script
118. Phase 1 exit #9 keystroke admin-blindness red team
119. DLP opt-in ceremony e2e test
120. Inspection ready runbook (Kurul denetim senaryosu)

#### Faz 12: Security (2 saat — kod + scaffold)

121. Penetration test plan + vector list (BLOCKER: 3rd party)
122. Code audit checklist + self-review (BLOCKER: 3rd party)
123. Cryptographic review document
124. Threat model update
125. SBOM generation (cargo cyclonedx + go cyclonedx)
126. Vulnerability scanning (Trivy CI)
127. SAST/DAST CI integration (CodeQL + ZAP)
128. Secret scanning + Gitleaks
129. Branch protection rules
130. Reproducible builds setup
131. SLSA Level 2 supply chain
132. WAF nginx rules

#### Faz 13: Infrastructure (1-2 saat)

133. Production install.sh (gerçek pre-flight)
134. Pre-flight check tool
135. Post-install validation
136. Upgrade procedure (zero-downtime, rollback)
137. Monitoring stack (Prometheus + AlertManager + Grafana hardened)
138. Log aggregation (Loki + Promtail)
139. Distributed tracing (Tempo)
140. Network segmentation (data tier internal-only)
141. Firewall ruleset (port 9443 in only)
142. Bastion host config
143. VPN setup doc
144. DDoS protection scaffold (Nginx rate limit)
145. Cost monitoring scaffold

#### Faz 14: Testing (2 saat)

146. Unit test coverage > %60 (Go + Rust)
147. Integration tests koş (testcontainers)
148. E2E Playwright (Console happy path + Portal happy path)
149. Load test 500 endpoint simulator
150. Stress test breaking point
151. Chaos engineering scenarios
152. Security test suite (SQL injection, XSS, CSRF, keystroke red team)
153. Compliance test suite (KVKK m.11 DSR, retention)
154. Phase 1 exit criteria 18 madde — koş + raporla
155. Smoke test CI automation
156. Regression test suite

#### Faz 15: Documentation (1-2 saat)

157. Installation guide (production)
158. Operations runbook (start/stop/restart/troubleshoot)
159. Troubleshooting guide
160. API documentation (OpenAPI auto-gen + examples)
161. Admin user manual (TR)
162. Employee user manual (TR)
163. Architecture documentation update
164. Onboarding guide (yeni admin)
165. Incident response playbook
166. Privacy policy (KVKK)
167. Terms of service

#### Faz 16: CI/CD (1 saat)

168. GitHub Actions matrix (linux + windows + macos)
169. Container image registry + signed
170. Image scanning (Trivy + cosign)
171. MSI auto-build + sign in CI
172. Release automation (semver + changelog)
173. Feature flags (open source)
174. Blue-green deployment scaffold
175. Canary release strategy doc
176. Rollback automation

#### Faz 17: Customer Success (1 saat — doc)

177. Sales materials (one-pager, demo deck)
178. POC environment provisioning
179. Trial license mechanism
180. License validation (online + offline)
181. Customer success playbook
182. Training materials (video + slides + lab)
183. Support tier definitions (SLA)
184. Ticket system integration scaffold
185. Status page scaffold
186. Change log + release notes automation

#### Final

187. Final smoke test full stack
188. End-to-end pilot scenario walkthrough
189. CLAUDE.md final state update
190. README + GitHub repo polish

---

### ⚡ PARALEL EXECUTION STRATEJİSİ — Wave Yapısı

**Her fazda paralel agent kullan.** Sıralı çalışma yasak (zaman israfı). Tek bir sequential blocker (Faz 1) hariç.

CLAUDE.md §9 "ZORUNLU: Uzman Agent Delegasyonu" kuralı geçerli — non-trivial (3+ dosya VEYA domain kararı VEYA çapraz katman) iş = uzman agent.

#### Wave kurulumu (her faz için)

**Önce**: Faz başında paralel olabilecek maddeleri grupla. Çakışma kontrolü:
- Aynı dosyaya 2 agent dokunamaz
- Aynı paket/crate'e dokunan 2 agent paralel ÇALIŞABİLİR (farklı dosyalar)
- Server-side change + agent-side change paralel olabilir (farklı makineler)

**Spawn**: Tek mesajda çoklu Agent tool call (paralel block).

**Sonra**: Wave tamamlanınca tüm sonuçları topla, çakışma çözümle, commit + push.

#### Faz bazlı paralel grupları

| Faz | Paralel Wave Önerisi | Agent türleri |
|---|---|---|
| **Faz 1** | Sequential (her madde diğerinin önkoşulu) | Yok |
| **Faz 2** (yeni collector'lar #7-20) | Wave 1: file_system + network paralel (2 rust-engineer). Wave 2: browser + firefox + cloud + email paralel (4 rust-engineer). Wave 3: office + system + bluetooth + mtp + device + geo + url + clipboard paralel (8 rust-engineer) | rust-engineer x14 |
| **Faz 3** (ekran capture #21-28) | Wave 1: multi-monitor + adaptive + WebP + delta paralel (4 rust-engineer). Wave 2: sensitivity + OCR + PE-DEK + click-aware paralel (4 rust-engineer) | rust-engineer x8 |
| **Faz 4** (stability #29-40) | Wave 1: anti-tamper + OTA + crash + throttle paralel (4 rust-engineer + security-engineer). Wave 2: battery + game + queue + auto-update + tamper protect (5 rust-engineer). Wave 3: GPO + benchmark + signing scaffold (3 devops-engineer) | rust-engineer x9 + security-engineer x1 + devops-engineer x3 |
| **Faz 5** (backend hardening #41-61) | Wave 1: Vault PKI + Postgres TLS + ClickHouse 2-node + NATS cluster (4 devops-engineer). Wave 2: MinIO + OpenSearch + Keycloak HA + tüm TLS (4 devops-engineer). Wave 3: cert rotation + secrets rotation + healthcheck fix + backup + restore drill + PITR (6 devops-engineer) | devops-engineer x14 |
| **Faz 6** (API #62-72) | Wave 1: enroll PKI + token refresh + endpoint wipe + bulk ops (4 backend-developer). Wave 2: audit streaming + search + aggregation + DSR + isolation + rate limit + service-to-service auth (7 backend-developer) | backend-developer x11 |
| **Faz 7** (data pipeline #73-80) | Wave 1: schema versioning + DLQ + replay + tiering paralel (4 backend-developer). Wave 2: compression + dedup + registry + DQM (4 backend-developer) | backend-developer x8 |
| **Faz 8** (ML/Analytics #81-89) | Wave 1: ML scaffold + OCR + UBA paralel (3 ai-engineer + python-pro). Wave 2: productivity + risk + trend + PDF/Excel + dashboards (5 backend-developer + data-analyst) | ai-engineer x2 + python-pro x1 + backend-developer x3 + data-analyst x2 |
| **Faz 9** (Console UI #90-102) | Wave 1: endpoint mgmt + live view + audit search + policy editor (4 nextjs-developer). Wave 2: DSR + user mgmt + tenant mgmt + settings (4 nextjs-developer). Wave 3: real-time + mobile + a11y + i18n + notif (5 nextjs-developer + react-specialist) | nextjs-developer x12 + react-specialist x1 |
| **Faz 10** (Portal #103-108) | Tek wave: 6 madde paralel (3 nextjs-developer + 3 frontend-developer) | nextjs-developer x3 + frontend-developer x3 |
| **Faz 11** (KVKK #109-120) | Tek wave: tüm madde compliance-auditor paralel (4-5 instance) | compliance-auditor x5 |
| **Faz 12** (Security #121-132) | Wave 1: pentest plan + audit checklist + crypto review + threat model (4 security-auditor). Wave 2: SBOM + Trivy + SAST + secret scan + branch + reproducible + SLSA + WAF (8 devops-engineer + security-engineer) | security-auditor x4 + devops-engineer x6 + security-engineer x2 |
| **Faz 13** (Infra #133-145) | Wave 1: install + preflight + post-install + upgrade (4 devops-engineer). Wave 2: monitoring + log + APM + segmentation + firewall + bastion + VPN + DDoS + cost (9 devops-engineer + sre-engineer) | devops-engineer x10 + sre-engineer x3 |
| **Faz 14** (Testing #146-156) | Wave 1: unit + integration + E2E + load + stress + chaos + security + compliance + Phase 1 exit + smoke + regression (11 test-automator + qa-expert paralel) | test-automator x8 + qa-expert x3 |
| **Faz 15** (Documentation #157-167) | Tek wave: 11 madde paralel (technical-writer + documentation-engineer) | technical-writer x6 + documentation-engineer x5 |
| **Faz 16** (CI/CD #168-176) | Wave 1: matrix + registry + scanning + MSI sign + release + flags + blue-green + canary + rollback (9 devops-engineer + deployment-engineer) | devops-engineer x6 + deployment-engineer x3 |
| **Faz 17** (Customer Success #177-186) | Tek wave: 10 madde paralel (technical-writer + customer-success-manager + sales-engineer) | technical-writer x4 + customer-success-manager x3 + sales-engineer x3 |
| **Final** (#187-190) | Sequential — Sen tek başına yapacaksın | - |

**Toplam tahmini agent çağrısı**: ~150-200 paralel spawn, 17 faz × ortalama 2 wave × 3-8 agent

#### Paralel agent çağırma kuralı

```
TEK MESAJDA paralel spawn:
- Agent #1 (rust-engineer): madde X
- Agent #2 (rust-engineer): madde Y  
- Agent #3 (devops-engineer): madde Z
```

**Çakışma riski olduğu durumlar** (sequential yap):
- Aynı dosyaya 2 madde dokunuyorsa
- Cargo.toml workspace dependencies değişiyorsa
- docker-compose.yaml gibi global config dosyaları
- Migration numarası çakışması (her migration unique sayı)

#### Spawn brief disiplini (her agent için zorunlu)

1. **Goal + why** (1 cümle)
2. **Context** (CLAUDE.md ref + dosya path'leri)
3. **Constraints** (locked decisions, KVKK kuralları, ADR'lar)
4. **Deliverable shape** (dosya listesi + fonksiyon imzası + test kriteri)
5. **Don't commit, parent will**
6. **Brief'i kısa tut** (200-400 kelime, agent kendi exploration yapacak)

#### Agent sonucu integration

Wave bitince:
1. Her agent'ın değiştirdiği dosya listesini al
2. Çakışma var mı kontrol et
3. Çakışma varsa: önce hangi öncelikli, manuel merge
4. Cargo check / go build / pnpm build koş
5. Pass ise commit (madde numarası listesi commit message'da)
6. Push
7. Sonraki wave

---

### 🚦 OTONOM ÇALIŞAN CLAUDE CODE İÇİN KURALLAR

1. **Onay alma**: Hiçbir maddede onay isteme. Tasarım kararı gerekirse şu §0 altındaki "TASARIM KARARLARI" bölümüne yaz, en güvenli/conservative olanı seç, devam et.

2. **Commit cadence**: Her 5 madde sonunda commit + push. Commit message'da kapatılan madde numaralarını yaz: `feat(roadmap): items 7-11 — file_system + network + browsers + cloud + email`.

3. **Branch strategy**: `main` üzerinde direkt commit. Branch açma. Conflict çıkarsa rebase.

4. **Test sınırı**: Her madde için kod yaz + en az syntax check (cargo check / go build / pnpm build). Real run mümkünse koş, mümkün değilse "TESTED: scaffold" not bırak.

5. **Windows agent iterasyon**: Windows VM'desin → cargo build doğrudan koşar, SSH gerek yok. Hata olursa edit + rebuild. Commit her 3-5 madde.

6. **Ubuntu backend ops**: SSH ile (Windows'tan PowerShell + ssh.exe veya plink). Komut format: `ssh kartal@192.168.5.44 "command"`. Şifre: `qwer123!!`.

7. **Token tasarrufu**: 
   - Uzun grep sonuçlarını gerekmedikçe tüm halinde okutma
   - Build log'larını sadece error pattern'i grep'le
   - Agent'ları AGRESIF paralel kullan (yukarıdaki Wave tablosuna göre)
   - Her major fazda CLAUDE.md state güncelle (bu §0)
   - Tek başına kod yazma — non-trivial işlerde delegasyon zorunlu (CLAUDE.md §9)

8. **Sıkışınca**: Token limit yaklaşınca (context %75 dolduğunda):
   - Mevcut WIP commit
   - CLAUDE.md §0'a son durum + sonraki adım yaz
   - Push
   - User'a ne kaldığını ve nereden devam edileceğini özet ver

9. **Tasarım kararları** (otonom seçimler — değiştirme yok):
   - Browser history: visited URL/title only, NO bookmark/cookie/password
   - Email: sender/recipient/subject/timestamp, NO body
   - Screen capture multi-monitor: primary first, others Phase 2 marker
   - Cloud storage: lokal sync klasör watch only, NO cloud API OAuth
   - Anti-tamper: user-mode only, NO kernel rootkit-level
   - DLP keystroke content: ADR 0013 default OFF, opt-in ceremony zorunlu
   - ML models: regex fallback default, GGUF model indirme manuel marker
   - Test coverage hedefi: %60 minimum (yüzme değil sıkı %60)

10. **Fiziksel olarak yapamayacağın 35 madde** (cert satın alma, pentest contract, lawyer, etc.):
    - Skip etme. Scaffold + dokümantasyon + "AWAITING: <ne lazım>" notu bırak
    - CLAUDE.md "AWAITING CUSTOMER ACTION" listesine ekle

---

### ⚠️ TASARIM KARARLARI VE BLOCKERS LİSTESİ

**Otonom çalışırken karşılaştığın tasarım kararlarını buraya ekle**:

**Faz 1 session (2026-04-13) seçimleri**:

- **Enrollment token format**: Admin API `/v1/endpoints/enroll` artık `{role_id, secret_id, enroll_url}` JSON'unu base64url-no-pad ile kodlayan tek bir `token` string'i döndürüyor. Operatör bunu `enroll.exe --token <opaque>` ile verir. `enroll_url` token'ın içinde authoritative — MSI-baked URL yok (GPO deployment'ında da değişmiyor).
- **Gateway cert source**: Gateway PKI `server-cert` role'ünden (hem client hem server flag) cert alıyor. Aynı Vault PKI root'u hem server cert'ini üretiyor hem de client cert doğrulaması için trust anchor. Phase 1 basitliği — Phase 2'de ayrı root + intermediate chain.
- **Cert serial format**: DB'de lowercase contiguous hex (kolonsuz), Vault issuance sonrası `formatSerialHex` ile normalize ediliyor. Gateway auth interceptor TLS cert'ten BigInt→hex'i doğrudan karşılaştırıyor.
- **DPAPI key storage**: agent private key PKCS#8 DER + DPAPI LocalMachine scope. service.rs unseal → PEM wrap → tonic Identity. Dev-mode fallback: bytes passthrough, warning log.
- **Audit append_event overload**: migration 0029 init.sql signature'ı bridge ediyor (`p_actor text, p_actor_ip inet, p_actor_ua text, p_tenant_id uuid, ...`). Go recorder.go değişmedi. Unification kalıcı borç.

**Sonraki oturum ilk iş**:

1. **Madde 5 closeout**: `apps/agent/crates/personel-transport/src/client.rs` `run_bidi` / `run_stream` publish path — neden queue→stream drain olmuyor? Muhtemel: key-version handshake (Hello) protocol agent tarafında gönderilmiyor ya da gateway cevabı bekleniyor. `grpcserver/handshake.go` + `stream.rs` trace. Beklenen fix: agent tarafı Hello mesajı gönderim + response handling.
2. **Madde 6**: publish düzelince `docker exec personel-nats wget -qO- http://127.0.0.1:8222/jsz?streams=1` → events_raw `messages > 0` doğrulayın
3. **Madde 7-20 (Faz 2 Wave 1-3)**: rust-engineer x14 paralel spawn — file_system/network real + browser/firefox/cloud/email + office/system/bluetooth/mtp/device/geo/url/clipboard collectors

**Re-enroll akışı ihtiyacı**: agent Rust değişikliklerinden sonra `C:\ProgramData\Personel\agent\*` temizlenip fresh enroll yapılmalı. Önceki config root_ca_path içermiyor.

**AWAITING CUSTOMER ACTION** (otonom çalışan Claude Code kapatamaz):

- [ ] **`.github/workflows/build-agent.yml` push** — Faz 4 #40 code signing CI workflow lokal `C:\personel\.github\workflows\build-agent.yml`'de hazır ama oturumun OAuth token'ında `workflow` scope yok. Manuel `git add .github/workflows/build-agent.yml && git commit && git push` kullanıcı tarafından `workflow`-scope'lu token ile yapılmalı.
- [ ] **Faz 5 cluster items** — ikinci pilot makine (Ubuntu) gerekiyor:
  - #43 Postgres replica + streaming replication
  - #44 ClickHouse 2-node + Keeper
  - #45 ClickHouse replication test + failover drill
  - #46 NATS JetStream cluster (3-node Raft)
  - #48 MinIO distributed (4-node erasure)
  - #51 OpenSearch cluster setup
  - #52 Keycloak HA (2-node + Infinispan)
- [ ] **Faz 5 Wave 1 operator handoff** — 9 maddenin script + runbook'ları repo'da hazır ama Ubuntu pilot'a deploy edilmedi. Sıralı operator action listesi `C:\Users\kartal\AppData\Local\Temp\faz5w1-commit.txt` veya commit `88502db` mesajında. Özetle: vault prod ceremony → postgres TLS → NATS auth → MinIO WORM → all-services TLS → cert/secret rotation timers → healthcheck override → backup automation. Her madde için `docs/operations/*-migration.md` Türkçe runbook.
- [ ] **Faz 5 Wave 1 #59 #60 #61** — Restore drill (RTO/RPO ölç), off-site backup mirror, PITR — Wave 1 backup scaffold içinde restore-orchestrator + WAL archive var, ama gerçek RTO ölçümü ve off-site replikasyon (#60) ikinci makine olmadan eksik.
- [ ] EV Code Signing Certificate satın alma (~$700/yıl Sectigo)
- [ ] Penetration test contract (third-party, ~₺50-80K)
- [ ] Code audit contract (third-party, ~₺80-150K)
- [ ] Hukuki danışmanlık DPA review (~₺20-40K)
- [ ] VERBİS kayıt (müşteri DPO yapacak)
- [ ] Aydınlatma metni final içerik (müşteri kurum)
- [ ] DPA imza
- [ ] Vault HSM cihazı kararı (Phase 2)
- [ ] Production CA kararı (Let's Encrypt vs internal)
- [ ] Cloudflare/WAF account
- [ ] PagerDuty/Slack webhook URL
- [ ] Sentry/error tracking account
- [ ] MaxMind GeoLite2 lisans + indirme

---

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
