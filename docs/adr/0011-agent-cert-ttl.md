# ADR 0011 — Agent Client Certificate TTL: 14 Days

**Status**: Accepted (supersedes 30-day value in initial draft of `mtls-pki.md`)

## Context

The Phase 0 draft of `docs/architecture/mtls-pki.md` specified a 30-day TTL for per-endpoint agent client certificates. During security-engineer review of the PKI bootstrap runbook (`docs/security/runbooks/pki-bootstrap.md`), this was challenged: the agent maintains a persistent bidirectional gRPC stream to the gateway, and `ServerMessage.RotateCert` lets the server drive a zero-downtime rotation at any cadence. Under those conditions a 30-day TTL is conservative mainly for the benefit of offline endpoints — and offline endpoints are already handled by the local queue and 48-hour offline tolerance, not by long-lived certs.

The tradeoff: shorter TTL = shorter worst-case window for a stolen cert to be usable against the gateway between the moment of compromise and the moment of effective revocation.

## Decision

Agent client certificates are issued with a **14-day validity**. Automatic renewal is triggered at **T-3 days** over the existing gRPC stream via `ServerMessage.RotateCert`. Endpoints that remain offline past expiry must re-enroll (fall back to the bootstrap flow with a fresh enrollment token).

Server-side certificates (gateway, API, LiveKit, DLP, Update) keep their 90-day TTL — they are less numerous, rotation is heavier, and their threat profile is different.

## Consequences

- Worst-case usable window of a stolen agent cert is halved versus the 30-day baseline (assuming revocation propagation already handled by the deny-list cache; the 14-day TTL is the fallback bound when deny-list propagation fails).
- A 500-endpoint pilot generates ~36 rotation events/day on average; a 10 000-endpoint deployment generates ~715/day. Both are trivially within Vault PKI issuance budget.
- An endpoint offline for >14 days cannot simply resume — it must re-enroll. This is a deliberate forcing function: chronically offline endpoints should be re-evaluated anyway.
- Enrollment flow must be robust because it is now the recovery path for expired certs. Any enrollment bug affects all long-offline endpoints.
- Operational runbook language everywhere (incident response, DR) must reference 14 days, not 30.

## Alternatives Considered

- **30 days** (original draft): rejected — unnecessarily generous given the persistent control channel.
- **7 days**: rejected — the marginal security gain from 14→7 is small, and the re-enrollment burden for offline endpoints becomes painful (a two-week vacation traveler returns to a dead agent).
- **Variable TTL per endpoint risk score**: rejected for Phase 1 — adds policy complexity without clear benefit. Revisit Phase 2.
- **24-hour TTL with SPIFFE/SPIRE**: interesting but premature; SPIRE adds a full additional control-plane component. Revisit Phase 3.

## Related

- `docs/architecture/mtls-pki.md` §Rotation
- `docs/security/runbooks/pki-bootstrap.md` §7
- `proto/personel/v1/agent.proto` — `ServerMessage.RotateCert`, `AgentMessage.CsrSubmit`
