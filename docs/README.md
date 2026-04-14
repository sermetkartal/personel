# Personel — Documentation Index / Doküman İndeksi

> Bilingual index. TR = Turkish, EN = English.

## Architecture / Mimari

| # | File | Lang | Description (EN) | Açıklama (TR) |
|---|---|---|---|---|
| 1 | [architecture/overview.md](architecture/overview.md) | TR | Executive system overview | Yönetici düzeyinde sistem özeti |
| 2 | [architecture/c4-context.md](architecture/c4-context.md) | EN | C4 L1 system context | C4 Seviye 1 sistem bağlamı |
| 3 | [architecture/c4-container.md](architecture/c4-container.md) | EN | C4 L2 container diagram | C4 Seviye 2 konteyner diyagramı |
| 4 | [architecture/bounded-contexts.md](architecture/bounded-contexts.md) | EN | DDD bounded-context map | DDD sınırlı bağlam haritası |
| 5 | [architecture/event-taxonomy.md](architecture/event-taxonomy.md) | EN | Canonical event catalog (36 types) | Kanonik olay kataloğu |
| 6 | [architecture/data-retention-matrix.md](architecture/data-retention-matrix.md) | TR | KVKK retention matrix | KVKK saklama matrisi |
| 7 | [architecture/mtls-pki.md](architecture/mtls-pki.md) | EN | mTLS / PKI hierarchy | mTLS / PKI hiyerarşisi |
| 8 | [architecture/key-hierarchy.md](architecture/key-hierarchy.md) | EN | Keystroke content key hierarchy | Klavye içerik anahtar hiyerarşisi |
| 9 | [architecture/live-view-protocol.md](architecture/live-view-protocol.md) | EN | Live view protocol + dual control | Canlı izleme protokolü |
| 10 | [architecture/agent-module-architecture.md](architecture/agent-module-architecture.md) | EN | Rust agent internal modules | Rust ajan iç modülleri |
| 11 | [architecture/mvp-scope.md](architecture/mvp-scope.md) | EN | Phase 1 scope and exit criteria | Faz 1 kapsamı ve çıkış kriterleri |
| 12 | [architecture/dlp-deployment-profiles.md](architecture/dlp-deployment-profiles.md) | EN | DLP container vs dedicated host profiles | DLP konteyner ve dedike host profilleri |
| 13 | [architecture/clickhouse-scaling-plan.md](architecture/clickhouse-scaling-plan.md) | EN | ClickHouse Stage 1/2/3 topology plan | ClickHouse ölçekleme planı |
| 14 | [architecture/audit-chain-checkpoints.md](architecture/audit-chain-checkpoints.md) | EN | Audit hash-chain checkpoint system | Denetim zinciri checkpoint sistemi |

## ADRs

| # | File | Decision |
|---|---|---|
| 0001 | [adr/0001-monorepo-layout.md](adr/0001-monorepo-layout.md) | Monorepo layout |
| 0002 | [adr/0002-rust-for-agent.md](adr/0002-rust-for-agent.md) | Rust for the endpoint agent |
| 0003 | [adr/0003-grpc-bidirectional-streaming.md](adr/0003-grpc-bidirectional-streaming.md) | gRPC bidi streaming agent↔gateway |
| 0004 | [adr/0004-clickhouse-for-timeseries.md](adr/0004-clickhouse-for-timeseries.md) | ClickHouse for telemetry |
| 0005 | [adr/0005-nats-jetstream-event-bus.md](adr/0005-nats-jetstream-event-bus.md) | NATS JetStream event bus |
| 0006 | [adr/0006-postgresql-metadata.md](adr/0006-postgresql-metadata.md) | PostgreSQL for metadata |
| 0007 | [adr/0007-livekit-webrtc-live-view.md](adr/0007-livekit-webrtc-live-view.md) | LiveKit WebRTC for live view |
| 0008 | [adr/0008-on-prem-first-deployment.md](adr/0008-on-prem-first-deployment.md) | On-prem first deployment |
| 0009 | [adr/0009-keystroke-content-encryption.md](adr/0009-keystroke-content-encryption.md) | Keystroke content encryption (admin-blind) |
| 0010 | [adr/0010-windows-user-mode-phase1.md](adr/0010-windows-user-mode-phase1.md) | Windows user-mode Phase 1 |
| 0011 | [adr/0011-agent-cert-ttl.md](adr/0011-agent-cert-ttl.md) | Agent client cert TTL = 14 days |
| 0012 | [adr/0012-live-view-recording-phase2.md](adr/0012-live-view-recording-phase2.md) | Live view recording (Phase 2 envelope) |
| 0013 | [adr/0013-dlp-disabled-by-default.md](adr/0013-dlp-disabled-by-default.md) | DLP disabled by default in Phase 1 (opt-in ceremony) |

