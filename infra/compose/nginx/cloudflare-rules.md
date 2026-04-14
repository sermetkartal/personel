# Cloudflare WAF Alternatif Konfigürasyonu

> **Amaç**: `nginx.conf` tarafında scaffold'lanan kuralların Cloudflare
> WAF ile equivalently nasıl uygulanacağını dokümante et.
> **Dil**: Türkçe birincil.
> **Ref**: Faz 12 #132

---

## 1. Neden Cloudflare?

`nginx.conf` self-hosted, tam kontrollü ama maintenance yükü getirir.
Cloudflare WAF üç avantajı:

1. **Managed Rule Sets**: OWASP CRS, Cloudflare Managed Rules otomatik
   güncellenir.
2. **Anomaly scoring**: Tek regex yerine pattern birleşimi skorlanır.
3. **Bot detection**: Advanced ML-based bot scoring (Pro+ plan).

Dezavantaj: Dışa bağımlılık + latency + customer data transit edge POP'lardan.
Personel için on-prem deployment ile uyumlu kullanım: **yalnızca admin UI
için** (agent trafiği direkt gateway'e).

---

## 2. Account kurulum

**AWAITING**:
- [ ] Cloudflare account (Free tier başlangıç OK)
- [ ] Domain transfer veya DNS records point edilmesi
- [ ] Origin CA cert üretimi (Cloudflare origin → customer nginx)

---

## 3. Firewall Rules / Custom Rules

Cloudflare dashboard → Security → WAF → Custom rules → Create rule:

### Kural 1 — Agent enroll rate limit

```
Rule name:       Personel — agent enroll strict
Field:           URI Path
Operator:        equals
Value:           /v1/agent-enroll
Action:          Rate limit
Rate limit:      10 requests / 1 minute
                 per IP address
```

### Kural 2 — Wipe endpoint audit + throttle

```
Rule name:       Personel — endpoint wipe
Field:           URI Path
Operator:        matches regex
Value:           ^/v1/endpoints/[^/]+/wipe$
Action:          Log + Rate limit
Rate limit:      3 requests / 1 minute
                 per IP address
```

### Kural 3 — Pipeline replay IP allowlist

```
Rule name:       Personel — pipeline replay DPO only
Field:           URI Path equals /v1/pipeline/replay
And:             IP Source Address NOT in {ADMIN_VPN_CIDR}
Action:          Block
```

### Kural 4 — Bulk ops one-per-minute

```
Rule name:       Personel — bulk endpoint ops
Field:           URI Path equals /v1/endpoints/bulk
Action:          Rate limit
Rate limit:      1 request / 1 minute
                 per IP address + per Authorization header
```

---

## 4. Managed Rule Sets

Security → WAF → Managed rules → Deploy:

- ✅ Cloudflare Managed Ruleset (Paranoia Level 2)
- ✅ OWASP Core Rule Set (Anomaly Threshold 5)
- ✅ Cloudflare Exposed Credentials Check
- ❌ Cloudflare Leaked Credentials (requires Pro, optional)

False positive için: Exceptions tab → add rule exception by path.

---

## 5. Bot Fight Mode

Security → Bots:

- ✅ Bot Fight Mode (Free tier)
- ⚠️ Super Bot Fight Mode (Pro+ plan, optional)

Whitelist: GitHub Actions IPs (for CI health checks) + known employee VPN.

---

## 6. Origin Ayarları

1. Cloudflare SSL/TLS mode: **Full (Strict)** — self-signed origin cert ile.
2. Origin Rules → Rewrite Host Header if necessary.
3. Cloudflare Origin CA cert oluştur ve nginx origin config'ine yükle.
4. Authenticated Origin Pulls: customer mTLS bridge için opsiyonel.

---

## 7. Logging

Cloudflare Logpush → S3/MinIO veya Splunk:

```json
{
  "ClientRequestURI": "/v1/agent-enroll",
  "ClientIP": "203.0.113.1",
  "WAFAction": "block",
  "WAFRuleID": "100003",
  "RayID": "..."
}
```

Personel audit log'una sync edilmez (farklı trust domain), ancak incident
investigation için saklanır.

---

## 8. Gap vs nginx.conf

| Feature | nginx | Cloudflare |
|---|---|---|
| SQLi regex block | ✅ | ✅ (Managed + CRS) |
| XSS regex block | ✅ | ✅ |
| Rate limit per-IP | ✅ | ✅ (better) |
| Rate limit per-token | ✅ | ❌ (per-IP only) |
| Bot scoring | ❌ | ✅ (ML-based) |
| DDoS absorption | ❌ | ✅ (Anycast) |
| Audit log PII handling | Self-control | Edge POP processes briefly |

**Karar**: Hibrit. Cloudflare agent cluster için **kullanılmaz** (direct
mTLS preserved). Console + portal için opsiyonel customer-choice.

---

## 9. Referanslar

- https://developers.cloudflare.com/waf/
- https://coreruleset.org/ (OWASP CRS)
- `infra/compose/nginx/nginx.conf` (#132 self-hosted alt.)
