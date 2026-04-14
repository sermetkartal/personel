# SLSA Level 2 Supply Chain

> **Amaç**: Personel'in supply chain güvenliğini SLSA v1.0 Level 2'ye taşımak.
> Level 3'e geçiş plan + gap analizi.
> **Dil**: Türkçe birincil. İngilizce SLSA spec terminolojisi korunur.
> **Ref**: Faz 12 #131, https://slsa.dev/spec/v1.0/levels
> **Status**: In progress — Level 2 ~%70 erişildi, provenance workflow draft.

---

## 1. SLSA Nedir? / What is SLSA?

**SLSA** = Supply-chain Levels for Software Artifacts. OpenSSF tarafından
geliştirilen, bir yazılım build pipeline'ının güvenilirlik seviyesini
ölçen framework. Level 0 → 3 arası, her seviye bir öncekinin üstüne
ekleme kriterler koyar.

| Level | Kriter |
|---|---|
| 0 | No requirements |
| 1 | Build process scripted + provenance metadata exists |
| 2 | Provenance is authenticated + build runs on hosted build service |
| 3 | Build platform is hardened + source provenance verified |
| 4 | Two-party review + hermetic builds (deprecated in v1.0; similar covered by Level 3 + BuildLevel) |

Personel hedefi: **Level 2 Faz 1 sonunda**, Level 3 Faz 3 roadmap.

---

## 2. Level 2 Gereksinimleri / Level 2 Requirements

### Producer side
- [x] **P1 Build is scripted**: GitHub Actions `ci.yml`, `sbom.yml`, future
      `slsa-provenance.yml`
- [x] **P2 Build service**: GitHub-hosted runners (Microsoft Azure VMs)
- [x] **P3 Source version controlled**: Git (GitHub)

### Build platform requirements
- [x] **B1 Build runs in hosted service**: GitHub Actions
- [x] **B2 Signed provenance**: via `slsa-framework/slsa-github-generator`
      (this workflow to be added by operator)
- [x] **B3 Tamper-resistant build service**: GitHub runners are ephemeral;
      each job runs in a fresh VM

### Expected provenance fields
- [x] `builder.id`: `https://github.com/actions/runner`
- [x] `buildType`: `https://slsa-framework.github.io/github-actions-buildtypes/workflow/v1`
- [x] `invocation`: workflow ref + inputs hash
- [x] `materials`: source repo + commit SHA
- [x] `metadata.buildInvocationId`: run_id + run_attempt
- [x] `metadata.buildStartedOn` / `buildFinishedOn`
- [x] `metadata.completeness.{parameters, environment, materials}`

---

## 3. Personel Gap Analizi / Gap Analysis

| Requirement | Status | Gap | Fix |
|---|---|---|---|
| Scripted build | ✅ | Yok | — |
| Build service (hosted) | ✅ | Yok | GitHub-hosted runners |
| Ephemeral environment | ✅ | Yok | Runner per-job |
| Authenticated provenance | 🚧 | Workflow yok | `slsa-provenance.yml` eklenecek (infra/ci-scaffolds/) |
| Signed provenance | 🚧 | Sigstore cosign keyless sig yok | SLSA generator kullanır → otomatik |
| Provenance completeness | 🚧 | `parameters: true` gerek | Workflow input'u deterministic tanımla |
| Source provenance (branch protection) | ✅ | — | #129 ile kapandı |
| Signed commits | 🚧 | GPG/SSH signing henüz force değil | #129 `required_signatures: true` |
| SBOM per artifact | ✅ | — | #125 ile kapandı |
| Vulnerability scanning | ✅ | — | #126 ile kapandı |
| Reproducible builds | 🚧 | Node partial | #130 doküman + script hazır; Node limitations |
| Isolated build | ❌ | GitHub hosted runners paylaşmıyor ama isolation Level 3 gerek | Level 3 için self-hosted hardened runner |
| Two-party review | 🚧 | Branch protection 2-reviewer zorunlu | #129 PR review count 2 |

