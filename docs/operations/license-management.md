# Personel — Lisans Yönetimi (TR)

Bu doküman Personel Platform lisansının yaşam döngüsünü, online/offline
doğrulama akışını ve sık karşılaşılan lisans operasyonlarını açıklar.

**Hedef kitle**: Personel DevOps ekibi + müşteri IT müdürü.

---

## Lisans Dosyası Anatomisi

Lisans dosyası `/etc/personel/license.json` dosyasında yaşar. İçeriği:

```json
{
  "claims": {
    "customer_id": "acme-corp-tr",
    "tier": "business",
    "max_endpoints": 100,
    "features": ["hris", "ocr", "uba"],
    "issued_at": "2026-01-15T09:00:00.000000000Z",
    "expires_at": "2027-01-15T09:00:00.000000000Z",
    "fingerprint": "a1b2...",
    "online_validation": false
  },
  "signature": "base64(ed25519(canonical(claims)))",
  "key_id": "personel-vendor-2026"
}
```

**Dosya izinleri**: `chmod 0600 /etc/personel/license.json`, sahibi
`personel:personel`.

### Claims alanları

| Alan | Anlam | Zorunlu mu |
|---|---|---|
| `customer_id` | Müşteri kısa kodu | evet |
| `tier` | `trial` / `starter` / `business` / `enterprise` | evet |
| `max_endpoints` | Azami enroll endpoint sayısı | evet |
| `features` | Aktif eklentiler (alfabetik sıralı) | evet |
| `issued_at` | Üretim zamanı (UTC RFC3339) | evet |
| `expires_at` | Geçerlilik sonu (UTC RFC3339) | evet |
| `fingerprint` | Donanım parmak izi SHA-256 hex | hayır |
| `online_validation` | Phone-home etkin mi | evet |

### İmza

Ed25519 imzası, claims alanının canonical (sıralı anahtar, sıkıştırılmış
JSON) formuna uygulanır. Public key API binary'sine compile edilmiştir —
harici servis çağrısı gerektirmez.

---

## Yaşam Döngüsü

```
                       ┌──────────┐
                       │  Valid   │  (yeşil)
                       └─────┬────┘
                 expires_at   │
                             ▼
                       ┌──────────┐
                       │  Grace   │  (sarı, read-only 7 gün)
                       └─────┬────┘
                expires+7gün  │
                             ▼
                       ┌──────────┐
                       │ Expired  │  (kırmızı, 403)
                       └──────────┘
```

### Valid state

- Tüm API yazma işlemleri kabul edilir
- `/v1/system/license` → `state: "valid"`
- Prometheus metric: `personel_license_state{state="valid"} 1`

### Grace state

- Yazma işlemleri **403** döner: `err.license.grace_readonly`
- Okuma işlemleri çalışmaya devam eder
- Admin Console "Lisansınız expired" banner'ı gösterir
- Amaç: operator'a yenileme için zaman tanıma, aniden kapatmama

### Expired state

- Tüm `/v1/*` rotaları **403** döner: `err.license.expired`
- `/healthz`, `/readyz`, `/public/status.json` açık kalır (operator
  sistemin hala ayakta olduğunu görebilsin)
- Agent enrollment reddedilir
- Mevcut enrolled agent'lar event yollamaya devam eder (veri kaybı yok)

---

## Online Doğrulama (Opsiyonel)

Lisans `online_validation: true` içerirse API 24 saatte bir
`POST <online_validation_url>` çağrısı yapar.

### Payload (gizlilik dostu)

```json
{
  "customer_id": "acme-corp-tr",
  "endpoint_count": 87,
  "version": "0.9.0",
  "license_fingerprint": "a1b2..."
}
```

**İçermez**: Kullanıcı verisi, event içeriği, IP bilgisi, KVKK hassas veri.

### Sunucu cevabı

```json
{ "valid": true, "valid_until": "2026-02-01T09:00:00Z" }
```

veya

```json
{ "valid": false, "reason": "customer_terminated" }
```

### Cache davranışı

- Son başarılı cevap 7 gün (`StaleAfter`) boyunca kabul edilir
- 7 gün sonrası network kopukluğu → lisans offline-only moduna düşer
- 4xx cevap ise otoriter → lisans anında invalid olur

