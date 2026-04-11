# ADR 0001 — Monorepo Layout

**Status**: Accepted (Phase 0)

## Context

Personel comprises a Rust agent, multiple Go services, two Next.js apps, shared protobuf schemas, infra assets, and docs. Fifteen-plus specialist agents will contribute concurrently. We need a single source of truth for cross-cutting types (proto), one atomic CI history, and fast onboarding.

## Decision

A single Git monorepo at the repo root with this top-level layout:

```
personel/
├── apps/
│   ├── agent/      (Rust — cargo workspace inside)
│   ├── gateway/    (Go)
│   ├── api/        (Go)
│   ├── console/    (Next.js)
│   ├── portal/     (Next.js)
│   └── dlp/        (Go, isolated)
├── packages/
│   └── proto/      (generated stubs output; sources in /proto)
├── proto/
│   └── personel/v1/
├── infra/
│   ├── compose/
│   └── systemd/
├── docs/
│   ├── architecture/
│   ├── adr/
│   ├── security/
│   └── runbooks/   (authored later)
└── README.md
```

Build tooling: per-language native (cargo, go mod, pnpm). A top-level `Taskfile.yml` or `Makefile` stitches CI. Proto generation writes to `packages/proto/{go,ts,rust}`.

## Consequences

- One PR can ship coordinated changes across proto, agent, backend, frontend.
- CI becomes aware of per-language caches; initial setup cost.
- No git submodules. No polyrepo coordination burden.
- Release tagging is per-component (`agent-v1.0.3`, `gateway-v1.0.3`) not monolithic.

## Alternatives Considered

- **Polyrepo**: rejected — proto drift risk is unacceptable given DLP/keystroke cryptographic invariants.
- **Bazel**: rejected — too much upfront cost for a 6-person pilot team; revisit at Phase 3.
- **Nx/Turborepo as top-level orchestrator**: partial — will use for the two Next.js apps only.
