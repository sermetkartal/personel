# Change Management Policy / Değişiklik Yönetimi Politikası

> **Belge sahibi**: CTO, co-owned by CISO
> **Versiyon**: 1.0 — 2026-04-10
> **Sonraki gözden geçirme**: 2027-04-10
> **İlgili**: SOC 2 CC8.1 (ADR 0023), ISO 27001:2022 A.8.32, A.5.37, A.8.25 (Secure Development Life Cycle), ADR 0020/0021 (SaaS + K8s), `docs/policies/incident-response.md`

## 1. Amaç ve Kapsam (Purpose & Scope)

Personel platformunun üretim sistemlerinde yapılan tüm değişikliklerin; (a) yetkilendirilmiş, (b) test edilmiş, (c) geri alınabilir ve (d) izlenebilir olmasını sağlar.

**Kapsam**:
- Kaynak kod değişiklikleri (apps/agent, gateway, api, console, portal, ml-classifier, ocr-service, uba-detector, livrec-service)
- Altyapı değişiklikleri (infra/compose, infra/systemd, scripts, Phase 3 Helm charts)
- Yapılandırma değişiklikleri (Keycloak realm, Vault policies, policy definitions, migration scripts)
- Müşteri on-prem kurulumlarına yayımlanan release'ler
- Phase 3 SaaS üretim ortamı

**Kapsam dışı**: geliştirici yerel makinesindeki deney, staging-only feature flag rollout'ları, CI/CD araç güncellemeleri (ayrı bir runbook ile).

## 2. Policy Statement

Üretime giren hiçbir değişiklik Git workflow dışında (PR / merge / signed tag / Argo CD sync) yapılamaz. Her değişiklik sınıflandırılır ve sınıfına göre onay ve test gereksinimlerini karşılar. Tüm değişiklikler ve onayları 3 yıl süreyle saklanır.

## 3. Change Classification / Sınıflandırma

### 3.1 Standard Change (Standart)

- Önceden onaylanmış, tekrarlayan, düşük riskli değişiklikler (ör. KVKK metin güncellemesi, localization string, Grafana dashboard)
- **Onay**: 1 code reviewer (peer)
- **Test**: CI unit + lint
- **Örnek**: `apps/console/messages/tr.json` dil güncellemesi

### 3.2 Normal Change (Normal)

- Standard dışındaki, planlı tüm değişiklikler
- **Onay**: 2 reviewer (1 peer + 1 domain lead / tech lead); production deploy için ek CAB onayı (§4)
- **Test**: CI unit + integration + relevant e2e
- **Örnek**: Yeni API endpoint, migration, UBA feature ekleme

### 3.3 Emergency Change (Acil)

- Devam eden üretim olayını çözmek veya aktif güvenlik açığını kapatmak için
- **Onay**: anlık 1 reviewer (on-call), **post-hoc review 24 saat içinde CAB tarafından**
- **Test**: minimum smoke (staging) — istisna belgelenmelidir
- **Örnek**: CVE patch bump, rate limit düzeltmesi, deadlock hotfix

## 4. CAB — Change Advisory Board

Phase 3.0'dan itibaren bir Change Advisory Board kurulur. Üretim (on-prem release veya SaaS deploy) etkileyen tüm **Normal** değişiklikler CAB tarafından onaylanmalıdır.

**Üyeler**:

- Technical Lead (CTO delegate) — chair
- Security Lead — güvenlik etkisini değerlendirir
- DPO — KVKK/GDPR etkisini değerlendirir (gizlilik etkileyen değişikliklerde zorunlu)
- Platform Lead — operasyonel etkiyi değerlendirir
- Customer Success delegate (müşteri etkisi varsa)

**Toplantı**: haftalık 30 dakika (asenkron Slack approval da kabul edilir — senkron toplantı sadece high-risk / major release). CAB kararları Git PR'da imzalı yorum olarak kayıt altına alınır.

## 5. Procedure / Prosedür

### 5.1 Change ticket

Her değişikliğin ilgili bir change ticket'i olmalıdır (JIRA / GitHub Issue / Linear). PR açıklaması ticket ID'ye referans vermek zorundadır. CI linter bu referansı zorlar.

### 5.2 PR requirements

