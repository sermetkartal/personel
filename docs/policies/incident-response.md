# Incident Response Policy / Olay Müdahale Politikası

> **Belge sahibi**: CISO, co-owned by DPO
> **Versiyon**: 1.0 — 2026-04-10
> **Sonraki gözden geçirme**: 2027-04-10 veya tatbikat sonucu
> **İlgili**: SOC 2 CC7.3/CC7.4/CC7.5 (ADR 0023), ISO 27001:2022 A.5.24–A.5.28, KVKK m.12/5, GDPR Art. 33 & 34, `docs/compliance/kvkk-framework.md` §12, `docs/security/runbooks/incident-response-playbook.md`

## 1. Policy vs Runbook Distinction

Bu politika **neyin** yapılması gerektiğini, **kimin** sorumlu olduğunu ve **hangi sürede** yapılması gerektiğini tanımlar. Teknik müdahale adımları `docs/security/runbooks/incident-response-playbook.md` içinde ayrıntılıdır. Bu belge auditor-facing governance; runbook operations-facing teknik.

## 2. Scope

- Personel SaaS üretim ortamı (Phase 3)
- Müşteri on-prem kurulumlarında, müşterinin destek talebi üzerine Personel destek ekibinin dahil olduğu olaylar
- Personel kurumsal altyapısı (kendi çalışanlarımızın hesapları ve laptopları — ikincil scope)

## 3. Policy Statement

Personel, güvenlik ve gizlilik olaylarını hızlı, şeffaf ve düzenleyici yükümlülüklere uygun şekilde müdahale eder. Her olay:

1. Saptanır ve sınıflandırılır
2. Sahiplenilir (Incident Commander)
3. Müdahale edilir (kontrol, yok etme, kurtarma)
4. Bildirilir (iç + dış gerekli muhataplar)
5. Belgelenir ve retrospektif edilir

## 4. Classification / Sınıflandırma ve SLA

| Sınıf | Tanım | Tespit→Sınıflama SLA | Tespit→Kontrol SLA | Tespit→Bildirim SLA |
|---|---|---|---|---|
| **Informational** | İlgi çekici gözlem, aksiyon gerekmez | 24 saat | — | — |
| **Low** | Sınırlı iç etki, veri etkilenmez | 8 saat | 24 saat | İç (management review) |
| **Medium** | Kısıtlı müşteri etkisi, kişisel veri etkilenmedi | 4 saat | 8 saat | İç + etkilenen müşteri 24 saat |
| **High** | Olası veri ihlali, servis kesintisi, ayrıcalıklı erişim kompromise şüphesi | 1 saat | 4 saat | **Kurul/SA 72 saat (zorunlu değerlendirme)** |
| **Critical** | Onaylı veri ihlali, audit chain bütünlük kaybı, sistem çapında kompromise | 30 dakika | 2 saat | **Kurul 72 saat + müşteri bildirim + olası m.34 bildirim** |

## 5. IR Team / Roller

| Rol | Sorumluluk | Birincil |
|---|---|---|
| Incident Commander (IC) | Müdahaleyi yürütür, iletişim koordinasyonu | On-call security engineer |
| Security Lead | Teknik analiz, forensic | CISO |
| DPO | KVKK / GDPR bildirim kararları | DPO |
| Legal Counsel | Hukuki risk analizi, dış iletişim | Legal |
| Communications Lead | İç + dış iletişim mesajları | Head of Customer Success |
| Executive Sponsor | Yüksek/Kritik olaylar için yetkilendirme | CTO (yedek CEO) |

On-call rotation: 7×24, haftalık rotasyon, 2 kişilik ekip (birincil + yedek).

## 6. Communication Plan / İletişim Planı

### 6.1 İç iletişim

- Slack `#incidents` (özel oda) — tüm olaylar otomatik bildirilir
- PagerDuty escalation chain: on-call → Security Lead → CISO → CEO
- Yönetim özet e-postası: High+ sınıflar için 4 saat içinde

### 6.2 Dış iletişim — müşteriler

Müşteri bildirim şablonları (TR + EN) `docs/security/runbooks/incident-response-playbook.md` ekindedir. İçeriği:

- Olayın özeti (teknik detay minimum)
- Müşteriye özel etki
- Şu an yapılan aksiyonlar
- Müşterinin yapması gereken
- İletişim kanalı ve sonraki güncelleme zamanı

Onay: Communications Lead + DPO + Legal Counsel. Critical olaylar CEO onayı gerektirir.

### 6.3 Dış iletişim — düzenleyici

#### KVKK m.12/5 — 72 saat Kurul bildirimi

Süreç, `docs/compliance/kvkk-framework.md` §12'de tanımlıdır. Bu politika onu genişletir:

- DPO, high/critical sınıflandırmasından itibaren **72 saat** içinde Kurul'a bildirmeye hazırlıklı olmalıdır
- Bildirim öncesi: Legal Counsel + DPO + CISO üçlü ön-kontrol
- Bildirim formatı: Kurul'un güncel şablonu (EK-1)
- Eksik bilgiyle geç kalmaktansa, geç kalmadan eksik bilgiyle bildirim yapılır (Kurul bunu kabul eder)

#### GDPR Art. 33 — 72 saat supervisory authority notification (Phase 3 EU)

