# ADR 0005 — NATS JetStream as Internal Event Bus

**Status**: Accepted

## Context

Between the Ingest Gateway and the downstream consumers (ClickHouse writer, DLP service, OpenSearch indexer, Audit service, alerting) we need a durable, high-throughput, replayable message bus that is operationally simple on-prem and supports per-consumer positions.

## Decision

Use **NATS JetStream** as the single internal event bus. One stream per top-level subject family (`events`, `live_view`, `policy`, `updates`, `pki`). Consumers are durable and named per service. Messages are protobuf-encoded.

Subject hierarchy:
- `events.v1.<event_type>.<tenant>` — telemetry fan-out
- `policy.v1.push.<tenant>.<endpoint>` — policy distribution
- `pki.v1.revoke` — cluster-wide revocations
- `updates.v1.notify.<tenant>` — update campaigns
- `dlp.v1.match.<tenant>` — DLP results
- `live_view.v1.control.<tenant>` — live-view control fan-out to gateway pods

## Consequences

- Single binary broker, single port, easy on-prem ops.
- Durable consumers provide replay for recovering from ClickHouse or OpenSearch outages.
- Subject-based wildcard consumers keep the gateway simple (one publish per event, fan-out is the bus's job).
- JetStream limits and retention policies must be tuned per stream to prevent disk blowouts.
- No strong ordering across subjects; per-subject ordering is sufficient for our consumers.
- Clustering in future requires 3-node JetStream quorum — documented but not needed for MVP.

## Alternatives Considered

- **Apache Kafka**: rejected — operational footprint too heavy for small on-prem customers; brings ZooKeeper-era ops baggage or a very new KRaft path; disk tuning burden.
- **Redpanda**: attractive but single-vendor operational familiarity in Turkey is limited; NATS is simpler.
- **RabbitMQ**: rejected — weaker streaming semantics and replay story.
- **Direct Kafka-compatible API on Redpanda**: revisit at Phase 3 if customers demand Kafka compatibility.
