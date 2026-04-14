# Yeniden Üretilebilir Derlemeler / Reproducible Builds

> **Amaç**: Aynı kaynaktan her build'de byte-identical binary üretebilmek.
> Supply chain tampering detection, forensic reproducibility ve SLSA Level 2+
> gereksinimi için.
> **Dil**: Türkçe birincil.
> **Ref**: Faz 12 #130, `docs/security/slsa-level2.md` (#131)
> **Status**: Rust + Go için çalışıyor (deterministic flags), Node + Docker
> için in-progress.

---

## 1. Giriş / Introduction

**Reproducible build** = iki farklı makine, iki farklı zaman, iki farklı CI
runner aynı commit'i alıp aynı build komutunu çalıştırsın ve üretilen binary
bayt seviyesinde özdeş olsun. Bu özdeşlik supply chain attack'ların tespitinde
kritik rol oynar: CI pipeline'ın kendisi compromise edilmiş olsa bile,
bağımsız bir build ile karşılaştırma tampering'i ortaya çıkarır.

SLSA Level 2 için şart, Level 3'ün ön koşullarından biri.

---

## 2. Ortak İlkeler / Common Principles

1. **Pin everything**: toolchain versiyonu, dependency hash, base image digest.
2. **Strip timestamps**: build timestamp → `SOURCE_DATE_EPOCH`.
3. **Strip paths**: absolute paths derlenmiş binary'de görünmemeli.
4. **Strip user info**: username, hostname, UID, GID bilgisi artifact'e sızmamalı.
5. **Strip random**: randomized symbol ordering, randomized section layout kapalı.
6. **Deterministic archives**: tar/zip mtime, uid, gid standart değerler.

---

## 3. Go Projeleri / Go Projects

### 3.1 Gerekli / Required

- Toolchain: `go 1.25` pinned in `go.mod` (not `go 1.x`).
- Build flag'leri:
  ```bash
  CGO_ENABLED=0 \
  GOFLAGS=-trimpath \
  go build \
    -trimpath \
    -buildvcs=false \
    -ldflags='-s -w -buildid=' \
    -o bin/api \
    ./cmd/api
  ```

**Flag'ler ne yapar?**

| Flag | Etki |
|---|---|
| `-trimpath` | Derlenmiş binary'den `/home/kartal/personel/...` gibi yerel path'leri kaldırır. |
| `-buildvcs=false` | Git VCS metadata'sı binary'ye gömülmez (commit SHA, timestamp). |
| `-ldflags='-s -w'` | Symbol table + debug info strip — tekrarlanabilir olmayan debug bilgisi çıkar. |
| `-ldflags='-buildid='` | Go linker'ın random build ID'si elimine edilir. |
| `CGO_ENABLED=0` | C toolchain farklarından kaynaklanan non-determinism elimine edilir. |

### 3.2 Dependency pinning

```bash
# Sürüm sabitleme
go mod tidy
go mod verify    # her zaman çalıştır
GOFLAGS=-mod=readonly go build ./...
```

Vendored deps tercih edilir (kararlı Phase 1.5):
```bash
go mod vendor
git add vendor/
# .gitattributes: vendor/** linguist-generated=true
```

### 3.3 Doğrulama

```bash
# İki ayrı klon + build → hash karşılaştırma
git clone https://github.com/sermetkartal/personel.git /tmp/p1
git clone https://github.com/sermetkartal/personel.git /tmp/p2

(cd /tmp/p1/apps/api && CGO_ENABLED=0 go build -trimpath -buildvcs=false \
  -ldflags='-s -w -buildid=' -o /tmp/p1.bin ./cmd/api)
(cd /tmp/p2/apps/api && CGO_ENABLED=0 go build -trimpath -buildvcs=false \
  -ldflags='-s -w -buildid=' -o /tmp/p2.bin ./cmd/api)

sha256sum /tmp/p1.bin /tmp/p2.bin
# Expected: aynı hash
```

---

## 4. Rust Agent / Rust Agent

### 4.1 Gerekli / Required

- Toolchain: `rust-toolchain.toml` ile pin (MSRV 1.88 zaten sabit).
- `Cargo.lock` commit edilmeli (library değil binary workspace için zorunlu).
- Build komutları:
  ```bash
  export SOURCE_DATE_EPOCH=$(git log -1 --format=%ct)
  export RUSTFLAGS='--remap-path-prefix=/=.'
  cargo build --release --locked --frozen
  ```

### 4.2 Cargo.toml profile

```toml
# apps/agent/Cargo.toml
[profile.release]
opt-level = 3
lto = "fat"
codegen-units = 1       # Paralel codegen kapalı — deterministik
strip = "symbols"
debug = false
incremental = false     # Incremental cache determinism bozar
panic = "abort"

[profile.release.package."*"]
opt-level = 3
```

**Neden codegen-units=1?** Rust varsayılan olarak 16 paralel codegen unit
kullanır; parallellik symbol ordering'i non-deterministic yapar. 1'e
düşünce build daha yavaş ama reproducible.

### 4.3 Windows-specific

MSVC toolchain için ek flags:
```
RUSTFLAGS=--remap-path-prefix=/=. -Clink-args=/Brepro
```

