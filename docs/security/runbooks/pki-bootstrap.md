# Runbook — PKI Bootstrap

> Language: English. Audience: devops-engineer, security on-call. Scope: first-time stand-up of the Personel on-prem PKI for a single tenant. Companion to `docs/architecture/mtls-pki.md`.

## 0. Tooling Decision

**Chosen tool: `step-ca` (Smallstep) for ceremony + `vault pki` secrets engine for online operation.**

Rationale:
- `openssl` alone requires hand-rolled CSR templates and is error-prone for Ed25519 + name constraints.
- `cfssl` is unmaintained upstream (Cloudflare archived active development in 2022).
- `step-ca` produces reproducible ceremony output, supports Ed25519 natively, has a simple `step certificate create` CLI that is easy to audit line-by-line, and emits PEM compatible with Vault import.
- Vault PKI is the runtime issuer (already required by `mtls-pki.md`). `step-ca` only touches root material during the offline ceremony; we do not deploy `step-ca` as a running service.

## 1. Hardware and Environment

The root ceremony MUST be performed on an air-gapped machine. Minimum:
- A clean Ubuntu 22.04 LTS laptop, wiped, never networked after install.
- `step-cli` v0.25+ installed from the signed `.deb` on a known-good USB.
- Two FIPS 140-2 Level 3 USB HSMs (YubiHSM 2 recommended) — primary + backup. If the customer supplies a network HSM (Thales / Entrust), use PKCS#11 mode instead; see §8.
- Two sealed tamper-evident envelopes for Shamir share storage.
- Paper printouts for the root CA fingerprint (3 copies, notarized).

Ceremony witnesses: minimum 2 authorized personnel (security-engineer + a named customer security officer). Ceremony log is hand-written and signed.

## 2. Key Parameters

| Entity | Algorithm | Curve / Size | Validity | Justification |
|---|---|---|---|---|
| Root CA | ECDSA | P-384 | 10 years | P-384 is broadly supported on Windows Schannel and required by some Turkish customer security baselines that reject Ed25519 at the root level. |
| Tenant CA | ECDSA | P-256 | 3 years | Cross-compat with gateway (Go `crypto/tls` + rustls on agent). |
| Agent Intermediate | ECDSA | P-256 | 2 years | Same chain-validation path as tenant. |
| Server Intermediate | ECDSA | P-256 | 2 years | Same. |
| Agent Client Cert | **Ed25519** | — | **14 days** (short-lived; was 30d in architecture doc, we shorten — see §7) | Small, fast; rustls and tonic support it end-to-end. |
| Server TLS Cert | ECDSA | P-256 | 90 days | Caddy / reverse proxy needs ECDSA P-256; Ed25519 on servers still has spotty browser/Windows client support for admin console. |
| Control-plane signing key (policy, pin-update, live-view control) | Ed25519 | — | 1 year | Small signature, fast verify on agent. |
| Code signing / release signing | Ed25519 | — | 1 year | See `auto-update-signing.md`. |

**Deviation from architecture doc**: `mtls-pki.md` lists agent cert validity at 30 days. We ship with 14 days because rotation is automatic on the existing gRPC stream (see `ServerMessage.RotateCert`) and a shorter TTL tightens the revocation window. Reconsider if rotation causes operational noise in pilot.

## 3. Air-Gapped Root Ceremony

All commands below are executed on the air-gapped machine. No output leaves the machine except: (a) the root CA public certificate `root_ca.crt`, (b) the signed tenant CA certificate `tenant_ca.crt`, (c) the CRL distribution manifest, (d) the Shamir shares.

### 3.1 Initialize step-ca PKI (root only)

```bash
export STEPPATH=/mnt/ceremony/step
step ca init \
  --name "Personel Root CA" \
  --dns "root.pki.personel.local" \
  --address ":0" \
  --provisioner "ceremony@personel.local" \
  --password-file /dev/null \
  --no-db \
  --ssh=false \
  --deployment-type standalone \
  --key-type ec \
  --key-curve P-384
```

This produces:
- `$STEPPATH/certs/root_ca.crt`
- `$STEPPATH/secrets/root_ca_key` (ENCRYPTED — password prompt; use a 24-word diceware passphrase generated on the machine)

### 3.2 Split root key into Shamir shares

```bash
step crypto key format \
  --pkcs8 \
  --no-password \
  --insecure \
  $STEPPATH/secrets/root_ca_key > /tmp/root_ca_key.pem

ssss-split -t 3 -n 5 -w "personel-root" < /tmp/root_ca_key.pem > /tmp/shares.txt
shred -uz /tmp/root_ca_key.pem
```

Seal each share in its own tamper-evident envelope, label share-1..share-5, distribute:
- share-1, 2: customer security officer (one in bank SDB, one on-site safe)
- share-3: security-engineer team lead (off-site safe)
- share-4: customer DPO
- share-5: sealed envelope in fireproof safe at customer HQ