- Başlık: `[CLASS] brief summary` (CLASS ∈ {STD, NRM, EMG})
- Açıklama:
  - Değişikliğin amacı
  - Test kapsamı
  - Rollback prosedürü (zorunlu — nasıl geri alınacağı)
  - Veri/schema etkisi
  - Güvenlik/KVKK etkisi (varsa)
- Branch protection: main'e merge için zorunlu yeşil CI + gerekli reviewer sayısı (§3)
- Commit'ler signed (Sigstore / GPG)

### 5.3 Testing

| Class | CI unit | Integration | E2E | Manual QA | Staging soak |
|---|---|---|---|---|---|
| Standard | ✅ | — | — | — | — |
| Normal | ✅ | ✅ | ✅ (relevant suite) | risk bazında | 24h |
| Emergency | ✅ | best-effort | — | incident commander takdiri | — |

### 5.4 Deployment

- On-prem: signed release artifact; müşteri kurulumu infra/install.sh ile
- SaaS (Phase 3): Argo CD GitOps, production'a promote için tagged release + CAB approval + deploy window
- Deploy window: iş günü 10:00–16:00 TRT (emergency hariç)

### 5.5 Rollback

Her PR rollback prosedürünü belgelemelidir. Prosedür:

1. Revert commit (Git)
2. Argo CD previous revision promote
3. Schema migration için: down migration veya compatible schema pattern (forward-only değil)
4. 30 dakika içinde verification smoke test

### 5.6 Emergency change post-hoc review

- Incident commander 24 saat içinde change retrospective açar
- CAB değişikliği retroaktif olarak onaylar veya ek aksiyon talep eder
- Post-hoc review sonucu change ticket'a eklenir

## 6. Records Retention

- Git history: kalıcı
- Change tickets + CAB approvals: **3 yıl**
- Release artifact'leri ve imzaları: **3 yıl**
- Deploy audit log'ları: hash-chained audit (7 yıl)

## 7. Exceptions

İstisnalar sadece şunlar için verilir:
- Uyum nedeniyle hızlı patch (CVE CVSS ≥ 9.0)
- Regulator talep (KVKK Kurul kararı ile)

Her istisna CO + CTO imzası gerektirir.

## 8. Metrics / Ölçütler

- **Change failure rate** (CFR): başarısız / toplam deploy. Hedef < %15 (SOC 2 A1.x için).
- **Mean time to rollback** (MTTR-rollback): hedef < 30 dk.
- **CAB yanıt süresi** (Normal): hedef < 48 saat async approval.
- **Emergency change oranı**: toplamın < %10'u.

Metrikler management review'e sunulur (ADR 0024).

## 9. Integration with Git workflow

- `main` branch korumalı (force push yasak, linear history zorunlu)
- PR gerekli reviewer'lar + CI + CODEOWNERS
- Tag-based release (semver)
- `release/*` branch'leri sadece hotfix için
- Argo CD production namespace'leri sadece tagged commit'leri deploy eder

## 10. Related Documents

- ADR 0023 SOC 2 CC8.1
- ADR 0024 ISO 27001 A.8.32, A.5.37, A.8.25
- `docs/policies/incident-response.md`
- `docs/policies/access-review.md`
- `infra/runbooks/upgrade.md`
- `docs/architecture/phase-3-roadmap.md`

## 11. Review Cycle

Yıllık (2027-04-10) veya anlamlı süreç değişikliğinde.

## 12. Approval

| Rol | Ad | İmza | Tarih |
|---|---|---|---|
| CEO | _______ | _______ | _______ |
| CTO | _______ | _______ | _______ |
| CISO | _______ | _______ | _______ |
| DPO | _______ | _______ | _______ |

---

## English Summary

This policy implements SOC 2 CC8.1 and ISO 27001:2022 A.8.32 / A.5.37 / A.8.25. Changes are classified as standard (1 peer reviewer), normal (2 reviewers + CAB for production), or emergency (1 on-call reviewer with post-hoc CAB review within 24 hours). The CAB comprises the Technical Lead, Security Lead, DPO, Platform Lead, and (for customer-impacting changes) Customer Success. All PRs must reference a change ticket, document a rollback procedure, and pass the CI testing matrix appropriate to their classification. Git is the sole production change channel; Argo CD promotes only tagged commits in Phase 3 SaaS. Change records are retained for 3 years and change failure rate / rollback MTTR are tracked as SOC 2 metrics.
```

---
