# ADR 0007 — LiveKit (WebRTC SFU) for Live View

**Status**: Accepted

## Context

Live view requires low-latency (< 500 ms glass-to-glass) one-to-few screen streaming from an agent on a corporate Windows endpoint to an Admin Console in a browser. It must work through corporate NAT, support TURN fallback, integrate with our auth, and permit scoped, short-lived tokens.

## Decision

Use **LiveKit** as the WebRTC SFU. The agent acts as a **publisher**; the Admin Console acts as a **subscriber**. Tokens are minted by the Admin API using LiveKit JWT, scoped to a room, and expire at session cap.

## Consequences

- Open-source, self-hostable, on-prem friendly — aligns with ADR 0008.
- Battle-tested SFU with stable Go SDK (agent can use the C/Rust SDK or plain WebRTC crate; to be decided by rust-engineer).
- Room-based isolation maps cleanly to the "one session per request" model.
- WebRTC is firewall-unfriendly on some corporate networks; TURN/relay deployment is part of the install.
- Implementing a custom SFU would be months of work; LiveKit gives us this for free.
- Recording is explicitly disabled in Phase 1 (see live-view doc).

## Alternatives Considered

- **Janus Gateway**: mature but C-based, harder to operate on-prem for our target customers.
- **Mediasoup**: library, not a deployable product; more glue work.
- **Jitsi**: designed for many-to-many conferencing; feature-heavy and harder to lock down.
- **Custom TCP screen stream**: rejected — reinvents congestion control, NAT traversal, and security for no benefit.
- **MJPEG over HTTPS**: too high-latency, too bandwidth-heavy.
