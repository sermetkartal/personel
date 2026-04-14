# Playwright E2E Smoke Suite (Faz 14 #148)

Five browser-level smoke tests that run against a live Personel
stack (console on :3000, portal on :3001). Uses the Keycloak
password grant to inject an access token without interactive redirects.

## Install

```bash
cd apps/qa/test/e2e-playwright
pnpm install
pnpm exec playwright install chromium
```

## Run

```bash
# All smoke specs against vm3
pnpm test:e2e

# Headed (watch the browser)
pnpm test:e2e:headed

# UI mode (interactive)
pnpm test:e2e:ui

# Against a custom stack
PERSONEL_CONSOLE_URL=http://localhost:3000 \
PERSONEL_PORTAL_URL=http://localhost:3001 \
PERSONEL_KEYCLOAK_URL=http://localhost:8080 \
  pnpm test:e2e
```

## Environment

| Variable | Default | Purpose |
|---|---|---|
| `PERSONEL_CONSOLE_URL` | `http://192.168.5.44:3000` | Admin console base URL |
| `PERSONEL_PORTAL_URL` | `http://192.168.5.44:3001` | Employee portal base URL |
| `PERSONEL_KEYCLOAK_URL` | `http://192.168.5.44:8080` | Keycloak base URL |
| `PERSONEL_REALM` | `personel` | Realm name |
| `PERSONEL_CLIENT_ID` | `console` | OIDC client id |
| `PERSONEL_ADMIN_USER` | `admin` | Admin username |
| `PERSONEL_ADMIN_PASSWORD` | `admin123` | Admin password (test realm only) |
| `PERSONEL_EMPLOYEE_USER` | `zeynep` | Showcase employee username |
| `PERSONEL_EMPLOYEE_PASSWORD` | `employee123` | Showcase employee password |
| `PERSONEL_TENANT_ID` | `be459dac-...-a927315` | Test tenant uuid |
| `PERSONEL_API_URL` | `http://192.168.5.44:8000` | Admin API base URL |

## Specs

| File | What it smokes |
|---|---|
| `console-login-happy.spec.ts` | Admin login + dashboard + showcase tiles |
| `employee-detail.spec.ts` | Click Zeynep → detail page rich signals grid |
| `audit-search.spec.ts` | Audit viewer search `endpoint.wipe` |
| `dsr-dry-run.spec.ts` | DPO DSR erasure dry-run (no destruction) |
| `portal-first-login.spec.ts` | Employee first-login aydınlatma ack |

## CI

Staged workflow in `infra/ci-scaffolds/e2e.yml` (workflow-scope
token required to push). When a human with the right scope runs
the workflow, it:

1. Boots the pilot compose stack in the runner
2. Seeds showcase employees via `POST /v1/admin/seed/showcase`
3. Runs all 5 specs
4. Uploads `playwright-report/` + `playwright-results.json` as
   artifacts, always (success or fail)

## Troubleshooting

- **"Keycloak password grant failed 401"**: direct-grant is disabled
  on the target client. Use a test-only client or enable
  direct-grant in the test realm via `infra/compose/keycloak/`.
- **"testcontainers-go: Docker socket unreachable"**: not related
  — this suite is browser-level, not container-level.
- **Traces on fail**: every failed test leaves a trace in
  `apps/qa/reports/playwright-report/trace.zip`. Open with
  `pnpm exec playwright show-trace trace.zip`.
