# KVKK ↔ GDPR Karşılaştırmalı Analiz / KVKK ↔ GDPR Gap Analysis

> Hukuki çerçeve: 6698 sayılı Kişisel Verilerin Korunması Kanunu (KVKK) ve Regulation (EU) 2016/679 (General Data Protection Regulation, GDPR).
> Status: PLANNING — version 0.1 (2026-04-10).
> Purpose: Faz 3 AB genişlemesi (ADR 0022) çerçevesinde KVKK ile GDPR arasındaki örtüşmeyi ve farkları noktasal olarak göstermek; ürün tarafındaki kod değişikliklerini minimize edecek ortak uygulama katmanını belirlemek.
> Audience: Personel DPO, hukuk müşaviri, ürün mühendisi, AB satış ekibi.

---

## Bölüm A — Türkçe Yönetici Özeti

### A.1. Genel Sonuç

KVKK ve GDPR, hukuki kaynak ve terminoloji bakımından farklı olmakla birlikte **pratik yükümlülükler bakımından yaklaşık %80 örtüşmektedir**. Personel platformu Faz 1'den itibaren KVKK-native tasarlandığı için, GDPR geçişinde **kod düzeyinde minimum değişiklik** gerekir; yükün büyük çoğunluğu **doküman, sözleşme ve organizasyonel süreç** düzeyindedir.

### A.2. Örtüşen Yükümlülükler (~%80)

- Kişisel veri tanımı ve özel nitelikli/kategoriler (KVKK m.3, m.6 ↔ GDPR Art. 4, Art. 9)
- Veri sorumlusu / veri işleyen ayrımı (KVKK m.3/1-ı, ğ ↔ GDPR Art. 4(7), (8))
- Hukuka uygun işleme sebepleri — meşru menfaat dahil (KVKK m.5 ↔ GDPR Art. 6)
- İlgili kişi hakları — erişim, düzeltme, silme, itiraz (KVKK m.11 ↔ GDPR Art. 15–22)
- Aydınlatma yükümlülüğü (KVKK m.10 ↔ GDPR Art. 13–14)
- Veri güvenliği tedbirleri (KVKK m.12 ↔ GDPR Art. 32)
- Veri ihlali bildirim yükümlülüğü — 72 saat kuralı (KVKK Kurul Kararı 2019/10 + m.12/5 ↔ GDPR Art. 33, 34)
- Silme/imha yükümlülüğü (KVKK Saklama ve İmha Yönetmeliği ↔ GDPR Art. 17)
- Saklama süresi ölçülülüğü (KVKK m.4 ↔ GDPR Art. 5(1)(e))
- Yurt dışına aktarım denetimi (KVKK m.9 ↔ GDPR Chapter V)

### A.3. Yalnız GDPR'da Olanlar

- **Art. 35 DPIA** — belirli işlemeler için zorunlu (sistematik izleme dahil → Personel için her SaaS müşterisinde zorunlu).
- **Art. 37 DPO atama** — belirli işlemeler için zorunlu (Personel-as-processor için bu eşik aşıldığı için DPO atanması zorunlu).
- **Art. 28 veri işleyen sözleşmesi** — asgari unsurları GDPR'da daha ayrıntılı. (KVKK m.12/2 de yazılı sözleşme ister ama içerik detayı daha sınırlıdır.)
- **Lead Supervisory Authority / One-Stop-Shop** — AB üye devletleri arası yetki.
- **Kurumsal Bağlayıcı Kurallar (BCR)** — KVKK'da eş değeri yok.
- **Standart Sözleşme Hükümleri (SCC)** — AB Komisyonu onaylı standart şablonlar.

### A.4. Yalnız KVKK'da Olanlar

- **VERBİS kaydı** — KVKK'nın sicil sistemi; GDPR'da eş değer sicil yok.
- **Kurul kararları ile somutlaşan Türkçe yerel gereksinimler** (aydınlatma Türkçe, açık rıza Türkçe, TCKN işleme izni vb.).
- **Türk İş Hukuku m.399 ve m.25 ile somut bağlantı** — işveren meşru menfaatinin hukuki altyapısı Türk iş kanununa göre şekillenir.

