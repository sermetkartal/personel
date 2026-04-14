---
name: Bug report
about: Report a defect in Personel
title: "[BUG] "
labels: bug, triage
assignees: ''
---

## Summary

<!-- One-paragraph description of what went wrong. -->

## Affected Component

- [ ] Rust agent (`apps/agent`)
- [ ] Gateway / Enricher (`apps/gateway`)
- [ ] Admin API (`apps/api`)
- [ ] Console (`apps/console`)
- [ ] Transparency portal (`apps/portal`)
- [ ] Infra / Compose / systemd (`infra/`)
- [ ] Documentation (`docs/`)
- [ ] Other: ________

## Environment

- Personel version: `v0.x.x` (from `docker compose exec api /personel-api --version`)
- OS / distro (backend): `Ubuntu 24.04`
- Windows endpoint build: `Win10 22H2` / `Win11 23H2`
- Deployment type: [ ] Pilot [ ] Staging [ ] Production

## Steps to Reproduce

1. …
2. …
3. …

## Expected Behavior

<!-- What should have happened. -->

## Actual Behavior

<!-- What actually happened. Include exact error messages. -->

## Logs / Screenshots

<!-- Attach relevant log excerpts. SCRUB any PII, JWT, secret, or KVKK-protected data. -->

```
<paste scrubbed log here>
```

## KVKK Impact

- [ ] No personal / special-category data leaked in this bug
- [ ] Personal data may have been mishandled — **notify DPO within 4 hours**
- [ ] Keystroke / DLP related (ADR 0013) — **block release until triaged**

## Additional Context

<!-- Linked audit IDs, related PRs, ADR references, etc. -->
