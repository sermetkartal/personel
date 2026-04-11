# Vendor / Third-Party Management Policy

> **Belge sahibi**: Compliance Officer, co-owned by CTO
> **Versiyon**: 1.0 — 2026-04-10
> **Sonraki gözden geçirme**: 2027-04-10
> **İlgili**: SOC 2 CC9.2 (ADR 0023), ISO 27001:2022 A.5.19–A.5.23 & A.8.30, GDPR Art. 28, KVKK m.8–9, ADR 0022 (GDPR expansion)

## 1. Amaç ve Kapsam

Personel'in üçüncü taraf tedarikçilerinin (yazılım bağımlılıkları, hizmet sağlayıcıları, entegrasyon hedefleri) oluşturduğu riskleri yönetir. Hem **self-hosted açık kaynak bileşenleri** (Keycloak, Vault, MinIO, PostgreSQL, ClickHouse) hem **harici üçüncü taraf servisleri** (BambooHR, Logo Tiger, Splunk, Microsoft Sentinel, Phase 3 SaaS için bulut sağlayıcı) kapsar.

## 2. Policy Statement

Hiçbir tedarikçi, risk değerlendirmesi yapılmadan ve (kişisel veri işliyorsa) DPA imzalanmadan üretimde kullanılamaz. Kritik tedarikçiler yıllık, önemli olanlar iki yılda bir gözden geçirilir.

## 3. Classification / Tedarikçi Sınıflandırması

| Sınıf | Tanım | Örnekler |
|---|---|---|
| **Critical** | Kesintisi müşteri servisini durdurur veya kompromise olması hash-chain audit bütünlüğünü, kriptografik temelleri veya üretim verilerini etkiler | Vault, PostgreSQL, ClickHouse, MinIO, Keycloak (self-hosted); Phase 3 SaaS için bulut sağlayıcı (IaaS), HSM sağlayıcı |
| **Important** | Önemli özellikleri devre dışı bırakır ama servis çalışmaya devam eder | NATS JetStream, OpenSearch, LiveKit, ML model sağlayıcı (Llama), Tesseract/PaddleOCR upstream |
| **Standard** | Opsiyonel entegrasyonlar, kişisel veri işleyebilir ama tek point-of-failure değil | BambooHR, Logo Tiger (HRIS), Splunk HEC, Microsoft Sentinel DCR |
| **Low-risk** | Minimal veri etkisi, kolayca değiştirilebilir | Grafana, Prometheus, statüs sayfası sağlayıcı, e-posta SMTP gateway |

## 4. Due Diligence Requirements / Durum Tespiti

| Gereksinim | Critical | Important | Standard | Low-risk |
|---|---|---|---|---|
| Security questionnaire (CAIQ / SIG-Lite) | ✅ | ✅ | ✅ | optional |
| SOC 2 veya ISO 27001 report review | ✅ mandatory | ✅ if available | preferred | optional |
| Penetration test özet / kanıt | ✅ | preferred | — | — |
| Financial viability review | ✅ | — | — | — |
| Legal review (sözleşme, SLA) | ✅ CO + Legal | ✅ CO | ✅ CO | CO self-review |
| DPA (GDPR Art. 28 / KVKK m.8–9) | ✅ if processes PD | ✅ if processes PD | ✅ if processes PD | ✅ if processes PD |
| Sub-processor disclosure | ✅ | ✅ | ✅ | — |
| Data residency attestation | ✅ (EU pinning) | ✅ | ✅ | — |
| Exit / data return clause | ✅ | ✅ | ✅ | — |

## 5. DPA (Data Processing Agreement) Requirement

Kişisel veri işleyen HER tedarikçi için DPA zorunludur. DPA içeriği GDPR Art. 28(3) minimum:

- İşleme konusu, süresi, niteliği ve amacı
- Kişisel veri türü + veri sahibi kategorisi
- Controller'ın yükümlülükleri ve hakları
- Processor'ın yükümlülükleri (Art. 28(3)(a)–(h))
- Sub-processor koşulları (Art. 28(2), (4))
- Uluslararası transfer koşulları (SCC — Phase 3 EU)
- Audit hakkı (Art. 28(3)(h))
- Veri iadesi / silme

KVKK m.8–9 için ek: Kurul'un "aktarım sözleşmesi" koşulları ve VERBİS tutarlılığı.

DPA şablonu: Phase 3.3'te `docs/compliance/dpa-template.md` olarak üretilir (ADR 0022 kapsamında).

## 6. Ongoing Monitoring / Sürekli İzleme

| Sınıf | Gözden Geçirme Sıklığı | İçerik |
|---|---|---|
| Critical | **Yıllık** (ve belirgin olay sonrası) | Yeni SOC 2/ISO raporları, yeni sub-processor'lar, incident history, financial health, sözleşme yenileme |
| Important | İki yılda bir (biennial) | Sertifikasyon güncellemesi, incident history |
| Standard | İki yılda bir | Güvenlik soruları + DPA doğrulama |
| Low-risk | Ad-hoc (olay tetikli) | — |

Tedarikçi güvenlik olayı bildirimleri Security Lead tarafından izlenir (vendor advisories, CVE feed, güvenlik bültenleri). Ciddi vendor olayları IR tetikler (`docs/policies/incident-response.md`).

## 7. Initial Vendor Inventory / Başlangıç Envanteri

> Phase 3.0 başlangıcında CO tarafından doldurulur. Bu tabloda yer alan her satır ayrı bir vendor record'a (`docs/compliance/vendor-records/`) bağlanır.

