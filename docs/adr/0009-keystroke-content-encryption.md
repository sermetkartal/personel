# ADR 0009 — Keystroke Content Encryption, Admin-Blind by Construction

**Status**: Accepted (Foundational). **Amended by ADR 0013 (2026-04-11)** — the DLP service is disabled by default in Phase 1; the cryptographic structure in this ADR is unchanged, but in the default operational state no process ever holds DSEK because no Vault Secret ID is issued for the `dlp-service` role until the customer completes the opt-in ceremony. See ADR 0013 and `docs/architecture/key-hierarchy.md` §Default vs Opted-In Runtime Guarantees.

## Context

Keystroke content is the most sensitive data Personel touches. Admin curiosity, insider threat, and KVKK m.6 (özel nitelikli veri) concerns all make "admin can view raw keystrokes if they want" an unacceptable default. A policy-only control ("we promise admins won't look") is not defensible. We need a cryptographic architecture in which admin access is impossible without a deliberate, auditable, multi-party code+config change.

## Decision

Implement the key hierarchy in `docs/architecture/key-hierarchy.md`:

1. Tenant Master Key (TMK) lives in Vault transit engine, non-exportable.
2. DLP Service derives an encryption key (DSEK) via HKDF from TMK. Only the `dlp-service` Vault AppRole can derive.
3. Per-Endpoint DEKs (PE-DEKs) are generated at enrollment, wrapped with DSEK, stored in Postgres, and delivered once to the endpoint via sealed channel.
4. Keystroke content is AES-256-GCM encrypted at the endpoint with PE-DEK. Only ciphertext ever leaves the endpoint.
5. The **DLP Service** — running in an isolated process, ideally a separate host — is the only component that can decrypt. It emits only `dlp.match` metadata (rule id, redacted snippet, severity, counts).
6. Admin API has zero Vault policy granting TMK derive access and zero proto surface returning plaintext.
7. Retention expiry is enforced by destroying Vault TMK versions after all blobs wrapped under them expire, making data cryptographically unrecoverable.

## Consequences

- Admins cannot read raw keystrokes, by construction. A malicious admin would need Vault root access AND code changes — neither is trivially available and both are audited.
- The DLP Service becomes a critical trust boundary. It must be hardened, monitored, and ideally physically separated.
- Operating the key hierarchy adds one non-trivial component (Vault) to the on-prem stack — acceptable cost.
- DLP rules are limited to what regex/pattern matching can express; deep NLP on keystrokes is not possible without changing this ADR.
- Legal and sales messaging must be explicit: Personel is a UAM product that does not permit keystroke surveillance in the colloquial sense.
- Destruction via key deletion is a defensible "silme/yok etme" mechanism under KVKK m.7.

## Alternatives Considered

- **Store keystrokes in plaintext with RBAC**: rejected — violates the locked product decision; insider risk unacceptable.
- **Encrypt with a key the admin can unlock with a password**: rejected — admin still has access.
- **HSM-per-customer with hardware break-glass**: over-engineered for Phase 1; revisit if a regulated customer demands it.
- **Redact client-side and never transmit content**: considered; we chose encrypted-transmit-and-DLP-match to preserve DLP value. If a customer opts out of DLP, the collector can be configured to emit only `keystroke.window_stats` and drop content entirely.
- **Store per-endpoint DEK only on the endpoint and re-encrypt on server via stream protocol**: adds round-trip cost; rejected.