`/Brepro` MSVC linker'a reproducibility modu söyler.

### 4.4 Doğrulama

```powershell
$env:SOURCE_DATE_EPOCH = (git log -1 --format="%ct")
$env:RUSTFLAGS = "--remap-path-prefix=/=."
cd apps\agent
cargo clean
cargo build --release --locked
Copy-Item target\release\personel-agent.exe c:\tmp\build1.exe

cargo clean
cargo build --release --locked
Copy-Item target\release\personel-agent.exe c:\tmp\build2.exe

certutil -hashfile c:\tmp\build1.exe SHA256
certutil -hashfile c:\tmp\build2.exe SHA256
```

---

## 5. Node / Next.js Projeleri

### 5.1 Gerekli / Required

- Node sürümü: `.nvmrc` ile sabit (`20.x`).
- pnpm sürümü: `packageManager` field in `package.json`.
- `pnpm install --frozen-lockfile` — lockfile'dan saparsa fail.
- `NODE_ENV=production` build.

### 5.2 Next.js-specific

Next.js çıktısı tamamen reproducible **değil**:
- `.next/cache` → mtime farkları
- Runtime-specific build IDs (`BUILD_ID` file)
- Webpack chunk hash'leri non-deterministic olabilir

**Mitigation**:
```json
// next.config.js
module.exports = {
  generateBuildId: async () => {
    // Use git SHA instead of random
    return process.env.GIT_SHA || 'dev';
  },
  productionBrowserSourceMaps: false,
  // ... other options
}
```

Deterministic chunk naming için `webpack.optimization.moduleIds = 'deterministic'`
(Next 15 varsayılan).

### 5.3 Sınırlamalar / Limitations

Node'da tam reproducibility şu an tam oturmuş değil. `.next/` çıktısı
%95 reproducible; kalan %5 cache dosyaları. CI'da `.next/cache/` diff'i
accepted delta olarak işaretlenir.

---

## 6. Docker İmajları / Docker Images

### 6.1 Multi-stage + pinned digests

```dockerfile
# Pinned by digest, not tag
FROM golang:1.25-alpine@sha256:abc123... AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOFLAGS=-trimpath \
    go build -trimpath -buildvcs=false \
    -ldflags='-s -w -buildid=' \
    -o /out/api ./cmd/api

FROM gcr.io/distroless/static:nonroot@sha256:def456...
COPY --from=build /out/api /api
USER nonroot:nonroot
ENTRYPOINT ["/api"]
```

### 6.2 Reproducible BuildKit options

```bash
export SOURCE_DATE_EPOCH=$(git log -1 --format=%ct)
docker buildx build \
  --provenance=true \
  --sbom=true \
  --output type=oci,tar=false,name=personel/api:ci,rewrite-timestamp=true \
  .
```

`rewrite-timestamp=true` BuildKit'e `SOURCE_DATE_EPOCH`'u tüm layer
metadata'ya uygulamasını söyler.

### 6.3 Sınırlamalar

Docker Hub'da görünen image digest = manifest digest ≠ layer content digest.
Reproducibility `docker save` + tar diff ile doğrulanır, manifest digest
ile değil.

---

## 7. Doğrulama Script'i / Verification Script

`infra/scripts/verify-reproducible.sh` — tüm bileşenleri iki kez build eder
ve SHA-256 hash'lerini karşılaştırır. Kullanım:

```bash
infra/scripts/verify-reproducible.sh               # tüm bileşenler
infra/scripts/verify-reproducible.sh go-api        # sadece Go api
infra/scripts/verify-reproducible.sh rust-agent    # sadece Rust agent
```

---

## 8. CI entegrasyonu / CI Integration

`sbom.yml` (#125) workflow'una ek job:

```yaml
  verify-reproducible:
    name: Verify reproducibility
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: infra/scripts/verify-reproducible.sh go-api
      - run: infra/scripts/verify-reproducible.sh go-gateway
```

Haftalık schedule ile Rust agent da doğrulanır (Windows runner).

---

## 9. Bilinen Gapler / Known Gaps

| # | Gap | Severity | Plan |
|---|---|---|---|
| 1 | Next.js `.next/cache/` non-deterministic | Low | Accept delta; cache hariç tut. |
| 2 | Rust `cargo` build script side effects (env var leak) | Medium | `build.rs` review; `rerun-if-env-changed` kontrol. |
| 3 | Docker base image digest drift (alpine patch) | Low | Renovate bot ile pinned digest update. |
| 4 | Windows MSI build Authenticode signature non-deterministic | Medium | `/t` signtime strip; kabul edilen delta. |
| 5 | Proto stub generation (protoc sürümü) | High | `buf generate` + `buf.lock` (Phase 2 migration). |

---

## 10. AWAITING

- [ ] Renovate bot account (base image digest pinning automation)
- [ ] Windows ARM64 runner (cross-reproducibility test)
- [ ] Independent rebuilder (third party — Reproducible Builds project)

---

## 11. Referanslar / References

- https://reproducible-builds.org/
- https://slsa.dev/spec/v1.0/levels
- https://go.dev/ref/mod#build-commands
- Rust RFC 2524: Deterministic builds
- https://docs.docker.com/build/ci/github-actions/attestations/
