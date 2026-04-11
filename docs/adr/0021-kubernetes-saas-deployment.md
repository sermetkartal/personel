# ADR 0021 — Kubernetes SaaS Deployment

**Status**: Accepted (Phase 3)
**Amends**: ADR 0008 (On-Prem First Deployment) — does NOT supersede. Docker Compose + systemd remain blessed for on-prem; Kubernetes is the SaaS runtime only.
**Related**: ADR 0020 (SaaS Multi-Tenant Architecture)

## Context

ADR 0020 commits Phase 3 to a true multi-tenant SaaS. This requires a runtime that supports multi-region, rolling upgrades, autoscaling, and GitOps. Docker Compose cannot meet these requirements at SaaS scale.

Constraints:

- SaaS runtime must be operable by a small DevOps team (1 FTE in Phase 3).
- On-prem runtime (Compose + systemd) must remain the blessed path. Helm charts for on-prem become **available-but-unsupported** in Phase 3.5.
- Stateful services (ClickHouse, Vault, NATS JetStream, MinIO, Postgres) require careful handling; "cloud-native operators" are mature for most of these but not all.
- No lock-in to a single cloud provider — the TR region may be a local DC without AWS/GCP availability; the EU region will likely be on a hyperscaler. The Helm chart must be cloud-agnostic.
- mTLS must be rotated without operator intervention (dense operational burden otherwise).
- Service mesh adds complexity; must pick the simplest mesh that meets the requirement.

## Decision

### Orchestrator

**Kubernetes** (upstream, minimum version 1.30 at Phase 3.1 start). No managed distribution lock-in; the chart works on EKS, GKE, AKS, OpenShift, and upstream k3s for small regions.

### Helm chart structure

```
charts/personel/
├── Chart.yaml                 # umbrella chart, lists subcharts
├── values.yaml                # defaults
├── values-saas-tr.yaml        # TR SaaS region overrides
├── values-saas-eu.yaml        # EU SaaS region overrides
├── values-onprem.yaml         # on-prem bundle (Phase 3.5, unsupported)
├── templates/                 # shared cross-chart templates (NetworkPolicy, PSS, etc.)
└── charts/
    ├── gateway/               # stateless ingest gateway Deployment
    ├── enricher/              # stateless enricher Deployment
    ├── api/                   # stateless admin API Deployment
    ├── console/               # stateless Next.js console Deployment
    ├── portal/                # stateless transparency portal Deployment
    ├── ml-classifier/         # Python service, Deployment + HPA
    ├── ocr/                   # Python service, Deployment + HPA
    ├── uba/                   # Python service, Deployment + HPA
    ├── livrec/                # Go service, StatefulSet (per-tenant temp storage)
    ├── mobile-bff/            # (if split out later)
    ├── postgres/              # CloudNativePG operator CR
    ├── clickhouse/            # Altinity operator CR, StatefulSet
    ├── nats/                  # NATS Helm subchart pinned, JetStream enabled, StatefulSet
    ├── minio/                 # MinIO operator Tenant CR
    ├── opensearch/            # OpenSearch operator CR
    ├── vault/                 # Banzai Cloud Vault operator CR, StatefulSet, namespace per tenant
    ├── keycloak/              # Keycloak operator CR
    ├── livekit/               # StatefulSet (for live view SFU)
    ├── observability/         # Prometheus, Grafana, Loki, Tempo stack (kube-prometheus-stack)
    └── linkerd/               # Linkerd control plane (see mesh choice)
```

Values hierarchy: `values.yaml` → `values-saas-{region}.yaml` → customer overlay (per-region ArgoCD Application) → last-mile image tag pin.

### Workload primitives

- **Stateless** (gateway, enricher, api, console, portal, ml-classifier, ocr, uba): `Deployment` + `HorizontalPodAutoscaler` keyed on CPU + custom latency metric.
- **Stateful** (clickhouse, vault, nats, postgres, opensearch, minio, livekit): `StatefulSet` managed by respective operators where operators are stable. CloudNativePG for Postgres, Altinity ClickHouse operator, NATS Helm StatefulSet pinned, MinIO operator, Banzai Vault operator.
- **Jobs** (backup, Velero, DSR export): `CronJob`.

No DaemonSets (endpoint agents are not in the cluster).

### Multi-region deployment

- One Argo CD "source of truth" cluster per region. Argo CD on each region cluster reconciles its own workloads.
- A central **meta-repo** holds region-pinned Applications; merges to main trigger Argo CD sync.
- Cross-region traffic is explicitly NOT permitted for tenant data. The only cross-region link is the control plane (Argo CD management, aggregated observability, billing reconciliation).
- Control plane and data plane are **logically separated**: the Personel SaaS control plane (billing, signup, provisioning orchestrator) runs in one primary region (TR); the data planes (tenant data) run in the tenant's pinned region.

### GitOps

**Argo CD** for continuous deployment. App-of-apps pattern: top-level Application references region-specific Applications referencing subcharts. Argo CD Image Updater pinned to signed image digests. Progressive rollouts via Argo Rollouts (canary to 10% → 50% → 100% of pods with automatic rollback on SLO burn).

### Service mesh: Linkerd

