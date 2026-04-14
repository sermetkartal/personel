# Personel API — curl Cookbook

> Dil: Türkçe + curl. 15 yaygın iş akışı örneği. Her örnek `$JWT`, `$API`, `$TENANT_ID` değişkenlerinin set edildiğini varsayar.

```bash
# Set environment
export API=https://api.personel.musteri.local
export TENANT_ID=be459dac-1a79-4054-b6e1-fa934a927315

# Keycloak'tan JWT al
export JWT=$(curl -sk -X POST \
  https://auth.personel.musteri.local/realms/personel/protocol/openid-connect/token \
  -d "grant_type=password" \
  -d "client_id=personel-api" \
  -d "client_secret=api-secret-dev" \
  -d "username=admin" \
  -d "password=admin123" \
  | jq -r .access_token)
```

---

## 1. Sağlık kontrolü

```bash
curl -sk $API/healthz | jq
```

## 2. Uç nokta listesi (ilk 50)

```bash
curl -sk "$API/v1/endpoints?limit=50&status=active" \
  -H "Authorization: Bearer $JWT" | jq '.items[] | {id, asset_tag, status, last_seen_at}'
```

## 3. Yeni ajan enroll token üret

```bash
curl -sk -X POST $API/v1/endpoints/enroll \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "'$TENANT_ID'",
    "asset_tag": "LAPTOP-042",
    "user_hint": "ahmet.yilmaz@musteri.local"
  }' | jq
```

## 4. Uç nokta uzaktan wipe (DSR erasure)

```bash
curl -sk -X POST $API/v1/endpoints/<id>/wipe \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{
    "reason_code": "dsr_erasure",
    "ticket_id": "DSR-2026-0042",
    "justification": "KVKK m.11/e gereği çalışan silme talebi onaylandı, DPO kararı DPR-CASE-0042."
  }'
```

## 5. Toplu deaktivasyon (500 uç nokta limit)

```bash
curl -sk -X POST $API/v1/endpoints/bulk \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{
    "operation": "deactivate",
    "endpoint_ids": ["uuid-1", "uuid-2", "uuid-3"],
    "reason_code": "offboarding_batch"
  }'
```

## 6. Politika oluştur + imzalı push

```bash
# 1. Create
POLICY_ID=$(curl -sk -X POST $API/v1/policies \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Default Work Policy",
    "rules": {
      "screenshot_interval_seconds": 60,
      "screenshot_exclude_apps": ["1Password", "KeePass", "mstsc.exe"],
      "usb_mass_storage_policy": "block",
      "keystroke_content_enabled": false
    }
  }' | jq -r .id)

# 2. Sign + push
curl -sk -X POST $API/v1/policies/$POLICY_ID/push \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{"broadcast": true}'
```

## 7. Canlı izleme isteği + HR onayı akışı

```bash
# 1. Requester (investigator rolü) istek açar
REQ_ID=$(curl -sk -X POST $API/v1/liveview/requests \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{
    "endpoint_id": "uuid",
    "reason_code": "policy_violation",
    "justification": "Ticket INV-2026-0042: şüpheli USB aktivitesi, kanıt toplama gerekli.",
    "duration_seconds": 900
  }' | jq -r .id)

# 2. HR (farklı kullanıcı!) onaylar
curl -sk -X POST $API/v1/liveview/requests/$REQ_ID/approve \
  -H "Authorization: Bearer $JWT_HR"
# → Yanıt: {"livekit_token":"...", "room":"lv-...", "expires_at":"..."}

# 3. Console LiveKit SDK ile room'a katılır
```

## 8. DSR başvurusu ve erişim talebi karşılama

```bash
# Employee portal'dan açılır (veya DPO manuel)
DSR_ID=$(curl -sk -X POST $API/v1/dsr \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{
    "request_type": "access",
    "subject_email": "ahmet.yilmaz@musteri.local",
    "justification": "KVKK m.11/b - hangi veriler işleniyor bilgisi"
  }' | jq -r .id)

# DPO karşılar (30 gün SLA içinde)
curl -sk -X POST $API/v1/dsr/$DSR_ID/fulfill-access \
  -H "Authorization: Bearer $JWT_DPO"
# → export_url + expires_at (24h presigned)
```

## 9. Silme (erasure) talebi karşılama

```bash
curl -sk -X POST $API/v1/dsr/$DSR_ID/fulfill-erasure \
  -H "Authorization: Bearer $JWT_DPO" \
  -H "Content-Type: application/json" \
  -d '{
    "scope": "full",
    "retention_exemptions": ["audit_log", "legal_hold_active"]
  }'
```

## 10. Audit log full-text arama

```bash
curl -sk -G $API/v1/search/audit \
  -H "Authorization: Bearer $JWT" \
  --data-urlencode "q=policy_push" \
  --data-urlencode "from=2026-04-01T00:00:00Z" \
  --data-urlencode "to=2026-04-13T23:59:59Z" \
  --data-urlencode "actor=admin@musteri.local" | jq
```

## 11. En çok kullanılan uygulamalar (ClickHouse raporu)

```bash
curl -sk -G $API/v1/reports/ch/top-apps \
  -H "Authorization: Bearer $JWT" \
  --data-urlencode "from=2026-04-01T00:00:00Z" \
  --data-urlencode "to=2026-04-13T23:59:59Z" \
  --data-urlencode "limit=20" | jq
```

## 12. Trend raporu (haftalık)

```bash
curl -sk -G $API/v1/reports/trends \
  -H "Authorization: Bearer $JWT" \
  --data-urlencode "metric=activity" \
  --data-urlencode "period=weekly" | jq
```

## 13. Rapor export (Excel)

```bash
curl -sk -X POST $API/v1/reports/export \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{
    "format": "xlsx",
    "report_type": "top_apps",
    "filters": {"from": "2026-04-01", "to": "2026-04-13"}
  }'
```

## 14. SOC 2 evidence coverage

```bash
# Coverage matrix
curl -sk "$API/v1/system/evidence-coverage?period=2026-04" \
  -H "Authorization: Bearer $JWT_DPO" | jq

# Signed ZIP pack (DPO only)
curl -sk "$API/v1/dpo/evidence-packs?period=2026-04" \
  -H "Authorization: Bearer $JWT_DPO" \
  -o evidence-pack-2026-04.zip
```

## 15. Canlı audit stream (WebSocket)

```bash
# wscat ile
wscat -c "wss://api.personel.musteri.local/v1/audit/stream?actor=admin@musteri.local" \
  -H "Authorization: Bearer $JWT"
# Her frame = bir audit_log satırı JSON
```

---

## Hata Cevabı Şekli (RFC 7807)

Tüm 4xx/5xx cevapları `application/problem+json`:

```json
{
  "type": "https://api.personel.local/problems/validation-error",
  "title": "Validation Error",
  "status": 400,
  "detail": "field 'reason_code' required",
  "instance": "/v1/endpoints/xxx/wipe",
  "request_id": "req_01HXYZ..."
}
```

## Rate Limit Header'ları

```
X-RateLimit-Limit: 1000
X-RateLimit-Remaining: 847
X-RateLimit-Reset: 1713999600
Retry-After: 30     (sadece 429 cevaplarında)
```

---

## Versiyon

| Sürüm | Tarih | Değişiklik |
|---|---|---|
| 1.0 | 2026-04-13 | Faz 15 #160 — İlk sürüm |