### A.5. Ürün Tarafında Gerekli Kod Değişiklikleri (Asgari)

| Alan | Mevcut KVKK durumu | GDPR için ek |
|---|---|---|
| DSR workflow | KVKK m.11 kapsamında çalışıyor | Portal metinlerinin EN dilinde GDPR Art. 15 referansları, SLA aynı (30 gün) |
| Aydınlatma metni | KVKK m.10 şablonu mevcut | EN GDPR Art. 13–14 şablonu eklenir |
| Veri saklama matrisi | Kategorilere göre definable | Değişiklik yok (ölçülülük her iki kanunda da aynı) |
| VERBİS export | Mevcut | Art. 30 export ek exporter olarak eklenir |
| Veri ihlali runbook | 72 saat kuralı KVKK için yazılmış | GDPR için aynı runbook kullanılır; bildirim adresi değişir |
| DPA / veri işleyen sözleşmesi | KVKK m.12/2 SaaS şablonu | GDPR Art. 28 şablonu eklenir; SCC'ler gerekirse ek edilir (Personel için bölge sabitlemesi ile SCC gerekmez) |
| DPO iletişim | Mevcut (TR) | EU iletişim kanalı eklenir |

**Sonuç**: Faz 3 kapsamında GDPR uyumu için **yeni kod yazmaya minimum ihtiyaç vardır**; odak doküman ve sözleşme üretimidir.

---

## Section B — English Point-by-Point Comparison

### B.1. Definitional mapping

| Concept | KVKK article | GDPR article | Notes |
|---|---|---|---|
| Personal data | m.3/1-d | Art. 4(1) | KVKK definition is narrower in literal reading but Kurul interprets it broadly; effectively equivalent. |
| Processing | m.3/1-e | Art. 4(2) | Equivalent. |
| Controller | m.3/1-ı (veri sorumlusu) | Art. 4(7) | Equivalent. |
| Processor | m.3/1-ğ (veri işleyen) | Art. 4(8) | Equivalent. KVKK requires a written contract (m.12/2); GDPR Art. 28 specifies minimum contract content more granularly. |
| Special categories | m.6 | Art. 9 | Very close; KVKK includes biometric and genetic data since 2016 amendment. Legal bases differ: KVKK m.6 allows explicit consent or specific legal grounds; GDPR Art. 9 has 10 enumerated grounds. |
| Data subject | m.3/1-ç (ilgili kişi) | Art. 4(1) | Equivalent. |
| Consent | m.3/1-a (açık rıza) | Art. 4(11), Art. 7 | Both require freely given, specific, informed. Both recognize the imbalance in employer-employee relationships. Both disfavor consent as the legal basis for workplace monitoring. |

### B.2. Lawful grounds

| Ground | KVKK | GDPR |
|---|---|---|
| Explicit consent | m.5/1 (genel), m.6/1 (özel nitelikli) | Art. 6(1)(a), Art. 9(2)(a) |
| Performance of contract | m.5/2-c | Art. 6(1)(b) |
| Legal obligation | m.5/2-ç | Art. 6(1)(c) |
| Vital interests | m.5/2-d | Art. 6(1)(d) |
| Public task | m.5/2-e | Art. 6(1)(e) |
| Legitimate interests | m.5/2-f | Art. 6(1)(f) |

**Personel's position under both**: legitimate interests (KVKK m.5/2-f / GDPR Art. 6(1)(f)) is the primary basis for workplace monitoring, with contract performance (KVKK m.5/2-c / GDPR Art. 6(1)(b)) as secondary for attendance/activity-log use cases. See `kvkk-framework.md` §2.1 for the three-stage balancing test, which is structurally identical to the GDPR legitimate interests assessment (LIA).

### B.3. Data subject rights

