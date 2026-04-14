# Gizli Veri Rotasyon Prosedürü / Secret Rotation Procedure

> **Amaç**: Git history'de veya log'da açıklanmış bir secret keşfedildiğinde
> uygulanacak deterministik rotation runbook'u.
> **Dil**: Türkçe birincil.
> **Ref**: Faz 12 #128 (Gitleaks), `docs/security/runbooks/secret-rotation.md`
> (generic baseline), `docs/security/runbooks/vault-setup.md`.

---

## 1. Kapsam / Scope

Bu runbook şu senaryolarda çalıştırılır:

1. Gitleaks CI → `CRITICAL` finding
2. Developer local scan → secret committed
3. Third-party breach disclosure (Keycloak, Vault upstream CVE → reused secret)
4. Çalışan ayrılması + o çalışanın erişebildiği shared secret
5. Penetration test → secret leakage bulgusu

---

## 2. Hızlı Karar Ağacı / Quick Decision Tree

```
Secret sızdı mı? 
│
├── Evet → Hangi tip?
│         │
│         ├── Vault root token  → §3.1 (Vault root rekey)
│         ├── Vault seal Shamir  → §3.2 (Seal rekey)
│         ├── AppRole Secret ID  → §3.3 (Revoke + re-issue)
│         ├── Agent enroll token → §3.4 (Expire + list endpoints)
│         ├── Service account PW → §3.5 (DB/API password change)
│         ├── Ed25519 signing    → §3.6 (Transit key rotate)
│         ├── TLS private key    → §3.7 (Cert reissue + revoke)
│         └── OIDC client secret → §3.8 (Keycloak client rotation)
│
└── Hayır → Yanlış alarm. .gitleaks.toml allowlist güncelle + rationale.
```

---

## 3. Senaryolar / Scenarios

### 3.1 Vault Root Token

**Kritiklik**: CATASTROPHIC. Root token = tüm secret engine'lere sınırsız
erişim.

1. **Contain** (< 5 min):
   - Vault audit device'ı sorgula: `vault audit list` → compromised token
     ile yapılan her çağrıyı çıkar.
   - Compromised token'ı revoke et: `vault token revoke <token>`.
2. **Rekey**:
   - Generate-root ceremony başlat: `vault operator generate-root -init`.
   - 3-of-5 Shamir participant'larını topla (DPO + Security + Ops).
   - Yeni root token üret → emniyetli off-box safe'e yaz.
3. **Rotate downstream**:
   - Tüm AppRole secret_id'leri re-issue (`vault write -force auth/approle/role/*/secret-id`).
   - Tüm transit anahtarlarını rotate (`vault write -force transit/keys/*/rotate`).
4. **Audit**:
   - Incident ticket açılır (severity: CRITICAL, KVKK 72h clock starts).
   - Executive + DPO notification.
   - Post-mortem <72h, Evidence Locker CC7.3 payload.

### 3.2 Vault Seal Shamir Share

**Kritiklik**: HIGH. Tek share = hiçbir şey; 3 share = seal compromise ama
data encryption master henüz compromise değil (defense in depth).

1. **Re-key**: `vault operator rekey -init` → 3-of-5 ceremony ile yeni
   share seti.
2. **Rotate master**: `vault operator rotate` (data encryption key
   rotation; transparent to applications).
3. **Audit**: Hangi share sızdı? O share'ı tutan kişinin rolü gözden
   geçirilir. Ceremony participant değişikliği.

### 3.3 AppRole Secret ID

**Kritiklik**: Role-scope'lu. `dlp-service` AppRole için → ADR 0013
bozulması; `gateway-service` için → gateway sertifika imzalama yetkisi.

1. Secret ID'yi anında revoke: `vault write auth/approle/role/<name>/secret-id/destroy secret_id=<leaked>`.
2. Yeni Secret ID issue: `vault write -f auth/approle/role/<name>/secret-id`.
3. Deploy yeni secret'ı ilgili service config'ine (Vault-Agent sidecar
   veya environment file update + service restart).
4. **Özel durum — dlp-service AppRole Secret ID sızdı**:
   - Bu sızıntı, attacker'ın Vault transit `derive` çağırabileceği
     anlamına gelir → PE-DEK ifşası riski.
   - TMK rotation tetiklenir (§6.2 crypto review).
   - Tenant DPO bildirimi, 72h KVKK İhlali clock başlar.

