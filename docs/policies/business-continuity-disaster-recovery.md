# Business Continuity & Disaster Recovery Policy

> **Belge sahibi**: CTO, co-owned by Platform Lead
> **Versiyon**: 1.0 — 2026-04-10
> **Sonraki gözden geçirme**: 2027-04-10 veya live DR drill sonucu
> **İlgili**: SOC 2 A1.2 / A1.3 / CC9.1 (ADR 0023), ISO 27001:2022 A.5.29 & A.5.30 (ICT continuity), ISO 22301 prensipleri, `infra/runbooks/backup-restore.md`, `infra/runbooks/disaster-recovery.md`, ADR 0020 (SaaS multi-tenant), ADR 0021 (K8s deployment)

## 1. Amaç ve Kapsam

Personel servislerinin planlı ve plansız kesintilerden kurtarılması için sorumluluk, hedef ve prosedürleri tanımlar. Kapsam:

- Phase 3 SaaS üretim ortamı (birincil odak)
- Personel'in kendi destek altyapısı (intranet, wiki, ticket system)
- Müşteri on-prem kurulumları: Personel destek ekibi yalnızca danışmanlık verir; BCP operasyonel sorumluluğu müşteridedir

## 2. Policy Statement

Personel, her servis katmanı için tanımlı bir kritiklik derecesi, RTO (Recovery Time Objective), RPO (Recovery Point Objective) ve doğrulanmış bir kurtarma prosedürü sağlar. Bu politika, `infra/runbooks/backup-restore.md` ve `infra/runbooks/disaster-recovery.md` runbook'larını governance bağlamına yerleştirir — runbook'ları tekrar etmez.

## 3. Criticality Tiering / Kritiklik Katmanlaması

| Tier | Servisler | İş etkisi |
|---|---|---|
| **Tier 0 — Kritik bütünlük** | Vault (PKI + TMK), hash-chained audit chain (PostgreSQL audit + WORM sink) | Tier 0 kaybı kriptografik + compliance bütünlüğü kırar. Geri gelmezse ürün yasal olarak çalışamaz. |
| **Tier 1 — Kritik servis** | Gateway (ingest), Admin API, PostgreSQL metadata, Keycloak | Tier 1 kaybı üretim kullanımını durdurur |
| **Tier 2 — Kullanıcı arayüzü ve analitik** | Admin Console, Transparency Portal, ClickHouse, MinIO | Tier 2 kaybı analitik ve görüntüyü etkiler ama ingest devam edebilir |
| **Tier 3 — Opsiyonel servisler** | ML classifier, OCR service, UBA detector, LiveKit, live view recording | Degraded mode acceptable |

## 4. RTO / RPO Targets

| Tier | RTO | RPO | Test cadence |
|---|---|---|---|
| Tier 0 | **2 saat** | **0 (zero-loss: WORM replication + sync writes)** | Çeyreklik restore drill + yıllık live DR |
| Tier 1 | **4 saat** | **15 dakika** | Çeyreklik restore drill |
| Tier 2 | **8 saat** | **1 saat** | Yarıyıl restore drill |
| Tier 3 | **24 saat** | **4 saat** | Yıllık restore drill |

Bu hedefler SOC 2 ADR 0023 CC9.1'de yayımlanan "RTO 4h / RPO 15min" (Tier 1 düzleminde) değerleriyle uyumludur; Tier 0 daha sıkıdır.

## 5. Backup Strategy (özet; detaylı runbook: `infra/runbooks/backup-restore.md`)

