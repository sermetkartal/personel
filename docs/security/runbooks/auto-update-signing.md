# Runbook — Signed Auto-Update Chain

> Language: English. Audience: devops-engineer, rust-engineer, release engineer. Scope: signing, distribution, verification, and rollback of the Rust agent binary. Companion to `docs/security/anti-tamper.md` §3 and `docs/architecture/agent-module-architecture.md` §Updater.

## 1. Goals

1. An update cannot be pushed to endpoints without at least two independent key holders cooperating.
2. A compromise of the OS code-signing certificate alone cannot push an arbitrary update.
3. An update cannot roll an agent backward to a known-vulnerable version.
4. A bad update must be detected automatically within 5 minutes on any endpoint that takes it, and rolled back.
5. Canary rollout bounds the blast radius of a latent regression.

## 2. Signing Chain

Each release artifact is signed **twice**:

### 2.1 OS code signing (outer signature)

- Purpose: lets Windows SmartScreen, Defender, and enterprise allowlists treat the binary as trusted; avoids SmartScreen friction during MSI install.
- Key type: RSA-3072 or ECDSA P-256 as required by the chosen commercial CA.
- CA options considered:
  - **Sectigo EV Code Signing**: ~€450/year, standard. Chosen for MVP.
  - **DigiCert EV**: ~€600/year, comparable trust.
  - **Turkish CA (TürkTrust / e-Güven)**: domestic option; mixed interop with Windows trust store for code signing outside TR. Rejected for MVP on compatibility grounds; revisit if a customer contract mandates it.
  - **Self-signed distributed via Group Policy**: zero recurring cost, requires customer IT to deploy the publisher cert via AD GPO, removes SmartScreen reputation entirely. This is a supported fallback mode for air-gapped customers who cannot receive a commercially signed update channel. Documented in §9.
- Storage: the code-signing key is held in a Yubikey FIPS HSM (required by Sectigo EV). Physical key is in the security engineer's safe. Signing requires physical presence.

### 2.2 Project signing (inner signature)

- Purpose: defense in depth. Even if an attacker compromises the code-signing CA chain or the EV Yubikey, they still cannot push an update the agent will accept, because the agent independently verifies this second signature against a key stored in Vault and delivered via the signed control-plane channel.
- Key type: **Ed25519**. Chosen for small signature size, fast verify, and strong offline-verification story.
- Storage: held in Vault transit engine at `transit/keys/project/release-signing` with `exportable=false`, `allow_plaintext_backup=false`. Signing is performed by calling `transit/sign/project/release-signing/sha256` — the key never leaves Vault.
- Signing requires a quorum: 2 of 3 release approvers hold individual tokens scoped to the `release-signer` Vault policy. Signing a release calls a custom script that requires both tokens to sign the same hash within a 5-minute window; if either does not sign, the release aborts.

Both signatures cover the same artifact: the full MSI file SHA-256. Agents verify both.

## 3. Release Pipeline

```
[git tag vX.Y.Z]
  → CI builds reproducibly on Windows runner (x86_64-pc-windows-msvc)
  → Unit + integration tests gate
  → cargo-audit, cargo-deny, cargo-vet checks gate
  → SBOM generated (CycloneDX)
  → Artifact: personel-agent-X.Y.Z.msi
  → SHA-256 computed
  → Release approval 1 signs hash via Vault
  → Release approval 2 signs hash via Vault
  → Inner Ed25519 signature attached
  → Outer EV code sign via Yubikey (manual step)
  → Manifest produced (see §4)
  → Uploaded to Update Service MinIO bucket `agent-releases/`
  → Canary rollout kicked off (manual trigger with canary=1%)
```

## 4. Manifest Format

Published at `https://update.internal/releases/manifest/v1/latest.json`, refreshed per release:

```json
{
  "schema_version": 1,
  "artifact": {
    "version": "1.4.2",
    "build_id": "1.4.2+git.abc123",
    "sha256": "9a3f...c1d2",
    "size_bytes": 18345216,
    "url": "releases/1.4.2/personel-agent-1.4.2.msi"
  },
  "constraints": {
    "minimum_previous_version": "1.3.0",
    "not_before": "2026-01-15T00:00:00Z",
    "not_after": "2026-07-15T00:00:00Z"
  },
  "rollout": {
    "canary_weight_bps": 100,
    "cohort_seed": "1.4.2-seed",
    "health_gate": {
      "max_crash_rate_bps": 50,
      "min_observation_seconds": 900
    }
  },
  "signatures": {
    "project_signing": {
      "key_id": "release-signing-2026",
      "algorithm": "ed25519",
      "signature": "base64(sig over canonical json of {artifact,constraints,rollout,manifest_metadata})"
    },
    "os_code_signing": {
      "embedded_in_msi": true,
      "authenticode_publisher": "Personel Teknoloji A.Ş."
    }
  },
  "manifest_metadata": {
    "manifest_id": "mfst-01HNXYZ...",
    "issued_at": "2026-02-10T08:23:11Z",
    "issuer": "release-bot@personel.local"
  }
}
```

The Ed25519 signature covers the canonical JSON encoding (keys sorted, no whitespace) of `{artifact, constraints, rollout, manifest_metadata}`. The signature field itself is not part of the signed payload.

## 5. Agent Verification Sequence

On `ServerMessage.UpdateNotify` or periodic poll:

1. Fetch manifest over mTLS from `update.internal`. Agent cert mTLS authenticates the channel.
2. Parse JSON. Reject manifests with unknown `schema_version`.
3. Verify `project_signing.signature` against the Ed25519 public key bundle baked into the current agent binary. The bundle contains the current key plus the previous key (§8), both trusted.
4. Verify `not_before <= now <= not_after`.
5. Verify `artifact.version > current_version` (monotonicity).
6. Verify `current_version >= constraints.minimum_previous_version` (cannot skip from a version older than the floor).
7. Compute `cohort = HMAC_SHA256(cohort_seed, endpoint_id) mod 10000`. If `cohort >= rollout.canary_weight_bps`, skip this manifest and report `update.deferred_canary`.
8. Download artifact to `C:\ProgramData\Personel\update\pending\personel-agent-<version>.msi`. Resume supported via HTTP Range.
9. Verify downloaded file SHA-256 matches `artifact.sha256`.
10. Verify Authenticode signature via `WinVerifyTrust` (this is the outer EV signature). Reject if publisher CN does not match `Personel Teknoloji A.Ş.` and the chain does not root to a trusted EV code-signing CA.
11. Emit `UpdateReady { artifact_path, signature }` over the watchdog IPC.
12. Watchdog stops main agent, atomically swaps the binary with `MoveFileEx(MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)`, restarts main agent, waits for heartbeat.
13. Main agent reports `agent.update_installed` with the new version.

Any step 3–10 failure emits `agent.tamper_detected { check_name="update_signature_invalid" }` and the update is refused.

## 6. Rollback Protocol

The watchdog is the rollback authority:

1. After swap, watchdog retains the previous binary at `C:\ProgramData\Personel\update\previous\personel-agent-<prev_version>.msi` for 24 hours.
2. Watchdog starts the new agent and observes a 5-minute settle window.
3. Triggers for automatic rollback during the settle window:
   - Main agent crashes 3 or more times.
   - Main agent fails to connect to gateway within 3 minutes after start.
   - Main agent emits `agent.health_heartbeat` with missing required fields.
4. On rollback trigger: watchdog stops main, `MoveFileEx` restores the previous binary, starts main, emits `agent.update_rolled_back` with the crash signature.
5. If rollback itself fails: watchdog escalates to `agent.update_rollback_failed`, keeps the watchdog heartbeat alive, and the endpoint stays on the broken version until human intervention. The event is priority 0 (critical) and the alerting pipeline surfaces it within 1 minute.

Honest limit: a bad update that subtly corrupts the local SQLite queue but does not crash within 5 minutes can evade automatic rollback. This is mitigated by (a) CI integration tests against a SQLite queue fixture, (b) per-canary cohort health gates at the server side which look at queue health and evictions before widening the cohort.

## 7. Canary Rollout

Server-side control of canary percentage is enforced by the Update Service.

| Stage | Canary weight | Minimum observation | Health gate |
|---|---|---|---|
| 1 | 1% (100 bps) | 1 hour | 0 rollbacks, <0.5% crash rate |
| 2 | 10% (1000 bps) | 2 hours | <0.5% crash rate, <1% update failures |
| 3 | 50% (5000 bps) | 4 hours | Same |
| 4 | 100% (10000 bps) | — | — |

Advancement is **manually triggered** by a release engineer based on the health dashboard; auto-advance is tempting but rejected for Phase 1 because the pilot fleet is only 500 endpoints and the statistical signal is too weak for confident automated decisions.

An **abort switch** is always available: publishing a manifest with `canary_weight_bps: 0` and `artifact.version` equal to the previously-stable version (i.e., a new manifest that pins everyone back) triggers a coordinated rollback. Combined with the monotonicity rule (§5.6), this requires a temporary override: the server can publish a `rollback_manifest_v1` variant signed by the same key but with an explicit `allow_downgrade_from: ["1.4.2"]` field. Agents verify the override's signature and origin, log the downgrade, and apply.

## 8. Project Signing Key Rotation

- Project signing key is rotated annually on a fixed calendar date (March 1).
- New key is generated in Vault before rotation; the new public key is added to the trust bundle in the next agent release.
- Old key is trusted for at least one major version after the rotation (typically 6 months), giving every endpoint a grace window to upgrade before the old signature stops verifying.
- Rotation emits `release.signing_key_rotated` audit event.
- Emergency rotation on compromise: follow `incident-response-playbook.md` §6.

## 9. Self-Signed Fallback Mode (Air-Gapped Customers)

Some customers cannot rely on commercial code-signing because they operate in isolated networks without CA revocation checks.

Mode "self-signed":
- The outer Authenticode signature is produced by a customer-managed EV or internal CA rooted in the customer's AD Enterprise Trust store.
- The project Ed25519 signature discipline is unchanged.
- The installer MSI must be distributed via GPO rather than public download.
- Customer is required to sign a contract addendum acknowledging the reduced external attestation; compliance-auditor produces the addendum template.

## 10. Honest Limits

- Watchdog can be terminated by a local administrator with `SeDebugPrivilege`. We cannot fully defend against that in Phase 1 user-mode; see `anti-tamper.md` §1.
- The EV code-signing HSM is physically in the Personel office. If an attacker physically steals it AND coerces the PIN, they have the outer signature. The inner Ed25519 signature stops arbitrary pushes provided the Vault release-signing key is uncompromised. If both are compromised, the attacker can push arbitrary updates — this is the ultimate supply-chain worst case and is addressed only by aggressive canary gates plus the `minimum_previous_version` rule limiting how far they can force an upgrade path.
- Reproducible builds are a stated goal but not yet fully guaranteed across MSVC toolchain versions. Until reproducibility is enforced, release comparison is limited to SHA-256 equivalence at the artifact level.

## 11. Handoffs

- **rust-engineer**: implements the agent verification sequence (§5), the watchdog rollback logic (§6), and the baked-in public key trust bundle (§8).
- **devops-engineer**: operates the CI pipeline, the Update Service, the MinIO bucket ACLs, and the canary dashboard.
- **release engineer (human role)**: owns the signing quorum, the EV Yubikey ritual, and the release approval process.
