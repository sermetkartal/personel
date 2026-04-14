# Canary Release Stratejisi

Faz 16 #175 — Personel için aşamalı yayın stratejisi.

## Amaç

Yeni sürümleri tüm müşteri ortamına tek seferde **push etmemek**. Önce
küçük bir dilime, metric'leri izle, güven sağlandıkça genişlet. Hata
patlaması görülürse otomatik rollback tetiklenir (Faz 16 #176).

Canary, blue-green'den farklıdır:
- **Blue-green** (#174): iki ortam, anlık switch, %0 ↔ %100
- **Canary** (#175): tek ortam, kademeli yüzde, %5 → %25 → %50 → %100

Personel'de her iki yaklaşım da kullanılır. Blue-green deploy'un altında
canary routing, bir katman yukarıda çalışır.

## Aşamalar

```
Tag atıldı (v1.5.0)
        │
        ▼
┌───────────────┐
│  %5 canary    │  1 saat — seçili canary tenant cohort
│  auto-abort   │
└───────┬───────┘
        │
        ▼
┌───────────────┐
│  %25 canary   │  2 saat — pilot müşteri dahil
└───────┬───────┘
        │
        ▼
┌───────────────┐
│  %50 canary   │  4 saat — ana müşteri kohortunun yarısı
└───────┬───────┘
        │
        ▼
┌───────────────┐
│  %100 rollout │  Stabil — eski versiyon image'ları silinmez
└───────────────┘
```

Her aşama sonunda **sayısal eşikler** sağlanmadan sonrakine geçilmez.

## Yüzdeyi nasıl kontrol ediyoruz?

Canary yüzdesi iki mekanizma ile uygulanır:

### 1. Feature flag (Faz 16 #173)

`canary_release_routing` bayrağı, API router'ına "bu isteği canary'ye
yolla" bilgisi verir. `rollout_percentage` alanı aşama eşliğinde
güncellenir. Evaluator deterministiktir (SHA256 of tenant_id|user_id|key
mod 100), bu yüzden bir tenant bir kez canary kohortuna girdiğinde
aşama artınca dışında kalmaz.

### 2. Tenant-bazlı explicit cohort

`tenant_overrides` alanında bilinçli olarak pilot müşteri tenant ID'leri
`true` olarak işaretlenir. Bunlar yüzde ne olursa olsun **her zaman**
canary görür — en erken feedback kaynağı.

### Hangi seçim ne zaman?

| Senaryo | Seçim |
|---|---|
| İlk 5% — risk yüksek | Explicit `tenant_overrides = {pilot-tenant: true}`, `rollout_percentage=0` |
| 25% — pilot onayı alındı | `rollout_percentage=25` ek olarak |
| 50% — stabil görünüyor | `rollout_percentage=50` |
| 100% — full rollout | `rollout_percentage=100`, override'lar silinir |

## İzlenecek metrikler

Her aşamada şu Prometheus sorgularını kontrol et. Hepsi Grafana
"Personel / Canary" paneline bağlı olacak (Faz 13 #137).

### Hata oranı

```promql
# Canary vs baseline 5xx oranı
(
  sum(rate(personel_api_requests_total{status=~"5..",color="canary"}[5m]))
  /
  sum(rate(personel_api_requests_total{color="canary"}[5m]))
)
-
(
  sum(rate(personel_api_requests_total{status=~"5..",color="baseline"}[5m]))
  /
  sum(rate(personel_api_requests_total{color="baseline"}[5m]))
)
```

**Abort eşiği**: fark > %0.5

### p95 gecikme

```promql
histogram_quantile(0.95,
  sum by (le, color) (rate(personel_api_request_duration_seconds_bucket[5m]))
)
```

**Abort eşiği**: canary p95 > baseline p95 × 1.3

### UBA anomaly

UBA detector (Faz 8 #84) canary dönemde aktif. Canary'nin saatlik anomaly
oranı baseline'ın > %200'ü olursa abort.

### DLP red team

Keystroke admin-blindness testi her aşama öncesi otomatik koşar
(`apps/qa/cmd/audit-redteam`). Fail ise aşama geçmez.

### SLA breach

DSR (Faz 6 #69) 30 gün SLA takibinde canary döneminde **hiçbir** breach
kabul edilmez. Gözlem penceresinde tek bir overdue → auto-abort.

## Aşama ilerleme komutu

Operatör her aşamada şunu çalıştırır:

```bash
# Rollout yüzdesini ilerlet
curl -X PUT https://api.internal/v1/system/feature-flags/canary_release_routing \
  -H "Authorization: Bearer $ADMIN_JWT" \
  -d '{
    "key": "canary_release_routing",
    "description": "Canary routing for v1.5.0",
    "enabled": true,
    "default_value": false,
    "rollout_percentage": 25
  }'
```

Bu her PUT audit_log'a yazılır (`feature_flag.set`). `docs/operations/canary-release.md`'nin
hazır operatör scripti `infra/scripts/canary-advance.sh` (ilerleyen faz)
bu adımı otomatize eder.

## Abort kriterleri (otomatik)

Alert fire ettiğinde Alertmanager → webhook → `infra/scripts/rollback.sh`:

| Alert | Abort koşulu |
|---|---|
| `CanaryErrorRateHigh` | Canary 5xx oranı baseline'dan %0.5 fazla, 5 dk |
| `CanaryLatencyRegression` | p95 gecikme baseline × 1.3, 10 dk |
| `CanaryUBAAnomalySpike` | Saatlik anomaly canary'de 2x baseline |
| `CanaryDSRSLABreach` | En az bir DSR overdue canary döneminde |
| `CanaryRedTeamFail` | audit-redteam test FAIL |

Her abort:
1. `canary_release_routing` flag'i `enabled=false`'a döner
2. Feature flag audit entry yazılır
3. Alertmanager → PagerDuty
4. `release.rolled_back` audit action eklenir
5. İnsan onayı olmadan bir sonraki yayın denemesi bloke

## Örnek: "Yeni risk scoring algoritması" canary

**Context**: Faz 8 #86 risk scoring algoritması yeniden yazıldı. Eski
algoritma `risk_scoring_v1`, yeni `risk_scoring_v2`. Her ikisi de
paralel koşar; hangi sonucun canlıya dönüleceği feature flag ile
kontrol edilir.

### 1. Kod hazırlığı

```go
// apps/api/internal/scoring/handler.go
func ComputeRiskScore(ctx context.Context, u User) float64 {
    ec := featureflags.EvalContext{
        TenantID: u.TenantID,
        UserID:   u.ID,
        Role:     u.Role,
    }
    if ff.IsEnabled(ctx, "risk_scoring_v2", ec, false) {
        return computeV2(u)
    }
    return computeV1(u)
}
```

### 2. Yayın öncesi

- Tag at: `v1.5.0`
- `risk_scoring_v2` feature flag create:
  ```json
  {
    "key": "risk_scoring_v2",
    "enabled": true,
    "rollout_percentage": 0,
    "tenant_overrides": {"pilot-tenant-uuid": true}
  }
  ```

### 3. Aşama 1 (5% / 1 saat)

Yalnızca pilot müşteri v2 görür. `v1` vs `v2` skorlarının delta
dağılımı ClickHouse'tan sorgulanır:

```sql
SELECT
    percentile(0.95)(abs(v2 - v1)) AS p95_delta,
    count() AS sample
FROM risk_score_compare
WHERE ts > now() - INTERVAL 1 HOUR
```

p95 delta > 0.15 ise v2 agresif — abort.

### 4. Aşama 2-4

Saatler ilerledikçe `rollout_percentage` 25 → 50 → 100.

### 5. Temizlik

24 saat sonra `risk_scoring_v2` flag'i kaldırılır, kod `computeV1`
silinir. Sonraki release (patch bump).

## Per-tenant vs per-user canary

| Kriter | Per-tenant | Per-user |
|---|---|---|
| Deterministic rollout | ✅ bucket tenant_id'ye bağlı | ⚠️ aynı tenant içinde farklı deneyim |
| Audit izlenebilirlik | Her tenant net "v1 veya v2" | Aynı tenant'ta karışık |
| KVKK uyumluluğu | ✅ tenant-level isolation korunur | ⚠️ aynı tenant kullanıcıları farklı algoritma — açıklanabilir olmalı |
| Pilot müşteri feedback | ✅ "biz canary'deyiz" net | ❌ belirsiz |
| A/B testing | ❌ karşılaştırma yapılamaz | ✅ |
| Canary rollout (önerilen) | ✅ **Personel default** | Yalnızca A/B için |

**Personel varsayılanı: per-tenant canary.** Feature flag evaluator
bucket'ı `tenant_id|user_id|key` üzerinden hashliyor — `user_id=""` ile
çağrıldığında bucket sadece tenant_id'ye bağlı, böylece tenant-level
deterministik rollout elde edilir.

## KVKK notu

Canary release, veri işleme kapsamını (kategoriler, retention, amaç)
değiştirmedikçe aydınlatma metni güncellemesi gerektirmez. Yeni collector
veya yeni kullanım amacı varsa:

1. Canary **başlamadan** DPIA amendment
2. Aydınlatma metni güncelleme
3. Çalışan bildirim akışı (`docs/compliance/calisan-bilgilendirme-akisi.md`)
4. Ondan sonra canary aşama 1

## Değişiklik kaydı

| Tarih | Değişiklik | Yazan |
|---|---|---|
| 2026-04-13 | İlk versiyon — Faz 16 #175 | devops-engineer |