**Decision: Linkerd**, not Istio, not "no mesh".

Rationale:
- Istio is feature-rich but operationally heavy; two-person DevOps cannot realistically run it at SaaS scale.
- No-mesh leaves mTLS rotation to cert-manager + manual secret wiring, which is error-prone for inter-service traffic.
- Linkerd gives automatic mTLS between all pods, per-route metrics, retries, timeouts, and traffic splitting — the 80% of mesh value at 20% of the operational cost.
- Linkerd's proxy is Rust (predictable memory), CPU overhead < 2% in observed benchmarks.

Linkerd injects automatically via namespace label. mTLS identities derive from ServiceAccount tokens via Linkerd identity controller.

### Certificates

- **cert-manager** issues internal certificates (Linkerd trust anchor rotation, external TLS for ingress).
- **External certificates** (customer-facing hostnames): cert-manager + Let's Encrypt DNS-01 for the SaaS-managed subdomains; customer custom domains via BYO cert uploaded through console.
- Linkerd trust anchor is rotated every 90 days automatically.
- Tenant agent enrollment PKI remains in Vault (ADR 0020), separate from cert-manager.

### Secrets

**External Secrets Operator (ESO)** with Vault backend. No secrets stored as Kubernetes Secret objects manually; every Secret is generated from `ExternalSecret` CRs that reference Vault paths under tenant namespaces. ESO refresh interval: 5 minutes.

### Storage

- **Block storage** (Postgres, ClickHouse, Vault, NATS JetStream): cloud-provider CSI driver with encryption-at-rest enabled at CSI level. In the TR region, this may be Ceph RBD or an on-prem CSI if hyperscaler unavailable.
- **Object storage for app data**: MinIO operator StatefulSet (NOT the cloud provider's native S3, to preserve on-prem parity and tenant bucket isolation model).
- **Object storage for cluster backup**: cloud provider S3-compatible (separate from MinIO).

### Backup

**Velero + Restic**:

- Velero orchestrates backups; Restic is the file-level backend for PVs (encrypted repo).
- Postgres backed up via CloudNativePG continuous WAL + weekly full (NOT Velero — operator native is more reliable).
- ClickHouse backed up via `clickhouse-backup` sidecar → S3 bucket in secondary region.
- Vault backed up via `vault operator raft snapshot` → S3 bucket (encrypted).
- MinIO backed up via bucket replication to a secondary bucket in a different AZ.
- RTO target: 4 hours. RPO target: 15 minutes for Postgres, 1 hour for ClickHouse, 5 minutes for Vault.

### Observability

`kube-prometheus-stack` (Prometheus + Grafana + Alertmanager) + Loki (logs) + Tempo (traces via OpenTelemetry). Per-tenant dashboards exposed in admin console via read-only Grafana API proxy. Control-plane observability aggregated into a separate, company-only observability cluster.

### Network policies

Default deny all namespaces. Explicit allow policies for:
- Gateway → NATS
- Enricher → NATS, ClickHouse, MinIO, Postgres
- API → Postgres, ClickHouse, Vault, MinIO, NATS, Keycloak
- Console/Portal → API only
- All → observability stack (scrape)
- Ingress → Console, Portal, API, Gateway

Pod Security Standards: `restricted` profile enforced via admission controller.

## Consequences

### Positive

- Clean rolling upgrades, canary deployments, zero-downtime database failovers.
- mTLS between every pod automatically, rotated continuously.
- One Helm chart works in both SaaS regions and as the "available-but-unsupported" on-prem K8s path.
- Argo CD gives audit trail for every production change (SOC 2 CC8.1 change management).

### Negative

- Operational surface larger than Compose; DevOps must learn all operators.
- Operator version drift is a real risk; quarterly operator upgrade drills required.
- Cluster itself is another audit scope — PCI-style "cluster as boundary" must be documented in SOC 2 system description.
- On-prem Compose remains blessed and must be kept in sync with the Helm chart (two packaging paths).

### Compliance

- SOC 2 CC8.1 change management: Argo CD audit log is evidence substrate.
- ISO 27001 A.8.32 change management: Argo CD + GitHub branch protection + required reviewers.
- GDPR Art. 32: mTLS-everywhere + encrypted-at-rest + region pinning.

## Alternatives Considered

- **Istio service mesh** — rejected for operational complexity vs one-DevOps team.
- **No service mesh** — rejected; manual mTLS wiring is error-prone and SOC 2 evidence is harder.
- **OpenShift** — rejected as a default for cloud-agnosticism reasons, but customers running OpenShift can still use the Helm chart.
- **Managed k8s lock-in (EKS only)** — rejected; TR region may not have EKS availability.
- **Nomad** — rejected; ecosystem too narrow for the operator landscape we need (Vault operator, ClickHouse operator, etc.).
- **Self-managed bare k8s on VMs** — acceptable fallback for TR region, documented in the region runbook.

## Cross-references

- `docs/adr/0020-saas-multi-tenant-architecture.md`
- `docs/adr/0008-on-prem-first-deployment.md` — amended
- `docs/architecture/phase-3-roadmap.md` — Phase 3.2 K8s deployment weeks
