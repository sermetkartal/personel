# Contributing to Personel

> This repository is a **proprietary commercial product**. External
> contributions are not accepted at this time. The guide below applies to
> internal team members and contracted engineers only.

---

## 1. Branch Strategy

- `main` is the single long-lived branch. Every merged change lands here.
- Feature branches: `feat/<short-slug>`, max 3 days life span.
- Fix branches: `fix/<ticket-id>` or `fix/<short-slug>`.
- Release tags: `vMAJOR.MINOR.PATCH` cut from `main` (see
  `docs/development/semver-policy.md`).
- `git rebase` over `git merge` — we keep history linear.
- **Never** force-push `main`. If you absolutely must rewrite a topic
  branch, warn the reviewer first.

## 2. Commit Conventions

Follow Conventional Commits with a scope prefix:

```
<type>(<scope>): <subject in imperative mood, <=72 chars>

[optional body, wrapped at 80]

[optional footer(s)]
```

| Type | Usage |
|---|---|
| `feat` | New feature or capability |
| `fix` | Bug fix |
| `refactor` | Non-functional code reshaping |
| `docs` | Documentation only |
| `test` | Tests only |
| `chore` | Build / deps / tooling |
| `ci` | GitHub Actions / CI pipeline |
| `perf` | Performance improvement |
| `security` | Security hardening (always CC `@security-team`) |
| `compliance` | KVKK / SOC 2 / ISO 27001 related |

Scope is the top-level directory or concern: `api`, `agent`, `gateway`,
`console`, `portal`, `infra`, `qa`, `docs`, `adr`, `compose`, etc.

Examples:

```
feat(api): add /v1/endpoints/{id}/wipe remote command (#64)
fix(agent): MSI ServiceControl start on uninstall only
docs(CLAUDE.md): Faz 5 Wave 2 deployment handover
compliance(evidence): wire CC9.1 backup-run collector
```

Reference CLAUDE.md §0 roadmap item numbers where relevant.

## 3. Pull Request Requirements

Every PR must satisfy ALL of the following before review:

1. **Build** — `cargo check`, `go build ./...`, `pnpm build`, and
   `docker compose config` all green on your workstation
2. **Lint** — `cargo clippy -- -D warnings`, `go vet`, `pnpm lint`,
   `pnpm type-check`
3. **Tests** — all existing tests still pass; new features ship tests
4. **ADR reference** — if you touch a locked decision (CLAUDE.md §6), cite
   or propose a new ADR
5. **KVKK impact note** — answer "does this change the KVKK data flow,
   retention, or access control?" in the PR body (one sentence is fine)
6. **No sensitive data** — never commit `.env`, tokens, `.pem`, unseal
   keys. Pre-commit Gitleaks scan blocks obvious leaks
7. **Rebase against main** — no merge commits in the feature branch

## 4. Reviewer Assignment

- Single-layer changes (≤3 files in one app): any teammate
- Cross-layer (backend + frontend): assign the other half of the stack
  explicitly
- Anything in `apps/api/internal/audit/`, `apps/api/internal/evidence/`,
  `apps/api/internal/dsr/`, `docs/compliance/`, or `docs/policies/`:
  **requires DPO sign-off** in addition to engineering review
- `docs/adr/` changes: at least one of the named decision stewards
  (see ADR 0001)
- Rust `personel-agent/` or `personel-crypto/` changes: at least one
  Rust specialist with security engineer CC

## 5. Code Quality Gates

The CI `required-checks` matrix must be green before merge:

- `go test ./...` (all Go apps)
- `cargo test --workspace`
- `pnpm test` (console + portal)
- `docker compose -f docker-compose.yaml -f docker-compose.dev.yaml config`
- `gitleaks detect`
- `cargo audit` + `go mod audit` + `pnpm audit`

## 6. Release & Deploy

1. Lead engineer cuts a release branch on Wednesdays at noon TRT
2. QA runs `infra/scripts/final-smoke-test.sh` against staging
3. If smoke rapor yeşilse, DPO + CTO sign-off on Friday
4. Tag + release notes (see `docs/releases/`)
5. Rolling upgrade against pilot via Senaryo 6 of
   `docs/operations/pilot-walkthrough.md`

## 7. Reporting Security Vulnerabilities

Do NOT file a public issue. Email `security@personel.example` (placeholder
— update when real domain exists) with the words "SECURITY" in the
subject. Response SLA: 72 hours.

See `docs/security/runbooks/incident-response-playbook.md` for our side
of the process.

## 8. CLA (Contributor License Agreement)

**Placeholder** — no CLA is required at this time since the repo is
closed to external contributors. When/if the project goes open source or
accepts external PRs, a DCO or Harmony CLA will be added here.

---

## 9. Further Reading

- `CLAUDE.md` — top-level project context for every session
- `docs/adr/` — Architecture Decision Records
- `docs/compliance/kvkk-framework.md` — KVKK obligations every change must respect
- `docs/development/semver-policy.md` — semver rules
- `apps/api/internal/postgres/migrations/README.md` — schema change contract