Threshold 3-of-5. We require two independent custodians to agree before the root is reassembled.

### 3.3 Wipe working key material

```bash
shred -uz /tmp/shares.txt
umount /mnt/ceremony
wipefs -a /dev/<usb_device>
```

The root CA key exists nowhere as a complete secret after this step.

### 3.4 Generate Tenant CA CSR on the online Vault host

On the **online** Vault host (see `vault-setup.md`), generate the CSR that the offline ceremony will sign. This CSR is transported to the air-gapped machine on a fresh USB.

```bash
vault secrets enable -path=pki/tenant/<tenant_id> pki
vault secrets tune -max-lease-ttl=26280h pki/tenant/<tenant_id>  # 3 years

vault write -format=json pki/tenant/<tenant_id>/intermediate/generate/internal \
  common_name="Personel Tenant CA <tenant_id>" \
  key_type=ec \
  key_bits=256 \
  ttl=26280h \
  | jq -r '.data.csr' > tenant_ca.csr
```

USB-transfer `tenant_ca.csr` to air-gapped host.

### 3.5 Sign tenant CSR with root

```bash
step certificate sign \
  --profile intermediate-ca \
  --not-after 26280h \
  tenant_ca.csr \
  $STEPPATH/certs/root_ca.crt \
  $STEPPATH/secrets/root_ca_key \
  > tenant_ca.crt
```

Enter the root passphrase (reassembled from 3 Shamir shares). After signing, the root key in memory is flushed by `step` (process exit). Shred the reassembled passphrase file.

USB-transport `tenant_ca.crt` + `root_ca.crt` back to the Vault host.

### 3.6 Import signed tenant CA into Vault

```bash
vault write pki/tenant/<tenant_id>/intermediate/set-signed \
  certificate=@tenant_ca.crt

vault write pki/tenant/<tenant_id>/config/urls \
  issuing_certificates="https://vault.internal:8200/v1/pki/tenant/<tenant_id>/ca" \
  crl_distribution_points="https://vault.internal:8200/v1/pki/tenant/<tenant_id>/crl"
```

## 4. Agent and Server Intermediate CAs

Both intermediates are signed **online** by the tenant CA. They do not require the air-gapped ceremony.

```bash
# Agent intermediate
vault secrets enable -path=pki/tenant/<tenant_id>/agents pki
vault secrets tune -max-lease-ttl=17520h pki/tenant/<tenant_id>/agents  # 2 years
# (repeat the intermediate/generate → sign → set-signed dance against tenant CA)

# Server intermediate
vault secrets enable -path=pki/tenant/<tenant_id>/servers pki
vault secrets tune -max-lease-ttl=17520h pki/tenant/<tenant_id>/servers
```

### 4.1 Agent issuance role

```bash
vault write pki/tenant/<tenant_id>/agents/roles/endpoint \
  allowed_domains="endpoints.<tenant_id>.personel.local" \
  allow_subdomains=true \
  allow_bare_domains=false \
  allow_glob_domains=false \
  enforce_hostnames=false \
  client_flag=true \
  server_flag=false \
  key_type=ed25519 \
  max_ttl=336h \
  ttl=336h \
  no_store=true \
  require_cn=true \
  ou="agents" \
  organization="Personel" \
  country="TR"
```

### 4.2 Server issuance role

```bash
vault write pki/tenant/<tenant_id>/servers/roles/gateway \
  allowed_domains="gateway.<tenant_id>.personel.local" \
  allow_subdomains=false \
  client_flag=false \
  server_flag=true \
  key_type=ec \
  key_bits=256 \
  max_ttl=2160h \
  ttl=2160h \
  ou="servers" \
  organization="Personel" \
  country="TR"
```

Repeat role definitions per server identity: `admin-api`, `live-view`, `update-service`, `dlp-service`, `reverse-proxy`.

## 5. Gateway Server Cert Issuance

```bash
VAULT_TOKEN=<gateway-service-token> vault write -format=json \
  pki/tenant/<tenant_id>/servers/issue/gateway \
  common_name="gateway.<tenant_id>.personel.local" \
  ttl=2160h \
  | tee /etc/personel/tls/gateway.json

jq -r .data.certificate /etc/personel/tls/gateway.json > /etc/personel/tls/gateway.crt
jq -r .data.private_key /etc/personel/tls/gateway.json > /etc/personel/tls/gateway.key
jq -r '.data.ca_chain[]' /etc/personel/tls/gateway.json > /etc/personel/tls/chain.pem
chmod 0400 /etc/personel/tls/gateway.key
chown personel-gateway:personel-gateway /etc/personel/tls/gateway.*
shred -uz /etc/personel/tls/gateway.json
```

