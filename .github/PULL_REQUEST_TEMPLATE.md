<!-- Thank you for contributing to Personel. Please read CONTRIBUTING.md before opening the PR. -->

## Summary

<!-- 1-3 sentences describing what this PR does and why. -->

## Related

- Roadmap item(s): <!-- e.g. CLAUDE.md §0 item #123 -->
- Issue(s): <!-- Fixes #NN, Refs #MM -->
- ADR(s): <!-- touches ADR 00NN, or new ADR at docs/adr/NNNN-... -->

## Changes

- [ ] Code: …
- [ ] Tests: …
- [ ] Docs: …

## Type of Change

- [ ] Bug fix (non-breaking)
- [ ] New feature (non-breaking)
- [ ] Breaking change (requires major bump + release note)
- [ ] Documentation only
- [ ] Infrastructure / build / CI
- [ ] Security hardening
- [ ] KVKK / compliance

## Checklist

### Build & Lint

- [ ] `go build ./...` green in every Go app I touched
- [ ] `cargo check --workspace` green (if Rust touched)
- [ ] `pnpm build` + `pnpm lint` + `pnpm type-check` green (if Next.js touched)
- [ ] `docker compose -f docker-compose.yaml -f docker-compose.dev.yaml config` green (if compose touched)

### Tests

- [ ] Unit tests added or updated
- [ ] Integration tests considered (testcontainers-go for Go; Playwright for UI)
- [ ] `apps/qa/cmd/smoke` still passes (for release branches)

### KVKK / Security

- [ ] No PII or secrets in commit history or logs
- [ ] Audit-before-side-effect rule respected for any new mutating endpoint
- [ ] Does NOT introduce any API returning raw keystroke content (ADR 0013)
- [ ] DPO sign-off obtained (only if this PR touches DSR, audit, evidence, or compliance code/docs)

### Documentation

- [ ] CLAUDE.md §0 state updated (if roadmap progress changed)
- [ ] Relevant ADR, runbook, or doc updated
- [ ] Migration README updated (if new Postgres migration added)

## Screenshots / Recordings

<!-- UI changes only. Otherwise delete this section. -->

## Reviewer Notes

<!-- Anything the reviewer should pay extra attention to. -->