| Vendor | Sınıf | Kişisel veri? | DPA? | Sub-processor disclosure | Data residency | Son review |
|---|---|---|---|---|---|---|
| HashiCorp Vault (OSS) | Critical | Indirect (key wraps) | N/A (self-hosted) | N/A | Self-hosted | TBD |
| PostgreSQL (OSS) | Critical | Yes | N/A (self-hosted) | N/A | Self-hosted | TBD |
| ClickHouse (OSS) | Critical | Yes | N/A (self-hosted) | N/A | Self-hosted | TBD |
| MinIO (OSS) | Critical | Yes (screenshots) | N/A (self-hosted) | N/A | Self-hosted | TBD |
| Keycloak (OSS) | Critical | Yes (identity) | N/A (self-hosted) | N/A | Self-hosted | TBD |
| NATS JetStream (OSS) | Important | Transient | N/A (self-hosted) | N/A | Self-hosted | TBD |
| OpenSearch (OSS) | Important | Yes (audit fulltext) | N/A (self-hosted) | N/A | Self-hosted | TBD |
| LiveKit (OSS) | Important | Yes (live view stream) | N/A (self-hosted) | N/A | Self-hosted | TBD |
| Llama 3.2 (Meta, open weights) | Important | No (classification only) | N/A (local inference) | N/A | Self-hosted | TBD |
| Tesseract / PaddleOCR | Important | No (local) | N/A | N/A | Self-hosted | TBD |
| BambooHR | Standard (optional) | Yes | Required | Required | US (transfer risk TBD) | TBD |
| Logo Tiger | Standard (optional) | Yes | Required | Required | TR | TBD |
| Splunk HEC | Standard (optional) | Indirect (event metadata) | Required | Required | Customer-chosen | TBD |
| Microsoft Sentinel | Standard (optional) | Indirect | Required | Required | Customer-chosen | TBD |
| Phase 3 SaaS cloud provider | Critical (Phase 3+) | Yes | Required | Required | EU + TR regions | TBD |

## 8. Sub-Processor Disclosure

GDPR Art. 28(2) gereği: Personel SaaS edisyonunda kullanılan her sub-processor müşterilere açıklanmalıdır.

- `docs/compliance/sub-processor-registry.md` (Phase 3.3'te üretilir) public / customer-portal'da yayımlanır
- Müşteriye **30 gün önceden bildirim** ile yeni sub-processor ekleme hakkı saklıdır (itiraz hakkı ile)
- İtiraz süreci: müşteri DPA feshedebilir veya eklemeye itiraz ederse ek dengeleyici kontroller görüşülür

## 9. Offboarding Procedure

Bir tedarikçi çıkarılırken:

1. **90 gün önceden bildirim** (sözleşme imkan veriyorsa)
2. Veri iadesi veya silme talebi — tedarikçiden yazılı onay (GDPR Art. 28(3)(g))
3. API token / credential revocation (Vault rotation)
4. Müşteri iletişimi (sub-processor ise)
5. Post-exit doğrulama: tedarikçiden silme attestation'ı (kanıt talep edilir)
6. Audit chain'e offboarding kaydı
7. Vendor record 3 yıl arşiv retention

## 10. Geographic Considerations / Coğrafi

- **EU müşterileri**: EU veri sakinliği zorunludur; EU dışı sub-processor sadece SCC + transfer impact assessment ile
- **TR müşterileri**: KVKK Kurul'un aktarım listesi + m.9 koşulları
- Phase 3.4'te region pinning (ADR 0021) uygulanır

## 11. Responsibilities

| Rol | Sorumluluk |
|---|---|
| CO | Envanter bakımı, review takvimi, kayıt saklama |
| Legal | DPA müzakeresi + sözleşme gözden geçirme |
| DPO | Kişisel veri işleme tedarikçileri için ek inceleme |
| Security Lead | Teknik güvenlik değerlendirmesi + CVE/feed izleme |
| Platform Lead | Entegrasyon, credential rotation |
| CTO | Critical vendor onayları |

## 12. Exceptions

Acil iş gereği olan vendor onboarding'i için CO + CTO geçici onay verebilir (max 30 gün), bu süre içinde tam durum tespiti yapılır.

## 13. Related Documents

- ADR 0022 — GDPR expansion (DPA template)
- ADR 0018 — HRIS connector interface (credential storage)
- ADR 0023 — SOC 2 CC9.2
- ADR 0024 — ISO 27001 A.5.19–A.5.23
- `docs/security/risk-register.md` §5.5
- `docs/compliance/kvkk-framework.md` §8

## 14. Review Cycle

Yıllık policy review + classification-based vendor review schedule.

## 15. Approval

| Rol | Ad | İmza | Tarih |
|---|---|---|---|
| CEO | _______ | _______ | _______ |
| CTO | _______ | _______ | _______ |
| DPO | _______ | _______ | _______ |
| CO | _______ | _______ | _______ |

---

## English Summary

This policy implements SOC 2 CC9.2, ISO 27001:2022 A.5.19–A.5.23 & A.8.30, GDPR Article 28, and KVKK Articles 8–9. Vendors are classified Critical / Important / Standard / Low-risk with due diligence requirements scaling by class. A DPA is mandatory for any vendor processing personal data. Critical vendors are reviewed annually; important biennially. The initial vendor inventory includes self-hosted open source components (Vault, PostgreSQL, ClickHouse, MinIO, Keycloak, NATS, OpenSearch, LiveKit, Llama, Tesseract) treated as supply-chain risks even without DPAs, and optional third-party integrations (BambooHR, Logo Tiger, Splunk, Microsoft Sentinel) requiring full DPA treatment. Sub-processor disclosure follows GDPR Art. 28(2) with 30-day customer notice and objection rights. Offboarding requires written data-return/destruction attestation. Geographic considerations follow ADR 0021 region pinning and transfer impact assessments for non-EU sub-processors.
```

---