### 3.4 Agent Enroll Token

**Kritiklik**: MEDIUM. Enroll token single-use + 15-min TTL; eski token
zaten invalid.

1. Token'ın zaman damgası hâlâ geçerli mi? (`decoded.issued_at + 15min > now`?)
2. Geçerli ise: `/v1/endpoints/enroll` → tüm active token'ları invalidate
   ederek (`POST /v1/endpoints/enroll/revoke-all`).
3. Son 15 dakikada enroll olan yeni endpoint'leri listele; unexpected
   olanları `endpoint.cert_revoked` ile revoke.

### 3.5 Service Account Password (DB / API)

**Kritiklik**: MEDIUM-HIGH depending on scope.

1. Vault KV'de ilgili entry'yi güncelle: `vault kv put secret/<service> password=<new>`.
2. Postgres: `ALTER ROLE <role> WITH PASSWORD '<new>';`
3. ClickHouse: `ALTER USER <user> IDENTIFIED BY '<new>';`
4. Service restart (rolling, from edge → core).
5. Audit: `system.password_rotated` entry + actor + reason code.

### 3.6 Ed25519 Signing Key (Control Plane / Policy / Release)

**Kritiklik**: HIGH. Past signatures'ın integrity'si soruya girer.

1. Vault transit key rotate: `vault write -force transit/keys/<name>/rotate`
   → new version v+1.
2. Re-sign all active artifacts (policy bundles, release manifests,
   audit checkpoints) with v+1.
3. Push new public key to agents via policy bundle update.
4. Verify: agents reject old signatures on next pull.
5. Historical signatures: still verifiable via Vault transit key history
   (old version retained for decryption — not deleted).

### 3.7 TLS Private Key

**Kritiklik**: HIGH for end-entity; CRITICAL for CA.

1. End-entity (leaf) cert:
   - Vault PKI revoke: `vault write pki/revoke serial_number=<serial>`.
   - CRL updated immediately; gateway refreshes < 5 min.
   - Reissue cert: `vault write pki/issue/<role> common_name=...`.
   - Service restart.
2. Intermediate CA key:
   - Full intermediate ceremony: revoke old intermediate, issue new from root.
   - Re-issue all downstream certs.
   - Cross-sign for grace period to avoid gateway outages.

### 3.8 OIDC Client Secret (Keycloak)

**Kritiklik**: MEDIUM.

1. Keycloak admin console → Clients → Select → Credentials → Regenerate.
2. Update service env var / config.
3. Service restart.
4. Existing sessions remain valid until token TTL (< 15 min typical);
   admin can force-logout: Session → Logout all.

---

## 4. Post-Rotation Verification / Doğrulama

Her rotation'dan sonra:

```bash
# 1. Gitleaks re-scan the git history
gitleaks detect --config .gitleaks.toml --log-level info

# 2. Smoke test — credential works
curl -sf https://gateway:9443/healthz     # if cert rotated
curl -sf http://api:8000/healthz -H "Authorization: Bearer $NEW_TOKEN"

# 3. Audit log chain still verifies
docker compose exec api /usr/local/bin/audit-verify --tenant $TID

# 4. Evidence Locker entry created
curl -sf http://api:8000/v1/system/evidence-coverage?period=$(date +%Y-%m) | jq
```

---

## 5. Communication / Bildirim

**Internal**:
- Incident channel (Slack #security-incidents — AWAITING: webhook)
- Ticket in internal tracker
- Executive + DPO + Legal email

**External** (if customer-affecting):
- KVKK İhlali Bildirimi 72-hour SLA starts at detection time.
- Customer DPO notification per DPA contractual terms.
- VERBİS güncellemesi gerekirse.

---

## 6. Post-Mortem / Lessons Learned

Her rotation için 5 gün içinde post-mortem yaz:

1. Timeline (detection → contain → rotate → verify)
2. Root cause (CI eksik tarama? bilinmeyen config path? developer workflow?)
3. Detection gap (niye Gitleaks / Trivy / audit yakalamadı?)
4. Action items (CI rule, code review checklist, training)
5. Evidence Locker entry (control CC7.3)

---

## 7. AWAITING

- [ ] Slack webhook URL for #security-incidents
- [ ] Paging rotation (PagerDuty or equivalent)
- [ ] Customer DPO notification template (per-customer)
- [ ] Legal counsel review of communication templates
