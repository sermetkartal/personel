# ADR 0008 — On-Prem First Deployment (Docker Compose + systemd)

**Status**: Accepted

## Context

The Turkish enterprise market for employee monitoring is dominated by on-prem deployments. KVKK concerns, banking/public sector procurement rules, and customer data-sovereignty demands make SaaS a harder sell in Phase 1. We also want to keep operational surface small for early customers whose IT teams are generalists.

## Decision

Phase 1 ships as an **on-prem** stack deployed via **Docker Compose** for application containers and **systemd** for host-level services (vault-agent sidecars, backups, log rotation, compose supervisor). **No Kubernetes. No Helm.** Single-tenant per install is the blessed MVP posture; multi-tenant code paths exist but are disabled by default.

Operator artifacts:
- `infra/compose/docker-compose.yml` — full stack
- `infra/compose/docker-compose.dlp-host.yml` — DLP overlay for separate-host deployments
- `infra/systemd/personel-compose.service` — systemd unit supervising compose
- `infra/systemd/personel-vault-agent.service`
- `infra/systemd/personel-backup-*.service` + `.timer`

## Consequences

- Faster time to first customer install; no k8s learning curve.
- Simpler support: operators know how to read `journalctl` and `docker compose logs`.
- Horizontal scale requires manual replication (documented for 10k target).
- Rolling updates are coarser; canary is possible at the agent level but not gateway.
- Migration to Kubernetes (Phase 3) is kept plausible by keeping each container 12-factor: no host path coupling beyond named volumes, externalized config, stateless app containers.
- Helm charts are explicitly out of scope until the SaaS phase.

## Alternatives Considered

- **Kubernetes + Helm**: rejected for Phase 1 per Decision 2. Revisit for Phase 3 SaaS.
- **Nomad**: simpler than k8s but unfamiliar to our customer IT; no net benefit over Compose for single-host.
- **Bare systemd (no Docker)**: rejected — heterogeneous runtime dependencies (ClickHouse, OpenSearch, MinIO, LiveKit) make container packaging the right tradeoff.
- **Snap/Deb packages**: rejected — version coupling across 10+ components is painful.