- **PostgreSQL**: WAL streaming + base backup; cross-region replica; nightly pg_dump → object-locked S3
- **ClickHouse**: native backup, cross-region replica (Phase 1 exit criterion #17)
- **MinIO**: versioning + object lock (compliance mode); cross-region sync
- **Vault**: Integrated Storage snapshot daily; Shamir key material offline + escrow
- **Keycloak**: realm export nightly + PostgreSQL backup
- **NATS JetStream**: at-rest encrypted storage snapshot
- **Config & secrets**: Git (encrypted where needed) + Vault snapshot
- **Hash-chained audit**: WORM sekonder sink (ADR 0014) + primary PostgreSQL backup

Backup bütünlüğü SHA-256 ile doğrulanır; her backup için restore-smoke otomatik koşar.

Backup şifreleme anahtarları ayrı bir key hierarchy seviyesinde (BCK-K) Vault'ta tutulur; rotation quarterly.

## 6. Recovery Scenarios / Senaryolar

> Her senaryo için `infra/runbooks/disaster-recovery.md` ayrıntılı adımları sunar. Bu bölüm governance düzeyindeki hedefi ve sahipliği listeler.

### 6.1 Ransomware (kurumsal IT veya SaaS node'u)

- Sahip: Security Lead + Platform Lead
- Aksiyon: etkilenen node segmentasyon, backup'tan restore, forensic image
- Backup'lar object-lock olduğu için ransomware tarafından şifrelenemez
- Restore hedefi: Tier 1 RTO 4 saat

### 6.2 Vault corruption / compromise (R-DAT-003)

- Sahip: Security Lead + CISO + CEO (break-glass)
- Aksiyon: Shamir unseal with escrow keys, snapshot restore, full PKI rotation if compromise confirmed
- RPO: 0 (integrated storage + daily snapshot)
- Hash chain cross-verified; tüm mTLS sertifikaları re-issued
- Müşteri on-prem etkilenen ise rotation ceremony müşteri başına koşturulur

### 6.3 ClickHouse loss (R-DAT-001)

- Sahip: Data Lead + Platform Lead
- Aksiyon: cross-region replica promotion, backup'tan replay
- Ingest gateway otomatik bekleme modu (NATS JetStream buffer, 24 saatlik retention)
- RPO: 15 dakika; RTO: 4 saat

### 6.4 Multi-region / AZ failure (Phase 3 SaaS)

- Sahip: Platform Lead + CTO
- Aksiyon: failover region'a promote, DNS geçiş, müşteri iletişimi
- RTO: Tier 1 4 saat; Tier 0 2 saat
- EU müşterileri için: failover hedef region EU içinde kalmalı (residency)

### 6.5 Full-site loss (tüm birincil region)

**Recovery öncelik sırası**:

1. Vault (Tier 0) — kriptografik temel
2. WORM audit sink (Tier 0) — bütünlük doğrulama
3. PostgreSQL (Tier 1) — metadata, RBAC, policies
4. Keycloak (Tier 1) — kimlik
5. Gateway (Tier 1) — ingest resume
6. Admin API (Tier 1)
7. ClickHouse (Tier 2) — analitik
8. MinIO (Tier 2)
9. Console + Portal (Tier 2)
10. ML, OCR, UBA, LiveKit (Tier 3)

## 7. Communications Plan (outage)

- Status page: ilk 15 dakika içinde güncellenir
- Müşteri e-postası: Tier 0/1 kesintisi için 30 dakika içinde
- Regulatory bildirimi: eğer kesinti veri kaybı ile birlikte ise incident response politikası tetiklenir (§72h KVKK + GDPR)
- Communications Lead Slack #status kanalını yönetir
- CEO Tier 0 kesintisinden haberdar edilir (≤ 15 dk)

## 8. Responsibility Matrix (RACI özet)

| Aktivite | CEO | CTO | Platform Lead | Security Lead | DPO | CO |
|---|---|---|---|---|---|---|
| BCP policy ownership | A | R | C | C | C | C |
| Backup operations | I | A | R | C | I | I |
| Restore drill | I | A | R | C | I | I |
| DR declaration | A | R | C | C | I | I |
| Customer communication | A | C | I | I | C | I |
| Regulator notification | A | I | I | C | R | C |
| Post-incident review | A | R | C | R | R | R |

(R=Responsible, A=Accountable, C=Consulted, I=Informed)

## 9. Test Cadence / Test Sıklığı

| Test türü | Sıklık | Sahip | Çıktı |
|---|---|---|---|
| Backup restore smoke | Nightly (otomatik) | Platform Lead | CI yeşil/kırmızı |
| Tier 1 restore drill | Çeyreklik | Platform Lead | Test raporu, retrospective |
| Tier 0 restore drill (Vault + audit) | Çeyreklik | Security Lead | Test raporu |
| **Full DR drill (live)** | **Yıllık** | CTO | Drill raporu, management review |
| BCP tabletop | Çeyreklik | CO | Tutanak |

Yıllık live drill sonuçları management review'e sunulur (ADR 0024).

## 10. Multi-Region Considerations (Phase 3 SaaS)

Phase 3.4 multi-region (ADR 0020/0021):

- EU region + TR region ayrı tenant plane'ları
- Cross-region replication: backup-only (runtime cross-region trafik yok)
- Failover region seçimi residency kısıtına uyar:
  - EU tenant → EU alternatif region
  - TR tenant → TR alternatif region (veya müşteri onayı ile EU)
- DNS geofailover yerine manuel promote (veri sakinliği garantisi)

## 11. Exceptions

Kabul edilen tier degradation durumları:
- Yeni müşteri onboarding ilk 30 gün (Tier 2 hedefleri geçici gevşetme, CO kabul)
- Planlı bakım pencereleri (iş dışı saat, önceden bildirim)

Her istisna CO + CTO imzasıyla belgelenir.

## 12. Related Documents

- `infra/runbooks/backup-restore.md`
- `infra/runbooks/disaster-recovery.md`
- `docs/security/runbooks/worm-audit-recovery.md`
- `docs/security/runbooks/pki-bootstrap.md`
- `docs/security/runbooks/vault-setup.md`
- ADR 0020 / 0021 (Phase 3 SaaS + K8s)
- ADR 0014 (WORM audit sink)
- `docs/policies/incident-response.md`
- `docs/security/risk-register.md` §5.3, §5.5

## 13. Review Cycle

- Policy yıllık review
- RTO/RPO hedefleri yıllık review ve her major arch change sonrası
- Yıllık live DR drill sonrası policy güncellenir (lessons learned)

## 14. Approval

| Rol | Ad | İmza | Tarih |
|---|---|---|---|
| CEO | _______ | _______ | _______ |
| CTO | _______ | _______ | _______ |
| CISO | _______ | _______ | _______ |
| Platform Lead | _______ | _______ | _______ |
| DPO | _______ | _______ | _______ |

---

## English Summary

This policy implements SOC 2 A1.2 / A1.3 / CC9.1, ISO 27001:2022 A.5.29 / A.5.30, and aligns with ISO 22301 principles. Services are tiered as Tier 0 (Vault + hash-chained audit; RTO 2h / RPO 0), Tier 1 (Gateway, API, PostgreSQL, Keycloak; RTO 4h / RPO 15min), Tier 2 (Console, Portal, ClickHouse, MinIO; RTO 8h / RPO 1h), and Tier 3 (ML, OCR, UBA, LiveKit; RTO 24h / RPO 4h). Backup strategy is detailed in `infra/runbooks/backup-restore.md`; this policy governs ownership, cadence, and test obligations. Defined scenarios include ransomware, Vault compromise, ClickHouse loss, AZ failure, and full-site loss with explicit recovery priority order. Test cadence is quarterly restore drills for Tier 0/1, biannual for Tier 2, annual for Tier 3, plus a mandatory annual live DR drill and quarterly tabletop. Multi-region planning for Phase 3.4 honours data residency: EU tenants fail over within EU, TR tenants within TR (or EU with explicit customer consent).