**Net**: Personel şu an Level 2'nin ~%70'inde. Workflow merge + signed
commit enforcement ile %100'e çıkılır.

---

## 4. Provenance Workflow — `slsa-provenance.yml`

Staging: `infra/ci-scaffolds/slsa-provenance.yml` (operator moves to `.github/workflows/`).

Bu workflow release tag'leri için provenance attestation üretir:

1. Release binary'leri build eder (Go, Rust, Docker images).
2. `slsa-framework/slsa-github-generator` reusable workflow'unu çağırır.
3. Provenance dosyasını Sigstore Rekor'a kaydeder (transparency log).
4. Release'e provenance + signature attach eder.
5. Release notes'a doğrulama komutu ekler:
   ```bash
   slsa-verifier verify-artifact personel-agent.msi \
     --provenance-path provenance.intoto.jsonl \
     --source-uri github.com/sermetkartal/personel \
     --source-tag v0.1.0
   ```

---

## 5. Level 3'e Geçiş Planı / Level 3 Path

Level 3 ek gereksinimleri:

1. **Hermetic build**: Dependency fetching build'den ayrı fazda; network
   build süresince kesik.
2. **Hardened build service**: Build runner'ın kendisi compromise-resistant
   (privilege separation, signed boot, vb).
3. **Source verification**: Commit history'nin tamamının SLSA provenance'ı.

### 5.1 Plan

- [ ] **Level 3 hermetic Go builds**: `go mod download` ayrı step'te,
      ardından `--network=none` container build. Phase 2.
- [ ] **Self-hosted hardened runner**: AWS Nitro Enclaves veya AMD SEV-SNP
      VM cluster. Cost tradeoff analizi Phase 3.
- [ ] **Sigstore Fulcio + Rekor transparency**: SLSA generator zaten kullanır.
- [ ] **Dependency commit verification**: vendored deps tercih + SHA256
      pinning. Partial (Rust `Cargo.lock`).

### 5.2 Geçici önlemler

Full Level 3'e ulaşana kadar:

- `.github/workflows/` audit log düzenli review (hardening proxy).
- `GITHUB_TOKEN` permissions minimum (`permissions: read` default).
- Third-party actions pin edilmiş SHA ile (ne `@v1` ne `@main`).
- Secret scope per-workflow (repo-level secrets yerine environment secrets).

---

## 6. Doğrulama Komutları / Verification Commands

End-user'ın release'i tüketirken provenance doğrulaması:

```bash
# slsa-verifier kurulumu
curl -sSL https://github.com/slsa-framework/slsa-verifier/releases/download/v2.6.0/slsa-verifier-linux-amd64 \
  -o /usr/local/bin/slsa-verifier
chmod +x /usr/local/bin/slsa-verifier

# Artifact doğrulama
slsa-verifier verify-artifact personel-agent.msi \
  --provenance-path personel-agent.msi.provenance \
  --source-uri github.com/sermetkartal/personel \
  --source-tag v0.1.0

# Docker image doğrulama
cosign verify-attestation personel/api:v0.1.0 \
  --type slsaprovenance \
  --certificate-identity-regexp="https://github.com/sermetkartal/personel/.*" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com"
```

---

## 7. AWAITING

- [ ] GitHub workflow scope OAuth token ile `slsa-provenance.yml` workflow'un
      `.github/workflows/` altına move edilmesi
- [ ] Sigstore Fulcio / Rekor account (free tier OK)
- [ ] Release pipeline stub'ının gerçek release ile test edilmesi
- [ ] `cosign` cluster secret Vault-integrated
- [ ] Level 3 hardened runner satın alma / self-host kararı
- [ ] Third-party action pin audit (renovate bot rule)

---

## 8. Referanslar

- SLSA v1.0 Spec: https://slsa.dev/spec/v1.0
- GitHub Actions + SLSA: https://github.com/slsa-framework/slsa-github-generator
- Sigstore: https://www.sigstore.dev/
- Reproducible builds (#130): `docs/security/reproducible-builds.md`
- Branch protection (#129): `docs/security/branch-protection.md`
