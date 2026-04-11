# Personel QA Test Strategy

## Scope

This QA framework validates all automated Phase 1 exit criteria for the Personel UAM platform. It covers the gateway, API, and event pipeline — not the Rust agent internals (those have unit tests in `apps/agent/`).

## Test Pyramid

```
      /\
     /  \    Manual / Pilot
    /    \   (14-day EC-1, EC-11–EC-16)
   /------\
  /        \  Security Red Team
 /          \ (EC-9 — blocking; runs on every main push)
/------------\
/            \ E2E Integration Tests
/              (EC-5, EC-6, EC-7, EC-10, EC-13, EC-18–EC-21)
/--------------\
/                \ Load Tests
/                  (EC-6, EC-7, EC-8 at scale; weekly + on-demand)
/------------------\
/                    \ Unit Tests (no containers; fast)
/____________________\ (cert math, crypto, generator distribution, assertions)
```

## Priority Order

1. **EC-9 (keystroke admin-blindness)** — Runs on every main branch push. Any failure is a Phase 1 blocker. Never make this `continue-on-error`.
2. **EC-6 (event loss < 0.01%)** — Runs in 500-agent load test. Core reliability guarantee.
3. **EC-10 (live-view governance)** — Runs in e2e. Core legal guarantee.
4. **EC-7 (latency p95 < 5s)** — Runs in load test.
5. **EC-5 (dashboard p95 < 1s)** — Runs against real ClickHouse in e2e.
6. All other criteria.

## Test Gates

| Gate | Trigger | Required to Pass |
|------|---------|-----------------|
| PR gate | Every PR to main/develop | lint, unit, EC-9 red team |
| Main branch gate | Every push to main | All above + e2e + short fuzz |
| Release gate | Manual release | All above + 500-agent load + phase1 exit report |
| Phase 1 exit | Before first customer | All 21 criteria verified |

## Determinism

All simulator scenarios use seeded RNGs for reproducible runs. The seed is embedded in each scenario JSON. Zero-seed means random (used in production load tests; never in CI).

## Test Data Policy

- Tests either use testcontainers (isolated stack) or are gated behind `QA_INTEGRATION=1`.
- No test writes to production or staging databases without explicit `QA_TARGET=staging` env var.
- Test PKI certs expire in 14 days (matching production) to catch rotation logic issues.