| Right | KVKK m.11 | GDPR | Response SLA |
|---|---|---|---|
| To know if data is processed | m.11/1-a | Art. 15 | KVKK: 30 days; GDPR: one month (28–31 days) |
| To request information | m.11/1-b | Art. 15 | Same |
| To know purpose and whether used accordingly | m.11/1-c | Art. 15 | Same |
| To know third-country transfers | m.11/1-ç | Art. 15(2) | Same |
| To request correction | m.11/1-d | Art. 16 | Same |
| To request deletion (right to be forgotten) | m.11/1-e | Art. 17 | Same |
| To notify third parties of corrections | m.11/1-f | Art. 19 | Same |
| To object to automated decisions causing adverse result | m.11/1-g | Art. 22 | Very close; GDPR Art. 22 is stronger (absolute right with exceptions). |
| Compensation for damage | m.11/1-h | Art. 82 | Equivalent. |
| Data portability | — | Art. 20 | **GDPR only**. Personel's DSR export endpoint already produces a portable JSON — forward-compatible. |
| Right to restriction of processing | — | Art. 18 | **GDPR only**. Implemented via legal hold mechanism repurposed. |

Personel's DSR workflow (`apps/api/internal/dsr/`) is forward-compatible with all GDPR rights. Phase 3 changes: add `portability_export` DSR type (already scaffolded), add `restriction` DSR type (reuse legal hold plumbing with different UX).

### B.4. Transparency obligations

| Item | KVKK m.10 | GDPR Art. 13–14 |
|---|---|---|
| Identity of controller | yes | yes |
| Purpose of processing | yes | yes |
| To whom and why data is transferred | yes | yes |
| Collection method and legal basis | yes | yes |
| Rights of the data subject | yes | yes |
| Retention period | inferred from Saklama/İmha Yönetmeliği | Art. 13(2)(a) explicit |
| Legitimate interests explanation (if LI is basis) | inferred | Art. 13(1)(d) explicit |
| DPO contact | — | Art. 13(1)(b) mandatory where DPO exists |
| Right to lodge complaint with SA | implied (KVKK Kurul başvurusu) | Art. 13(2)(d) explicit |
| Automated decision-making existence | — | Art. 13(2)(f) explicit |

**Product impact**: the `apps/portal/src/app/[locale]/aydinlatma/` page must have an EN variant for GDPR with the additional explicit fields. The KVKK TR version remains authoritative for Turkish customers.

### B.5. Security obligations

KVKK m.12 and GDPR Art. 32 both require "appropriate technical and organizational measures". Both reference:
- Pseudonymization and encryption
- Confidentiality, integrity, availability
- Resilience
- Testing and evaluation of effectiveness

Personel's existing ADR 0013 (crypto), ADR 0014 (WORM audit), ADR 0021 (mTLS everywhere), and threat model documents satisfy both. No code changes.

### B.6. Breach notification

| Dimension | KVKK | GDPR |
|---|---|---|
| Notify SA | m.12/5 + Kurul Kararı 2019/10: "en kısa sürede, en geç 72 saat içinde" | Art. 33: 72 hours |
| Notify data subject | m.12/5: yes | Art. 34: yes, when high risk |
| Documentation | required | Art. 33(5) explicit |

**Operationally identical** for Personel. One runbook serves both. The notification recipient (Kurul vs. supervisory authority) differs, but the triggers and clock are the same.

### B.7. Processor contract (Art. 28 / m.12/2)

Both require a written contract. GDPR Art. 28(3) lists eight minimum contents explicitly; KVKK m.12/2 and Kurul guidance list similar but not identically enumerated items.

| Contract element | GDPR Art. 28(3) | KVKK equivalent |
|---|---|---|
| (a) Process only on documented instructions | yes | inferred from m.12/2 + "adına işleme" definition |
| (b) Confidentiality obligation on personnel | yes | implied |
| (c) Art. 32 security measures | yes | m.12 reference |
| (d) Sub-processor authorization | yes | not explicit in m.12/2; Kurul guidance recommends |
| (e) Assistance with data subject rights | yes | inferred |
| (f) Assistance with Art. 32–36 obligations | yes | partial |
| (g) Deletion/return at end of processing | yes | implied |
| (h) Right to audit | yes | implied |

**Product impact**: Phase 3 authors a dual-template DPA that covers both frameworks in a single document (TR-language master + EN translation), minimizing contract sprawl.

### B.8. International transfers

