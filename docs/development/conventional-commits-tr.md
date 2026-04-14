# Personel — Conventional Commits Rehberi (TR)

Bu doküman, Personel repo'sunda kullanılan commit mesaj formatını tanımlar.
Tutarlı commit mesajları:
- Otomatik changelog üretimi (`infra/scripts/generate-changelog.sh`)
- Release notes automation (`docs/releases/v*.md`)
- Semantic versioning kararları
- Code review kolaylığı
için kritiktir.

**Zorunlu okuma**: Her commit açan geliştirici + AI agent.

---

## Temel Format

```
<tip>(<scope>): <kısa özet, 50 karakterden az, küçük harfle başla>

<opsiyonel daha uzun açıklama — 72 karakter wrap, neden-odaklı>

<opsiyonel footer — referans, breaking change>
```

### Örnek — basit

```
feat(api): add POST /v1/tickets ticket integration scaffold
```

### Örnek — açıklamalı

```
fix(agent): prevent DPAPI unseal on dev-mode fallback

The agent service.rs was calling DPAPI unseal unconditionally even
when running in --dev mode, causing crashes on Linux dev containers
where DPAPI doesn't exist. Gate the call on cfg.dev_mode.

Closes: #42
```

### Örnek — breaking change

```
feat(api)!: rename /v1/dlp-state to /v1/system/module-state

BREAKING CHANGE: Console + Portal clients must update the endpoint
URL. The old path returns 410 Gone.
```

---

## Commit Tipleri

| Tip | Kullanım | Changelog Kategori | Semver Etkisi |
|---|---|---|---|
| `feat` | Yeni özellik | Added | MINOR |
| `fix` | Bug fix | Fixed | PATCH |
| `docs` | Sadece doküman | Documentation | - |
| `security` | Güvenlik patch | Security | PATCH veya MINOR |
| `refactor` | Davranış değişmez refactor | Changed | - |
| `perf` | Performans iyileştirme | Performance | PATCH |
| `test` | Test ekleme/düzeltme | - | - |
| `build` | Build system değişikliği | - | - |
| `ci` | CI/CD değişikliği | - | - |
| `chore` | Diğer (deps update, rename, vb) | - | - |

### Breaking change

Her commit tipinde `!` ekleyerek veya footer'da `BREAKING CHANGE:` yazarak
breaking change işaretlenir. Bu **MAJOR** version bump'ı tetikler.

---

## Scope'lar

Kullanılabilecek scope'lar Personel'in alt bileşenleriyle eşleşir:

- **agent** — Rust Windows/macOS/Linux agent
- **gateway** — Go ingest gateway
- **enricher** — Go NATS consumer
- **api** — Go admin API
- **console** — Next.js admin UI
- **portal** — Next.js employee portal
- **infra** — Docker Compose, systemd, scripts
- **docs** — Markdown dokümanlar
- **proto** — gRPC proto dosyaları
- **compliance** — KVKK ile ilgili
- **security** — güvenlik ile ilgili
- **roadmap** — roadmap madde işaretleme (commit 5+ madde için)

Birden fazla scope varsa `,` ile ayır: `feat(api,console): ...`

---

## Özel Footer'lar

### Co-Authored-By

AI agent veya başka geliştirici ile birlikte yazılmış commit'ler için:

```
Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
```

### References

GitHub issue veya ADR referansı:

```
Closes: #123
Refs: ADR-0013
```

### Roadmap items

Roadmap madde commit'leri için (her 5 maddede):

```
feat(roadmap): items 177-186 — Faz 17 customer success
```

---

## Sık Yapılan Hatalar

### ❌ YANLIŞ

```
Update stuff
```
**Problem**: Tip yok, scope yok, özet anlamsız.

```
feat: added a new feature to the api
```
**Problem**: "added" → "add" (imperative mood). "the" gereksiz.

```
fix(api): BUGFIX
```
**Problem**: Ne bug'ı, nerede? Belirsiz.

### ✅ DOĞRU

```
fix(api): return 404 instead of 500 when dsr not found
```

```
feat(console): add 12-month evidence coverage heatmap
```

```
security(agent): validate policy signature before applying updates
```

```
docs(kvkk): update DPIA template with 2026 Kurul decision references
```

---

## Changelog Üretimi

Commit mesajları düzgün yazılırsa:

```bash
./infra/scripts/generate-changelog.sh --version 1.0.0 --release 1.0.0
```

Otomatik olarak:
1. Son tag'den HEAD'e kadar tüm commit'leri parse eder
2. Kategorize eder (Added / Fixed / Security / ...)
3. `CHANGELOG.md`'nin başına yeni section ekler
4. `docs/releases/v1.0.0.md` dosyası oluşturur

---

## Pull Request Başlıkları

PR başlıkları da aynı formatı takip etmeli. GitHub "Squash merge" kullandığında
PR başlığı commit mesajı olur.

### İyi PR başlığı

```
feat(console): add 12-month evidence coverage heatmap
```

### Kötü PR başlığı

```
Updates and fixes
```

---

## Commit Kontrolü (CI)

`commitlint` ile CI'da otomatik doğrulama (TODO — Faz 16):

```yaml
# .github/workflows/commitlint.yml
- uses: wagoid/commitlint-github-action@v5
  with:
    configFile: .commitlintrc.yml
```

## Sık Sorular

**Q**: Çok küçük bir değişiklik için bile bu formatı kullanmam gerekir mi?
**A**: Evet. Typo fix'i bile `docs(readme): fix typo in install section` şeklinde.

**Q**: AI agent (Claude Code) commit atarken bu kurallara uyuyor mu?
**A**: Evet — CLAUDE.md §0 bu kuralı referans alır. Agent her commit'te uyumludur.

**Q**: Mesajımı Türkçe yazabilir miyim?
**A**: Hayır, İngilizce yazın. Changelog uluslararası okunabilirlik için EN. İstisna: `docs` commit'lerinde Türkçe içerik olabilir.

**Q**: Bir commit'te 2 tip değişiklik var (feat + fix), ne yapayım?
**A**: **Böl**. İki ayrı commit at. Atomic commit prensibi.

---

## Referanslar

- https://www.conventionalcommits.org/
- https://keepachangelog.com/
- https://semver.org/

---

*Güncelleme: yeni commit tipleri veya scope'lar eklenirse bu doküman güncellenir.*
