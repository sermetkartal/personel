# ADR 0003 — gRPC Bidirectional Streaming Between Agent and Gateway

**Status**: Accepted

## Context

The agent must (a) push high-volume event batches, (b) receive live policy updates, live-view control, and update notifications, and (c) survive intermittent connectivity, all while running on `LocalSystem` with a strict CPU budget. The wire format and RPC style set the tone for everything downstream.

## Decision

A single **gRPC bidirectional stream** between agent and gateway, transported over HTTP/2 with mTLS. One long-lived `rpc Stream(stream AgentMessage) returns (stream ServerMessage)` per connected agent. Protobuf (`proto3`) is the only wire format.

Framing details:
- `AgentMessage` is a oneof envelope: `Hello`, `Heartbeat`, `EventBatch`, `PolicyAck`, `UpdateAck`, `LiveViewEvent`, `Csr`.
- `ServerMessage` is a oneof envelope: `Welcome`, `PolicyPush`, `UpdateNotify`, `LiveViewStart`, `LiveViewStop`, `RotateCert`, `PinUpdate`, `Ping`.
- Agent reconnect uses exponential backoff (1s → 60s) with jitter. Resumption cookie is sent in `Hello` to avoid full policy re-push.

## Consequences

- One TCP+TLS connection per agent instead of per-request churn; scales to 10k with sensible pooling on the gateway side.
- Server-push capability without extra protocol — critical for live-view and policy.
- No REST/JSON surface to document or throttle.
- gRPC is bandwidth-lean vs JSON, and prost-generated Rust is zero-copy-friendly.
- Gateway must be written with stream lifecycle carefully (goroutine per stream in Go).
- Proxies/WAFs in between can mangle HTTP/2; we mitigate by terminating TLS at our own gateway and not allowing customer WAFs to MITM the agent path.

## Alternatives Considered

- **MQTT**: rejected — excellent for pub/sub but weaker for request/response patterns (live-view handshakes), and MQTT broker adds a component without benefit over NATS internally.
- **WebSocket + JSON**: rejected — heavier on CPU for the agent, no schema enforcement, more fragile over long-lived connections.
- **Raw TCP + custom framing**: rejected — loss of tooling, observability, and language binding ecosystem.
- **gRPC unary + long-polling**: rejected — higher latency for server push; more connection churn.