Automated on renewal by `vault-agent` sidecar driven from `personel-vault-agent.service`.

## 6. Agent Enrollment Bootstrap Token

Enrollment uses a short-lived Vault AppRole Secret ID delivered out-of-band to the MSI installer. Agent enrollment is a two-step ritual: the Admin API owns the AppRole login; the installer never talks to Vault directly.

```bash
vault write auth/approle/role/agent-enrollment \
  token_ttl=15m \
  token_max_ttl=15m \
  secret_id_ttl=15m \
  secret_id_num_uses=1 \
  token_policies="agent-enrollment" \
  bind_secret_id=true

ROLE_ID=$(vault read -field=role_id auth/approle/role/agent-enrollment/role-id)

# Admin API calls this per enrollment:
SECRET_ID=$(vault write -f -field=secret_id auth/approle/role/agent-enrollment/secret-id)

# Token handed to installer as opaque base64:
printf '%s:%s' "$ROLE_ID" "$SECRET_ID" | base64
```

Installer calls `POST /v1/enroll` with this token plus its CSR and hardware fingerprint. Admin API exchanges the AppRole for a 15-minute Vault token, calls `pki/tenant/<tenant_id>/agents/sign-verbatim`, and returns the signed cert inline. AppRole Secret ID is single-use; it cannot be replayed.

## 7. Rotation Schedule

| Entity | Validity | Alert at T- | Action |
|---|---|---|---|
| Root CA | 10 years | 24 months | Schedule ceremony; overlap-sign new root under old for 12 months |
| Tenant CA | 3 years | 6 months | Repeat §3.4–§3.6 with new CSR |
| Agent/Server Intermediates | 2 years | 6 months | Automated Vault renewal; staged with 30-day overlap |
| Agent Client Cert | 14 days | 3 days | Automated on-stream `RotateCert` |
| Server TLS Cert | 90 days | 14 days | `vault-agent` template renewal + service SIGHUP |
| Control-plane signing key | 1 year | 60 days | Manual rotation with grace overlap; see `auto-update-signing.md` §4 |

## 8. HSM Fallback Path

If the customer supplies a network HSM with PKCS#11:

1. Replace §3.1 with `step ca init --kms "pkcs11:..."`.
2. The root key never leaves the HSM; Shamir is unnecessary for key recovery (HSM handles that).
3. Document the HSM serial number, slot, and PIN custodianship in the ceremony log.
4. Vault tenant CA continues to use its internal key (we do NOT try to back the online CA with the customer HSM in Phase 1 — added operational complexity, deferred to Phase 2).

## 9. Revocation Policy

Prefer short-lived certificates over CRL/OCSP. Operating rules:

1. **Agent cert compromise suspected**: set the cert serial on the gateway deny-list (NATS `pki.v1.revoke` subject); rotate the endpoint's cert on next connect. The 14-day TTL caps the worst-case offline window.
2. **Server cert compromise**: issue a replacement, deploy via `vault-agent`, then publish revocation to CRL. Short 90-day TTL caps the window if CRL delivery fails.
3. **Intermediate compromise**: ceremony to re-sign a new intermediate under tenant CA; revoke old intermediate via CRL; force agent cert rotation cluster-wide (triggers a ~30 minute rekey storm for 500 endpoints; budget for it).
4. **Tenant CA compromise**: full PKI rebuild. Disclose to customer per §10 of `incident-response-playbook.md`.

CRL is published hourly at `https://vault.internal:8200/v1/pki/tenant/<tenant_id>/crl` as a fallback for clients that cannot rely on short TTLs (mainly audit reviewers).

## 10. Operational Verification

After initial bootstrap, run the verification script (to be authored by devops-engineer):

```bash
personel-pki-verify \
  --tenant <tenant_id> \
  --expect-root-sha256 <paper_printed_fingerprint> \
  --expect-tenant-sha256 <from_ceremony_log> \
  --gateway gateway.internal:443 \
  --enroll-dry-run
```

The script MUST:
- Re-fetch the root CA from Vault and compare SHA-256 to the paper printout.
- Validate the tenant CA chain.
- Request a test agent cert via a dry-run enrollment token.
- Assert the issued cert is Ed25519, TTL ≤ 14 days, SAN matches expected template.
- Fail the bootstrap gate if any assertion fails.

## 11. Files Produced

- `/etc/personel/tls/root_ca.crt` — public root, readable by all Personel services.
- `/etc/personel/tls/tenant_ca.crt` — public tenant CA.
- `/etc/personel/tls/<service>.{crt,key,chain.pem}` — per-service TLS material, renewed automatically.
- `docs/runbooks/ceremony-log-<date>.pdf` — signed ceremony log, stored with compliance-auditor.
