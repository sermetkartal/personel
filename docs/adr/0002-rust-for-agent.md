# ADR 0002 — Rust for the Endpoint Agent

**Status**: Accepted

## Context

The endpoint agent runs on hundreds of thousands of Windows laptops under a strict performance budget (<2% CPU, <150 MB RAM), must handle encrypted local queueing of sensitive data, must resist tampering and memory disclosure attacks, and will eventually host cryptographic primitives for keystroke content encryption. Language choice constrains everything: perf, safety, hiring, build chain, and anti-tamper story.

## Decision

Write the endpoint agent in **Rust**, using `tokio` as the async runtime, `tonic` for gRPC, `rustls` for TLS, `prost` for proto, `ring` for crypto, and the official `windows` crate for Win32 bindings.

## Consequences

- Memory safety eliminates an entire category of exploit classes relevant to an agent running as `LocalSystem`.
- Native single-binary distribution; no runtime, no GC pauses.
- Compile times are nontrivial; CI caching matters.
- Hiring pool in Turkey is smaller than Go/C#; mitigated by the fact that only 1–3 people own the agent.
- Excellent crypto and async ecosystems; no OpenSSL dependency.
- ETW bindings are thinner than in C# — we wrap our own helper.
- Interop with C for any future minifilter driver is straightforward (`cc-rs`, `bindgen`).

## Alternatives Considered

- **C++**: rejected — memory-safety burden conflicts with `LocalSystem` trust level and the keystroke-encryption invariant.
- **C#/.NET**: rejected — runtime footprint, GC jitter, and the need for aggressive obfuscation against tampering. Viable for admin tools but not the agent.
- **Go**: rejected — goroutine stack overhead, larger binaries, weaker Windows-specific API support, less control over allocator for zeroization guarantees.
- **Zig**: rejected — ecosystem maturity insufficient for Phase 1.
