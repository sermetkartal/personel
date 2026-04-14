# Personel — Sürüm Politikası (Semantic Versioning)

Faz 16 #172 — Personel'in sürüm numaralandırma ve release yayınlama kuralları.

## Sürüm formatı

Tüm yayınlar [Semantic Versioning 2.0.0](https://semver.org/lang/tr/) uyar:

```
vMAJOR.MINOR.PATCH[-ÖNSÜRÜM]
```

Örnekler:
- `v1.0.0` — ilk kararlı sürüm
- `v1.4.2` — patch güncellemesi
- `v2.0.0-rc.1` — 2.0 için release candidate
- `v2.0.0-beta.3` — beta
- `v1.5.0-pilot.1` — pilot müşteri için özel dal

## Ne zaman bumplamalı?

| Bump | Gerekçe | Örnekler |
|---|---|---|
| **MAJOR** | Geriye uyumsuz API/veri değişimi | `/v1/*` → `/v2/*`, migration 00XX'in downgrade yolu yoksa, gRPC proto alanında tip değişimi, config.yaml schema break |
| **MINOR** | Yeni özellik, geriye uyumlu | Yeni collector, yeni endpoint, yeni RBAC rolü, yeni feature flag, opt-in davranış |
| **PATCH** | Hata düzeltme, güvenlik yaması | Bug fix, CVE patch, performans iyileştirme, dependency update |

## Her katman için SemVer sınırları

### API sözleşmesi (`apps/api/api/openapi.yaml`)

- MAJOR: mevcut alan silinir, tip değişir, zorunlu parametre eklenir
- MINOR: yeni endpoint, yeni opsiyonel alan, yeni enum değer
- PATCH: yalnızca iç davranış, sözleşme değişmez

### gRPC (`proto/personel/v1/*.proto`)

- Alan numarası **asla** yeniden kullanılmaz (proto3 reserved)
- Alan tipi değişimi = MAJOR
- Yeni alan ekleme = MINOR
- `deprecated=true` eklemek PATCH seviyesinde, MINOR'da kaldırılabilir hale gelir, MAJOR'da silinir

### Postgres migration

- Migration numarası yalnızca ileri gidebilir
- Her `up.sql` için `down.sql` zorunlu
- `down.sql` veri kaybına yol açıyorsa: MAJOR
- Geriye uyumlu migration: MINOR veya PATCH

### Windows agent

- Config formatı (`config.toml`) MAJOR bumbpunda break edilebilir
- `enroll.exe --token` format değişimi = MAJOR (yeni token eski agent ile çalışmaz)
- Yeni collector = MINOR
- Collector bug fix = PATCH

### Konsol/Portal (Next.js)

- Kullanıcıya görünen UI akışı MAJOR'da değişebilir (örn. onay akışı sayı farkı)
- MINOR: yeni sayfa, yeni widget, yeni i18n stringi
- PATCH: stil, copy düzeltmesi, a11y fix

## Conventional Commits → otomatik changelog

`.gitmessage` şablonu + `git-cliff` config (`cliff.toml`) conventional commit
prefixlerini release notes'a eşler:

| Prefix | Bölüm | SemVer etkisi |
|---|---|---|
| `feat(…):` | 🚀 Features | MINOR |
| `fix(…):` | 🐛 Bug Fixes | PATCH |
| `perf(…):` | ⚡ Performance | PATCH |
| `refactor(…):` | 🧹 Refactor | (yok) |
| `docs(…):` | 📝 Documentation | (yok) |
| `test(…):` | 🧪 Tests | (yok) |
| `ci(…):` | 🏗️ CI/CD | (yok) |
| `chore(…):` | 🔧 Chores | (yok) |
| `BREAKING CHANGE:` footer | 💥 Breaking | MAJOR |
| `feat(…)!:` | 💥 Breaking | MAJOR |

**Örnek commit:**

```
feat(api): yeni feature flag yönetim endpoint'i ekle

/v1/system/feature-flags altına GET ve PUT eklendi. DPO/Admin erişimli,
her flip audit_log'a yazılıyor, bilinmeyen flag false döner.

Refs: Faz 16 #173
```

## KVKK ve hukuki bildirimler

Aşağıdaki değişiklikler MAJOR olmak zorundadır, çünkü müşteri DPO incelemesi
ve yeniden aydınlatma gerektirir:

- Yeni kişisel veri kategorisi toplama (yeni collector, yeni event type)
- DLP varsayılanını değiştirme (ADR 0013 hariç)
- Canlı izleme default kapsamını genişletme
- Audit log retention ya da WORM mode düşürme
- Saklama matrisinde süre kısaltma (`docs/architecture/data-retention-matrix.md`)

Bu değişiklikler için release notes'ta **"⚠️ KVKK NOTIFICATION REQUIRED"**
başlığı zorunludur ve müşteri DPA amendment yapılmadan yayın gitmez.

## Pilot/beta etiketleri

Pilot müşteriye özel, genel dağıtılmayacak paketler için önsürüm kullan:

```
v1.5.0-pilot.1
v1.5.0-pilot.2
```

Bu paketler `prerelease=true` ile yayınlanır ve public `latest` tag'ini
güncellemez. Pilot bitince semver kurallarına göre bir kararlı sürüme
(örn. `v1.5.0`) geçilir.

## Release süreci (operator)

1. `main` üzerinde son commit yeşilse devam et (`build.yml` + `image-verify.yml` yeşil)
2. `CHANGELOG.md`'yi manuel gözden geçir (git-cliff otomatik üretir, ama KVKK uyarısı eklemek için)
3. `git tag -s v1.5.0 -m "Personel 1.5.0"` — `-s` GPG imza zorunlu
4. `git push origin v1.5.0`
5. `release.yml` workflow'u otomatik çalışır:
   - changelog üretir
   - MSI + SBOM + kaynak arşivi indirir
   - GitHub release oluşturur
   - Slack + Discord webhook gönderir
6. Yayın sonrası: `docs/operations/canary-release.md` prosedürünü başlat

## Rollback politikası

- Bir yayın çekildiğinde (`gh release delete v1.5.0`), aynı sürüm **asla**
  yeniden kullanılmaz — bir sonraki patch (`v1.5.1`) çıkarılır
- Müşteri tarafında çalışan versiyonlar için `infra/scripts/rollback.sh`
  kullanılır (Faz 16 #176)
- ClickHouse/Postgres şema ilerlemesi rollback'te **manuel karar** gerektirir:
  yalnızca `--include-schema` flag'i ile migration down koşulur, varsayılan
  kapalıdır

## Tarihçe

| Sürüm | Tarih | Not |
|---|---|---|
| (beklemede) | - | İlk kararlı 1.0.0 Phase 1 exit criteria ile çıkacak |
