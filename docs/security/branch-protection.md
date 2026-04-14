# Branch Protection — Personel

> **Amaç**: `main` branch'in nasıl korunacağını, hangi status check'lerin
> zorunlu olacağını, kimin bypass hakkı olduğunu deklaratif olarak tanımla.
> **Dil**: Türkçe birincil, `gh` CLI komutları İngilizce.
> **Ref**: Faz 12 #129

---

## 1. Koruma Matrisi / Protection Matrix

| Ayar / Setting | main |
|---|---|
| Require pull request before merge | ✅ |
| Required approving reviews | **2** |
| Dismiss stale reviews on push | ✅ |
| Require review from Code Owners | ✅ |
| Require last push to be approved | ✅ |
| Require status checks to pass | ✅ |
| Require branches to be up to date | ✅ |
| Require conversation resolution | ✅ |
| Require signed commits | ✅ |
| Require linear history | ✅ |
| Require deployments to succeed | ❌ (deferred — no prod env yet) |
| Lock branch (read-only) | ❌ |
| Do not allow bypassing settings | ✅ (enforce on admins) |
| Restrict who can push | ✅ (whitelist: release-engineers team) |
| Allow force pushes | ❌ |
| Allow deletions | ❌ |

---

## 2. Zorunlu Status Checks / Required Status Checks

Bu check'ler PR merge edilemeden önce geçmeli:

| Check Name | Source Workflow | Owner |
|---|---|---|
| `ci / build-go` | `ci.yml` | backend |
| `ci / build-rust` | `ci.yml` | agent |
| `ci / build-node` | `ci.yml` | frontend |
| `ci / test-go` | `ci.yml` | backend |
| `ci / test-rust` | `ci.yml` | agent |
| `Trivy — Vulnerability & License Scan / trivy-fs` | `trivy.yml` (#126) | security |
| `Trivy — Vulnerability & License Scan / trivy-config` | `trivy.yml` | security |
| `Trivy — Vulnerability & License Scan / trivy-license` | `trivy.yml` | security |
| `CodeQL / Analyze (go)` | `codeql.yml` (#127) | security |
| `CodeQL / Analyze (javascript-typescript)` | `codeql.yml` | security |
| `CodeQL / Analyze (python)` | `codeql.yml` | security |
| `Gitleaks / Gitleaks scan` | `gitleaks.yml` (#128) | security |
| `SBOM Generation / go-sbom` | `sbom.yml` (#125) | security |

> Not: Workflow isimleri GitHub'da UI üzerinden görünen isimdir.
> Sadece ismi ve job key'i eşleşirse check required olarak kabul edilir.

---

## 3. Bypass Yetkileri / Bypass Actors

| Durum | Kim Bypass Edebilir | Nasıl |
|---|---|---|
| Normal PR | Hiç kimse | — |
| Critical security hotfix | `security-leads` team (min 2 members) | Temporary exception via GitHub UI with audit log |
| Release engineering | `release-engineers` team | Only for tag creation on release branches |
| Migration rollback | `dba` team | Only for `infra/compose/**` schema hotfix |

**İlke**: Bypass her zaman auditable, her bypass'e gerekçe logu zorunludur.

---

## 4. Uygulama / Application

Branch protection GitHub API ile deklaratif olarak uygulanır.

### 4.1 İlk kurulum / First-time setup

```bash
# Requires: gh CLI + admin token on the repo
cd /personel
gh api -X PUT \
  repos/sermetkartal/personel/branches/main/protection \
  --input docs/security/branch-protection.json
```

### 4.2 Check — mevcut durum

```bash
gh api repos/sermetkartal/personel/branches/main/protection | jq
```

### 4.3 Güncelleme / Update

1. `docs/security/branch-protection.json` dosyasını düzenle
2. Yukarıdaki PUT komutunu tekrar çalıştır
3. Commit message + PR ile değişiklik kayıt altına alınır

---

## 5. CODEOWNERS

`.github/CODEOWNERS` dosyası review zorunluluğunu yönlendirir:

```
# Global fallback
*                              @sermetkartal/core

# Agent
apps/agent/**                  @sermetkartal/rust-team

# Backend services
apps/api/**                    @sermetkartal/backend-team
apps/gateway/**                @sermetkartal/backend-team
apps/enricher/**               @sermetkartal/backend-team

# Frontend
apps/console/**                @sermetkartal/frontend-team
apps/portal/**                 @sermetkartal/frontend-team

# Infra + security-sensitive
infra/**                       @sermetkartal/devops-team
docs/security/**               @sermetkartal/security-team
docs/compliance/**             @sermetkartal/compliance-team
docs/adr/**                    @sermetkartal/architects @sermetkartal/security-team
.github/workflows/**           @sermetkartal/devops-team @sermetkartal/security-team
```

> **AWAITING**: Bu team'lerin GitHub org'da oluşturulması + doğru üyelerin
> atanması. Şu an `sermetkartal/personel` solo repo olduğu için
> CODEOWNERS placeholder.

---

## 6. Signed Commits

Tüm commit'ler GPG veya SSH signing key ile imzalanmalı.

```bash
# GPG setup
gpg --full-generate-key
git config --global user.signingkey <KEY_ID>
git config --global commit.gpgsign true
git config --global tag.gpgsign true

# SSH signing (alternative)
git config --global gpg.format ssh
git config --global user.signingkey ~/.ssh/id_ed25519.pub
git config --global commit.gpgsign true
```

GitHub `Settings → SSH and GPG keys → New signing key` ile public key
eklenir. PR page'de "Verified" badge görünmeli.

---

## 7. Doğrulama / Verification

```bash
# Protection aktif mi?
gh api repos/sermetkartal/personel/branches/main/protection \
  --jq '.required_status_checks.contexts'

# Signed commit zorunluluğu aktif mi?
gh api repos/sermetkartal/personel/branches/main/protection/required_signatures

# Son 100 PR'da kaç tanesi imzalı?
gh pr list --state merged --limit 100 --json number,mergedAt,author \
  | jq '.'
```

---

## 8. AWAITING

- [ ] GitHub team oluşturma + üye atama (security-leads, release-engineers, dba, rust-team, backend-team, frontend-team, devops-team, security-team, compliance-team, architects)
- [ ] CODEOWNERS dosyası push (workflow scope değil, normal scope — bu yapılabilir)
- [ ] Branch protection JSON payload repo admin token ile uygulama (workflow değil, repo-admin scope)
- [ ] GPG/SSH signing key her maintainer için GitHub'a yüklenmesi