## Security / Güvenlik

| File | Lang | Description |
|---|---|---|
| [security/threat-model.md](security/threat-model.md) | EN | STRIDE across seven critical flows (incl. Flow 7 — Employee-initiated disable) |
| [security/anti-tamper.md](security/anti-tamper.md) | EN | User-mode anti-tamper strategy |
| [security/security-architecture-decisions.md](security/security-architecture-decisions.md) | EN | Security-engineer architecture decisions |
| [security/runbooks/](security/runbooks/) | EN | PKI bootstrap, DLP isolation, audit immutability, Vault setup, secret rotation, IR, auto-update signing |

## Compliance / Uyum

| File | Lang | Description |
|---|---|---|
| [compliance/kvkk-framework.md](compliance/kvkk-framework.md) | TR | KVKK compliance framework (6698 sayılı Kanun uygulaması) |
| [compliance/aydinlatma-metni-template.md](compliance/aydinlatma-metni-template.md) | TR | Aydınlatma metni şablonu |
| [compliance/acik-riza-metni-template.md](compliance/acik-riza-metni-template.md) | TR | Açık rıza metni şablonu |
| [compliance/calisan-bilgilendirme-akisi.md](compliance/calisan-bilgilendirme-akisi.md) | TR | Çalışan bilgilendirme akışı |
| [compliance/dpia-sablonu.md](compliance/dpia-sablonu.md) | TR | DPIA şablonu |
| [compliance/hukuki-riskler-ve-azaltimlar.md](compliance/hukuki-riskler-ve-azaltimlar.md) | TR | Hukuki riskler ve azaltımlar |
| [compliance/iltica-silme-politikasi.md](compliance/iltica-silme-politikasi.md) | TR | İmha/silme politikası |
| [compliance/verbis-kayit-rehberi.md](compliance/verbis-kayit-rehberi.md) | TR | VERBİS kayıt rehberi |

## Operations / İşletim (Faz 5-13)