### Air-gapped deployment

Air-gapped kurulumlar `online_validation: false` ile çalışır. Sadece
offline imza kontrolü yapılır. Hiçbir dış çağrı yoktur.

---

## Yaygın Operasyonlar

### Lisans yenileme

1. Satış ekibi yeni lisans dosyası üretir: `gen-trial-license.sh`
2. Müşteriye secure channel ile teslim (USB, imzalı e-posta)
3. Müşteri IT müdürü dosyayı `/etc/personel/license.json` üzerine yazar
4. API'yi reload: `curl -XPOST https://api/v1/system/license/refresh -H "Authorization: Bearer <admin-token>"`
5. Doğrula: `curl https://api/v1/system/license -H "Authorization: Bearer <admin-token>"` → `state: "valid"`

**Restart gerekmez** — license service dosyayı yeniden okur.

### Kapasite artışı (endpoint sayısı yükseltme)

Örneğin 100 → 200 endpoint:

1. Yeni lisans üret: `--max-endpoints=200`
2. Yukarıdaki yenileme akışını tekrarla
3. Yeni endpoint enrollment'ları başarılı olur

### Lisans iptali (müşteri kaybı)

1. Online validation server'ında customer_id → `revoked` olarak işaretle
2. Mevcut cache 7 gün içinde bayatlar, API invalid state'e geçer
3. Alternatif: müşteriden lisans dosyasını manuel silmesini isteyin
4. `docs/sales/poc-guide.md` içindeki teardown scripti de bunu içerir

### Donanım değişikliği (fingerprint değişti)

Lisans `fingerprint` içeriyorsa ve müşteri sunucusunu değiştirdiyse:

1. Müşteri yeni fingerprint'i gönderir (API `/v1/system/license` → error mesajında görünür)
2. Satış ekibi yeni fingerprint ile lisansı yeniden üretir
3. Müşteri eski lisansı siler, yenisini koyar

---

## Troubleshooting

| Belirti | Olası Sebep | Çözüm |
|---|---|---|
| API boot'ta `license: no license file loaded` | `/etc/personel/license.json` yok | Lisans dosyasını yerleştir, API'yi restart et |
| `license: invalid signature` | Vendor public key değişti (API sürüm farkı) | Personel destek ile lisansı yeniden üret |
| `license: fingerprint mismatch` | Donanım değişti (VM taşıma, RAM ekleme bazen) | Yeni fingerprint ile lisansı yenile |
| `license: expired beyond grace period` | Expires_at + 7 gün geçti | Yeni lisans al |
| Grace period bitmek üzere uyarısı | Expires'a <7 gün kaldı | Yenileme süreci başlat |
| `license: cached response is stale` | Phone-home 7 gündür başarısız | Online validation URL'ine ağ erişimi kontrol et |

### Log pattern'leri

```
INFO  license refreshed customer_id=acme-corp tier=business max_endpoints=100 state=valid
WARN  license phone-home failed; using cache err="dial tcp: lookup license.personel.local"
ERROR license cache is stale; downgrading age=168h reason=network_error
```

---

## Güvenlik Notları

1. **Vendor private key** sadece Personel şirketinin güvenli offline host'unda
   tutulmalı. Müşteri sistemine hiçbir zaman kopyalanmamalı.
2. **License file tampering** tespiti: imza doğrulaması başarısız olur, durum
   `invalid` olur. Audit log'a `license.tamper_detected` event yazılır.
3. **Dev mode**: API `AllowMissing: true` ile başlatılırsa (sadece CI/dev),
   sentetik "enterprise permissive" lisans kullanılır. Production config'de
   bu bayrak **false** olmalı.
4. **Key rotation**: Vendor key rotate edildiğinde eski lisanslar invalid olur.
   Roll-out: eski key'i public key ringe bir süre daha tutmak (roadmap Faz 17+).

---

## İlgili Dosyalar

- `apps/api/internal/license/` — implementation
- `infra/scripts/gen-trial-license.sh` — lisans üretici
- `docs/sales/poc-guide.md` — POC lisans süreci
- `docs/customer-success/support-sla.md` — lisans destekli SLA tierları
