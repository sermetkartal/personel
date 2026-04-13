# OCR İşlem Hattı (Faz 8 #83)

> Bu belge Personel UAM platformunun OCR servisinin mimarisini, iki-motorlu
> (Tesseract + PaddleOCR) yapısını, KVKK m.6 redaksiyon disiplinini ve canonical
> yanıt şemasını açıklar.

## 1. Amaç

Ekran görüntüsü yakalamaları `apps/ocr-service` Python FastAPI servisine iletilir.
Servis, görüntü üzerindeki metni çıkarır, **yanıtın dışarı çıkmadan önce** her
türlü özel nitelikli veriyi (TCKN, IBAN, kredi kartı, telefon, e-posta)
maskeler ve yapılandırılmış JSON olarak döndürür.

**KVKK m.6 değişmezi**: Servis, `text_redacted` alanının ham özel nitelikli
veriyi asla içermemesini garanti eder. Redaksiyon pipeline'ın son adımıdır ve
hata durumlarında bile ham çıktı geriye dönmez (`routes.py` her catch-all
bloğunda 500 + generic mesaj döner, çıkarılan metin log'a yazılmaz).

## 2. İki Motorlu Mimari

```
┌──────────┐   base64    ┌────────────────┐
│  caller  ├────────────►│ POST /v1/ocr/  │
│ (enrich) │             │     extract    │
└──────────┘             └────────┬───────┘
                                  │
                                  ▼
                         ┌────────────────┐
                         │  preprocess    │  (grayscale + autocontrast)
                         └────────┬───────┘
                                  │
                    ┌─────────────┴─────────────┐
                    │  backend_hint switch      │
                    └──┬───────────────────────┬┘
                       │                       │
                       ▼                       ▼
                ┌────────────┐          ┌────────────┐
                │ tesseract  │          │  paddle    │
                │  (tur+eng) │          │  (multi)   │
                └──────┬─────┘          └──────┬─────┘
                       │                       │
                       └──────────┬────────────┘
                                  ▼
                         ┌────────────────┐
                         │  postprocess   │  (conf filter + unicode NFC)
                         └────────┬───────┘
                                  │
                                  ▼
                         ┌────────────────┐
                         │  redact  (**)  │  ← KVKK son kapı
                         └────────┬───────┘
                                  │
                                  ▼
                         ┌────────────────┐
                         │ canonical_adapter│
                         │ text_redacted +  │
                         │ redaction_hits + │
                         │ words?           │
                         └────────┬───────┘
                                  │
                                  ▼
                             HTTP 200
```

### Motor seçim politikası

| backend_hint | Davranış |
|---|---|
| `tesseract` | Yalnızca Tesseract; yoksa 503 |
| `paddle` | Yalnızca PaddleOCR; yoksa 503 |
| `auto` | Tesseract mevcutsa tesseract; değilse paddle; ikisi de yoksa 503 |

**Neden Tesseract öncelikli?** Pilot ortamda `tur.traineddata` + `eng.traineddata`
indirilmiş durumda ve Tesseract'ın Türkçe doğruluğu (özellikle şapkalı + özel
karakterlerde) PaddleOCR'ın varsayılan modelinden belirgin biçimde daha iyi
çıktı. PaddleOCR çok dilli bir fallback olarak kalır.

**Ensemble modu** (şimdilik rezerve): `backend` alanı `"ensemble"` değerine izin
verir; bu motor iki sonucun kelime bazlı güven skorlarını karşılaştırıp en
yüksek skoru alacak şekilde birleştirebilir. Faz 8'de devreye alınmadı — adapter
hazır, `pipeline.py` tek motor yolu koşuyor.

## 3. KVKK Redaksiyon Matrisi

`personel_ocr/redaction.py` içinde beş desen çalışır:

| Kural | Algoritma | Örnek | Değiştirilen |
|---|---|---|---|
| TCKN | Resmi 11-hanelik checksum (d10 + d11 kuralları) | `12345678950` | `[TCKN]` |
| IBAN | `TR` + 24 hane + ISO 13616 mod-97 | `TR33 0006 ...` | `[IBAN]` |
| Kredi kartı | 13-19 hane + Luhn checksum | `4111 1111 1111 1111` | `[CREDIT_CARD]` |
| Telefon | `+90` / `0` prefix + 10 hane | `+90 532 123 45 67` | `[PHONE]` |
| E-posta | Basitleştirilmiş RFC 5322 | `ali@test.com` | `[EMAIL]` |

Checksum'lar zorunludur — false positive oranını düşürür. Örnek olarak
`11111111111` (tüm aynı haneler) TCKN kuralına yakalanır ama checksum ile
reddedilir.

`redact()` fonksiyonu idempotent'tir: aynı metni iki kez maskelemek işleyişi
değiştirmez (tag'ler pattern'lere uymaz).

## 4. Canonical Yanıt Şeması (Faz 8 #83)

Eski `/v1/extract` çağrıları (enricher) geriye dönük uyumlu kalır. Yeni tüketiciler
canonical endpoint'i kullanır:

```
POST /v1/ocr/extract
Content-Type: application/json

{
  "image_bytes": "<base64>",
  "tenant_id": "uuid",
  "endpoint_id": "uuid",
  "screenshot_id": "uuid",
  "backend_hint": "auto",
  "language": "auto",
  "confidence_per_word": false
}
```

Yanıt:

```json
{
  "request_id": "01HW4...",
  "backend": "tesseract",
  "language": "tur",
  "text_redacted": "TC: [TCKN] IBAN: [IBAN]",
  "confidence_overall": 0.82,
  "word_count": 47,
  "redaction_hits": [
    {"rule": "TCKN", "count": 1},
    {"rule": "IBAN", "count": 1}
  ],
  "processing_ms": 234,
  "words": null
}
```

`confidence_per_word=true` gönderilirse `words[]` alanı doldurulur. Her kelimenin
`text` alanı tekrar `redact()` fonksiyonundan geçirilir — savunma derinliği
prensibi: toplu geçişte yakalanmayan bir pattern (örn. tokenize edilince
görünür hale gelen) kelime döngüsünde yakalanır.

## 5. Batch Endpoint

```
POST /v1/ocr/batch
```

- Maksimum **50 item** (Pydantic `max_length=50`)
- Maksimum **10 MB toplam base64 payload** (adapter `MAX_BATCH_PAYLOAD_BYTES`)
- **Paralel yürütme**: `ThreadPoolExecutor(max_workers=4)` ile koşar. Tesseract
  ve PaddleOCR her ikisi de C binding kullandığı için GIL serbest kalır.
- **Item-level hata izolasyonu**: bir item başarısız olursa batch iptal edilmez;
  başarısız item boş `text_redacted` + `processing_ms=0` ile dönderilir, diğerleri
  tamamlanır. Gerekçe: enricher retry penceresi item bazlı değil batch bazlı
  olduğu için tek hatalı item yüzünden 49 başarılı sonuç kaybedilmemeli.

## 6. Gözlemlenebilirlik

Prometheus series'leri:

| Metric | Tip | Label'lar |
|---|---|---|
| `personel_ocr_extractions_total` | Counter | engine, status |
| `personel_ocr_extraction_latency_seconds` | Histogram | engine |
| `personel_ocr_redactions_total` | Counter | kind |
| `personel_ocr_word_count` | Histogram | - |
| `personel_ocr_batch_size` | Histogram | - |
| `personel_ocr_http_requests_total` | Counter | method, path, status_code |
| `personel_ocr_http_request_latency_seconds` | Histogram | method, path |

Grafana paneli ilgili Faz 8 #89 dashboard'unda `ocr` grubu altına eklendi.

## 7. Test Matrisi

`apps/ocr-service/tests/test_redaction.py`:

- `TestTCKNValidation` / `TestTCKNRedaction` — checksum doğrulama + pozitif/
  negatif pattern matching
- `TestIBANValidation` / `TestIBANRedaction` — boşluklu + kompakt + mod-97
- `TestLuhnValidation` / `TestCreditCardRedaction` — Visa + Mastercard
- `TestPhoneRedaction` + `TestPhoneFormatMatrix` — altı ayrı Türkçe format
- `TestEmailRedaction` — tekli + metin içi
- `TestCompoundRedaction` — karışık PII + boş metin dayanıklılığı
- **`TestLeakRegression`** — KVKK değişmezi için regression test class'ı:
  11 farklı PII değeri birleştirilip tek pass ile maskelenir, sonra her
  değerin ham halinin yanıt metninde **bulunmadığı** assert edilir.

## 8. KVKK m.6 + ADR 0013 bağlantısı

Bu pipeline, **`dlp_enabled=false`** tenant'lar için de koşar — çünkü OCR çıktısı
klavye vuruşu değil, ekran içeriğidir ve aydınlatma metni ekran görüntüsü
yakalamasını kapsar. Ancak:

1. OCR çalışmadan önce enricher tarafında `screenshot_exclude_apps` policy'si
   uygulanır — hassas uygulamaların görüntüleri hiç bu servise gelmez
   (banking, parola yöneticisi, InPrivate mod).
2. Redaksiyon her tenant için default ON — opt-out yoktur.
3. `text_redacted` alanı yerine ham metni döndüren *hiçbir* API path'i yoktur;
   DPO bile ham metne yalnızca WORM bucket'taki asıl ekran görüntüsü üzerinden
   (ikili onayla) erişebilir.

## 9. Referanslar

- `apps/ocr-service/src/personel_ocr/redaction.py` — pattern'lar + checksum'lar
- `apps/ocr-service/src/personel_ocr/pipeline.py` — legacy yol (enricher uyum)
- `apps/ocr-service/src/personel_ocr/canonical.py` — yeni şema modelleri
- `apps/ocr-service/src/personel_ocr/canonical_adapter.py` — adapter + batch
- `docs/compliance/kvkk-framework.md` §6 — m.6 özel nitelikli veri kuralları
- `docs/adr/0013-dlp-disabled-by-default.md` — DLP opt-in mimarisi