| File | Lang | Description |
|---|---|---|
| [operations/installation-guide.md](operations/installation-guide.md) | TR | Pilot kurulum rehberi |
| [operations/ops-runbook.md](operations/ops-runbook.md) | TR | Başlat/durdur/yeniden başlat + sorun giderme |
| [operations/troubleshooting.md](operations/troubleshooting.md) | TR | Sorun giderme kılavuzu |
| [operations/backup-restore.md](operations/backup-restore.md) | TR | Backup + restore prosedürleri |
| [operations/admin-onboarding.md](operations/admin-onboarding.md) | TR | Yeni admin onboarding |
| [operations/pilot-walkthrough.md](operations/pilot-walkthrough.md) | TR | **6 senaryo demo akışı** (Faz 17 #188) |
| [operations/postgres-replication.md](operations/postgres-replication.md) | TR | Postgres replica bring-up |
| [operations/postgres-tls-migration.md](operations/postgres-tls-migration.md) | TR | Postgres TLS migration |
| [operations/clickhouse-cluster.md](operations/clickhouse-cluster.md) | TR | ClickHouse 2-node + keeper |
| [operations/nats-minio-cluster.md](operations/nats-minio-cluster.md) | TR | NATS cluster + MinIO distributed |
| [operations/nats-prod-auth-migration.md](operations/nats-prod-auth-migration.md) | TR | NATS operator JWT migration |
| [operations/minio-worm-migration.md](operations/minio-worm-migration.md) | TR | MinIO Object Lock (Compliance) migration |
| [operations/opensearch-keycloak-cluster.md](operations/opensearch-keycloak-cluster.md) | TR | OpenSearch + Keycloak HA |
| [operations/vault-prod-migration.md](operations/vault-prod-migration.md) | TR | Vault prod ceremony |
| [operations/all-services-tls-migration.md](operations/all-services-tls-migration.md) | TR | 18 servis TLS rotasyonu |
| [operations/secret-rotation.md](operations/secret-rotation.md) | TR | Secret rotation automation |
| [operations/healthcheck-restoration.md](operations/healthcheck-restoration.md) | TR | `service_started` → `service_healthy` |
| [operations/registry-policies.md](operations/registry-policies.md) | TR | Container image registry policies |
| [operations/code-signing.md](operations/code-signing.md) | TR | EV code signing setup |
| [operations/gpo-deployment.md](operations/gpo-deployment.md) | TR | GPO / Intune deployment |
| [operations/network-segmentation.md](operations/network-segmentation.md) | TR | Ağ segmentasyonu |
| [operations/bastion.md](operations/bastion.md) | TR | Bastion host config |
| [operations/vpn-setup.md](operations/vpn-setup.md) | TR | VPN setup rehberi |
| [operations/grafana-tenant-isolation.md](operations/grafana-tenant-isolation.md) | TR | Grafana tenant isolation |
| [operations/cost-monitoring.md](operations/cost-monitoring.md) | TR | Maliyet izleme scaffold'u |
| [operations/chaos-scenarios/](operations/chaos-scenarios/) | TR | Chaos engineering scenarios |

## Policies / ISO 27001 + SOC 2 Type II

| File | Lang | Description |
|---|---|---|
| [policies/access-review.md](policies/access-review.md) | TR+EN | Çeyrek dönem access review policy |
| [policies/change-management.md](policies/change-management.md) | TR+EN | Change management policy |
| [policies/incident-response.md](policies/incident-response.md) | TR+EN | Incident response policy |
| [policies/vendor-management.md](policies/vendor-management.md) | TR+EN | Vendor management policy |
| [policies/business-continuity-disaster-recovery.md](policies/business-continuity-disaster-recovery.md) | TR+EN | BCP/DR policy |

## Development

| File | Lang | Description |
|---|---|---|
| [development/semver-policy.md](development/semver-policy.md) | EN | Semver + release tagging policy |

## Product / Strateji

| File | Description |
|---|---|
| [product/competitive-analysis.md](product/competitive-analysis.md) | Teramind / ActivTrak / Veriato / Insightful / Safetica teardown |

## Sales / Demo / POC

| File | Description |
|---|---|
| [sales/one-pager-tr.md](sales/one-pager-tr.md) | Tek sayfalık ürün özeti |
| [sales/demo-deck-outline.md](sales/demo-deck-outline.md) | Demo sunumu iskeleti |
| [sales/poc-guide.md](sales/poc-guide.md) | POC kurulum rehberi |
| [sales/roi-calculator-tr.md](sales/roi-calculator-tr.md) | ROI hesaplama dokümanı |
| [sales/sales-faq-tr.md](sales/sales-faq-tr.md) | Satış SSS |

## User Manuals / Kullanıcı Kılavuzları

| File | Description |
|---|---|
| [user-manuals/admin-manual-tr.md](user-manuals/admin-manual-tr.md) | Admin kullanıcı kılavuzu |
| [user-manuals/employee-manual-tr.md](user-manuals/employee-manual-tr.md) | Çalışan kullanıcı kılavuzu |

## Proto (Source of Truth)

- `proto/personel/v1/common.proto`
- `proto/personel/v1/agent.proto`
- `proto/personel/v1/events.proto`
- `proto/personel/v1/policy.proto`
- `proto/personel/v1/live_view.proto`

## Reading Order for New Contributors / Yeni Katkıcılar İçin Okuma Sırası

1. `README.md` (repo root)
2. `architecture/overview.md` (TR exec)
3. `architecture/mvp-scope.md` (what's in/out)
4. All ADRs (short)
5. `architecture/c4-context.md` + `c4-container.md`
6. `architecture/event-taxonomy.md`
7. `architecture/key-hierarchy.md` + `adr/0009`
8. `architecture/live-view-protocol.md`
9. `security/threat-model.md`
10. Proto files