- İhlalin farkına varılmasından itibaren 72 saat (Phase 3 EU market)
- İlgili supervisory authority (SA): Phase 3.4 region pinning'e göre belirlenir
- İçerik: Art. 33(3): nature, categories, approximate number, DPO contact, likely consequences, measures

#### GDPR Art. 34 — Data subject notification

- Sadece **high risk to rights and freedoms** durumunda zorunlu
- Karar: DPO + Legal Counsel; Executive Sponsor imzası
- İstisnalar: Art. 34(3) — şifreleme, sonradan azaltma, orantısız efor

### 6.4 Public communication

- Statüs sayfası (status.personel.tr) — availability olayları için otomatik
- Müşteri e-postası — high+ için
- Basın açıklaması — critical + medya sorgusu varsa (Legal + CEO onayı)

## 7. Lifecycle Phases (NIST SP 800-61r2 uyumlu)

1. **Preparation**: runbook güncelleme, araç hazırlık, tatbikat
2. **Detection & Analysis**: SIEM, Prometheus, Falco, Loki, employee report, customer report
3. **Containment**: runbook'taki sınıfa özel adımlar (kısa vadeli + uzun vadeli)
4. **Eradication**: kök neden ortadan kaldırma
5. **Recovery**: sistem geri getirme, monitoring extra
6. **Post-Incident Activity**: retrospective (§8), risk register güncellemesi, kontrol iyileştirme

## 8. Post-Incident Review / Olay Sonrası İnceleme

- High ve Critical olaylar için **5 iş günü içinde** retrospective toplantı zorunludur
- Katılımcılar: IR team + Executive Sponsor
- Çıktılar:
  - Kök neden analizi (5-why / fishbone)
  - Zaman çizelgesi (timeline)
  - İyi çalışan / kötü çalışan / öğrenilenler
  - Aksiyon öğeleri (sahip + son tarih)
  - Risk register güncellemesi (`docs/security/risk-register.md` §4)
- Retrospective özetleri management review'a gider (ADR 0024)
- Medium olaylar için kısa retrospective (Slack thread / PR) yeterlidir

## 9. Tabletop Exercises / Tatbikatlar

- **Çeyreklik** masa üstü tatbikat zorunludur
- Senaryolar (dönüşümlü):
  - Ransomware (çalışan laptop + yanal hareket)
  - Vault kompromise (R-DAT-003)
  - ClickHouse büyük sızıntı (R-DAT-001)
  - DPO-erişim insider threat (R-OPS-002)
  - KVKK 72-hour notification drill (yılda en az 1)
- Tatbikat tutanakları CO tarafından 3 yıl saklanır
- Yıllık en az 1 live drill (sadece tabletop değil)

## 10. Evidence Preservation / Delil Koruma

- Hash-chained audit log dokunulmaz (`docs/security/runbooks/admin-audit-immutability.md`)
- Olay delilleri WORM bucket'a yazılır (`docs/security/runbooks/worm-audit-recovery.md`)
- Forensic export `infra/scripts/forensic-export.sh` ile dual-control (DPO + CISO)
- Chain-of-custody kayıtları zorunludur (kim, ne zaman, ne amaçla)

## 11. Exceptions

Bu politikadan sapmalar (örn. 72-saatlik Kurul bildirim pencereesini aşma) yazılı olarak belgelenmeli ve Legal Counsel + DPO + CEO onayı ile yapılmalıdır. Kurul'a gerekçe sunulmalıdır.

## 12. Related Documents

- `docs/security/runbooks/incident-response-playbook.md` (teknik)
- `docs/compliance/kvkk-framework.md` §12 (Kurul bildirim süreci)
- `docs/security/threat-model.md`
- `docs/security/runbooks/worm-audit-recovery.md`
- `docs/security/runbooks/admin-audit-immutability.md`
- `docs/policies/business-continuity-disaster-recovery.md`
- `docs/security/risk-register.md`
- ADR 0014 (WORM audit sink)

## 13. Review Cycle

Yıllık gözden geçirme veya her Critical olay sonrası.

## 14. Approval

| Rol | Ad | İmza | Tarih |
|---|---|---|---|
| CEO | _______ | _______ | _______ |
| CTO | _______ | _______ | _______ |
| CISO | _______ | _______ | _______ |
| DPO | _______ | _______ | _______ |
| Legal Counsel | _______ | _______ | _______ |

---

## English Summary

This policy implements SOC 2 CC7.3/CC7.4/CC7.5, ISO 27001:2022 A.5.24–A.5.28, KVKK Article 12/5, and GDPR Articles 33–34. Incidents are classified Informational / Low / Medium / High / Critical, with SLA-bound detection, containment, and notification targets per class. The IR team comprises an Incident Commander, Security Lead, DPO, Legal Counsel, Communications Lead, and Executive Sponsor, with 7×24 dual on-call. Regulatory notifications are 72-hour for KVKK (Kurul) and GDPR Art. 33 (supervisory authority); Art. 34 data-subject notification is discretionary on high-risk assessment by DPO + Legal + Executive Sponsor. Technical runbook content is deliberately kept out of this policy document and lives in `docs/security/runbooks/incident-response-playbook.md`. Post-incident reviews are mandatory within 5 working days for High+ incidents, and quarterly tabletop exercises (with at least one annual live drill) are required.
```

---