| Dimension | KVKK m.9 | GDPR Chapter V |
|---|---|---|
| Adequacy list | Kurul by case (very few declared) | Commission decisions |
| Explicit consent | yes | yes (Art. 49(1)(a)) |
| Safeguards | Kurul authorization | SCC, BCR, codes of conduct, certification |
| Derogations | limited | Art. 49 |

**Personel's posture (ADR 0022)**: region pinning eliminates runtime cross-border transfer for tenant data. Only incidental paths (support escalation, observability control plane) exist, and those are explicitly filtered. No SCC required because no actual transfer occurs for the tenant data plane.

### B.9. Supervisory authority

| Dimension | KVKK | GDPR |
|---|---|---|
| SA | Kişisel Verileri Koruma Kurulu (KVKK Kurulu) | National DPAs (26+ in EU/EEA) coordinated by EDPB |
| Cross-border | not applicable (TR only) | One-Stop-Shop via LSA (Art. 56) |
| Fines | up to ~2M TL (per violation bracket) | up to €20M or 4% of global turnover |

**Personel action**: designate LSA during Phase 3.0 (see ADR 0022). Maintain Kurul relationship via DPO for TR side.

### B.10. VERBİS vs Art. 30 records

KVKK has VERBİS (Veri Sorumluları Sicil Bilgi Sistemi), a public registry of controllers. GDPR has no equivalent public registry; Art. 30 requires an internal record of processing activities.

Personel's existing VERBİS exporter emits a specific format. Phase 3 adds a second exporter producing the Art. 30 format from the same underlying data. The shared substrate is a `processing_activities` table with fields covering both formats.

### B.11. DPIA (Art. 35) vs KVKK DPIA

KVKK requires risk assessment as part of m.12 security measures but does not prescribe a formal DPIA process. Kurul guidance recommends a DPIA-like process for high-risk processing.

Personel has a DPIA template (`docs/compliance/dpia-sablonu.md`) that is already written in a GDPR-compatible structure. Phase 3 amendment adds explicit Art. 35 language and the specific high-risk triggers (systematic monitoring of employees on a large scale).

### B.12. DPO (Art. 37) mandatory appointment

GDPR Art. 37 requires DPO appointment when:
- Processing is carried out by a public authority (not Personel);
- Core activities require regular and systematic monitoring of data subjects on a large scale (**this is Personel in SaaS mode** — the tenants' employees are monitored by Personel's processing);
- Core activities consist of processing on a large scale of special categories (occasional but not core for Personel).

KVKK does not have a mandatory DPO concept at the law level; Kurul strongly recommends a DPO-equivalent irtibat kişisi.

**Product impact**: Personel (as Art. 28 processor in SaaS mode) must appoint a DPO for GDPR Art. 37. This DPO can be the same person who serves as the KVKK irtibat kişisi. One appointment serves both.

---

## Section C — Implementation checklist (Phase 3.3 compliance workstream)

| Item | Status | Owner |
|---|---|---|
| Dual-template DPA (KVKK m.12/2 + GDPR Art. 28) | TODO | CO + external counsel |
| Art. 30 records exporter | TODO | Dev-B |
| EN aydınlatma metni (GDPR Art. 13–14) | TODO | CO + content team |
| DSR portability export type | TODO | Dev-B |
| DSR restriction type | TODO | Dev-B |
| DPIA template Annex B (GDPR Art. 35) | TODO | CO |
| DPO appointment (contactable in EU) | TODO | Executive |
| LSA designation decision | TODO | CO + external counsel |
| Sub-processor registry page | TODO | CO + Dev-E |
| Breach notification runbook — GDPR addendum | TODO | CO + Security |

All items are Phase 3.3 deliverables. None require substantial engineering; most are documentation with light product-surface changes.

---

## Cross-references

- `docs/compliance/kvkk-framework.md` — primary KVKK framework
- `docs/adr/0022-gdpr-expansion.md` — ADR level decision
- `docs/adr/0020-saas-multi-tenant-architecture.md` — region pinning as GDPR Art. 44 solution
- `docs/architecture/phase-3-roadmap.md` — Phase 3.3 compliance workstream
