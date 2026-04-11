# SOC 2 Manuel Kanıt Submit — DPO Runbook'u

> **Hedef kitle**: DPO, Admin, Platform Lead.
> **Amaç**: Otomatik collector'u olmayan ya da tetiklenmemiş kontroller için
> manuel kanıt submit etmek. Üç kontrol bu runbook ile yönetilir:
> CC6.3 (access review), CC7.3 (incident closure), CC9.1 (BCP drill).
> A1.2 (backup run) otomatiktir ama acil durumlarda manuel submit edilebilir.
> **Ön koşul**: DPO veya Admin rolünde oturum açılmış olmalı.
> **Kaynak belgeler**: `docs/policies/access-review.md`,
> `docs/policies/incident-response.md`, `docs/policies/business-continuity-disaster-recovery.md`.

---

## 0. Genel Akış

Her manuel submit aynı şablonu izler:

1. İlgili ceremony (review / incident PIR / drill) tamamlanır
2. DPO formu doldurur (veya runbook örneğinden uyarlar)
3. `curl` ile ilgili endpoint'e POST atılır
4. API imzalı evidence item'ını üretir + tenant'ın `audit-worm` bucket'ına WORM yazar
5. `/tr/evidence` dashboard'unda kontrolün sayımı +1 olur, gap (varsa) kapanır

Tüm endpoint'ler Bearer token ile korunur. Token'ı konsoldaki "Profil → API Token" sayfasından alabilirsin ya da `kubectl exec`/`docker exec` ile servis hesabından geçici olarak üretebilirsin.

---

## 1. CC6.3 — Access Review (`POST /v1/system/access-reviews`)

### Ne zaman tetiklenir
- **Çeyreklik** (quarterly): admin / dpo / investigator / legal_hold_owners / vault_root / break_glass rollerinin erişim listesi
- **Yarı-yıllık** (semi-annual): `regular_users` scope'u

### Ön hazırlık
1. HRIS + Keycloak'tan scope'a ait kullanıcı listesini al
2. İlgili role sahipleriyle birlikte her kullanıcıyı gözden geçir
3. Her kullanıcı için karar: `retained`, `revoked`, `reduced`
4. vault_root veya break_glass ise ikinci DPO/CISO imzası gerekli

### Submit
```bash
TOKEN="$(cat ~/.personel/api-token)"

curl -sS -X POST "https://<api-host>/v1/system/access-reviews" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @- <<'EOF'
{
  "scope": "admin_role",
  "second_reviewer_id": "",
  "started_at": "2026-04-11T09:00:00Z",
  "completed_at": "2026-04-11T09:45:00Z",
  "decisions": [
    {"user_id":"u-1","username":"alice","action":"retained","reason":"active admin, needed"},
    {"user_id":"u-2","username":"bob","action":"revoked","reason":"transferred to ops team 2 ay önce"},
    {"user_id":"u-3","username":"carol","action":"reduced","reason":"demoted to manager"}
  ],
  "notes": "Q2 2026 admin role review. 1 revoke, 1 scope reduction."
}
EOF
```

Response: `{"evidence_id":"01J..."}`

### Çift kontrol örneği (vault_root)
```json
{
  "scope": "vault_root",
  "second_reviewer_id": "<uuid-of-other-DPO>",
  "started_at": "2026-04-11T10:00:00Z",
  "completed_at": "2026-04-11T10:30:00Z",
  "decisions": [
    {"user_id":"u-platform-1","username":"cto","action":"retained","reason":"vault root operator"}
  ]
}
```

**İkinci reviewer birinci ile aynı olamaz** — API 422 döner.

---

## 2. CC7.3 — Incident Closure (`POST /v1/system/incident-closures`)

### Ne zaman tetiklenir
Her kapatılan security/compliance incident sonrası post-incident review (PIR) imzalandığında. 5-tier severity model kullanılır:

| Severity | Örnekler |
|---|---|
| informational | Proaktif tespit, etki yok |
| low | Kısmi DLP tetikleme, 1-2 kullanıcı |
| medium | Bir tenant içi politika ihlali, sınırlı veri erişimi |
| high | Yetkisiz erişim denemesi, account takeover |
| critical | Gerçek veri sızıntısı, KVKK 72h tetikli |

### Ön hazırlık
1. PIR'i tamamla, kök neden ve önlemleri belirle
2. KVKK m.12 72h notification gerekiyorsa kurul başvurusunun timestamp'ını yaz
3. GDPR Art. 33 gerekiyorsa EU süpervizörüne notifikasyon timestamp'ını yaz
4. Remediation action listesini sırala

### Submit
```bash
curl -sS -X POST "https://<api-host>/v1/system/incident-closures" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @- <<'EOF'
{
  "incident_id": "INC-2026-04-03",
  "severity": "high",
  "detected_at":  "2026-04-03T14:22:00Z",
  "contained_at": "2026-04-03T14:47:00Z",
  "closed_at":    "2026-04-10T16:00:00Z",
  "summary": "Bir yönetici hesabı brute force denendi, MFA bloke etti",
  "kvkk_notified_at": "",
  "gdpr_notified_at": "",
  "root_cause": "Hesap e-postası bir LinkedIn leak'inde ortaya çıkmıştı, MFA olmadan kısa bir pencere vardı",
  "remediation_actions": [
    "Tüm admin hesaplarına MFA zorunluluğu enforce edildi",
    "Failed-login rate limit 5→3/hour'a düşürüldü",
    "Policy review cadence çeyrekliğe çekildi"
  ]
}
EOF
```

### KVKK tetikli örnek
```json
{
  "incident_id": "INC-2026-04-08",
  "severity": "critical",
  "detected_at":     "2026-04-08T02:15:00Z",
  "contained_at":    "2026-04-08T03:00:00Z",
  "closed_at":       "2026-04-11T17:00:00Z",
  "kvkk_notified_at":"2026-04-09T10:00:00Z",
  "gdpr_notified_at":"",
  "summary": "3 çalışan screenshot'ı yanlış tenant'a sızdı",
  "root_cause": "Migration script'i tenant filtresi unutmuştu",
  "remediation_actions": ["Script'e tenant_id assertion eklendi", "Change management CAB'ye escalation"]
}
```

**72h clock** `detected_at` + 72 saat. API, `kvkk_within_72h` booleanını otomatik hesaplar — geç bile olsa kayıt atılmalı, silinen kayıt yoktur.

---

## 3. CC9.1 — BCP / DR Drill (`POST /v1/system/bcp-drills`)

### Ne zaman tetiklenir
- **Çeyreklik tabletop**: masa başında senaryo tartışması, gerçek restore yapılmaz
- **Yıllık live drill**: gerçek restore, tam stack rebuild

### Ön hazırlık
1. Drill senaryosunu seç (ransomware / vault_compromise / clickhouse_loss / az_failure / full_site_loss)
2. Tier'ları belirle: Tier 0 (Vault+audit), Tier 1 (gateway/API/Postgres/Keycloak), Tier 2 (Console/Portal/ClickHouse/MinIO), Tier 3 (ML/OCR/UBA/LiveKit)
3. Drill sırasında her tier için target vs actual RTO'yu kaydet
4. Lessons learned dokümanını yaz

### Submit
```bash
curl -sS -X POST "https://<api-host>/v1/system/bcp-drills" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @- <<'EOF'
{
  "drill_id": "BCP-2026-Q1-live",
  "type": "live",
  "scenario": "ransomware",
  "started_at":   "2026-03-15T09:00:00Z",
  "completed_at": "2026-03-15T14:30:00Z",
  "tier_results": [
    {
      "tier": 0,
      "service": "vault+audit_chain",
      "target_rto_seconds": 7200,
      "actual_rto_seconds": 5400,
      "met_rto": true,
      "notes": "Shamir unseal 3/5 OK"
    },
    {
      "tier": 1,
      "service": "postgres+api+gateway+keycloak",
      "target_rto_seconds": 14400,
      "actual_rto_seconds": 12600,
      "met_rto": true
    },
    {
      "tier": 2,
      "service": "clickhouse",
      "target_rto_seconds": 28800,
      "actual_rto_seconds": 34200,
      "met_rto": false,
      "notes": "MinIO lifecycle rehydration yavaştı"
    }
  ],
  "lessons_learned": "Tier 2 RTO ihlal edildi — MinIO lifecycle read-back path optimize edilmeli. Action item: infra/runbooks/backup-restore.md §5 güncellemesi."
}
EOF
```

### Tabletop örnek
```json
{
  "drill_id": "BCP-2026-Q2-tabletop",
  "type": "tabletop",
  "scenario": "vault_compromise",
  "started_at":   "2026-04-05T14:00:00Z",
  "completed_at": "2026-04-05T15:30:00Z",
  "tier_results": [
    {"tier": 0, "service": "vault", "target_rto_seconds": 7200, "actual_rto_seconds": 6000, "met_rto": true, "notes": "Şamir ceremony walkthrough"}
  ],
  "lessons_learned": "Şamir key tutucularından 1 kişi tatilde — yedek key tutucuları planı genişletmek gerek"
}
```

---

## 4. A1.2 — Manuel Backup Run Submit (opsiyonel)

`POST /v1/system/backup-runs` normalde sistemd timer + backup script tarafından otomatik çağrılır. Ancak acil durumlarda (cron çöktü, manuel restore sonrası re-bake) operatör elle submit edebilir.

```bash
curl -sS -X POST "https://<api-host>/v1/system/backup-runs" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @- <<'EOF'
{
  "kind": "postgres",
  "target_path": "minio://backups/postgres/2026-04-11T03-00-00.pgdump",
  "size_bytes": 4523789012,
  "sha256": "0123abcd...",
  "started_at":  "2026-04-11T03:00:00Z",
  "finished_at": "2026-04-11T03:18:00Z",
  "source_host": "db-primary-01.internal#45123"
}
EOF
```

---

## 5. Submit Sonrası Doğrulama

Her submit sonrasında:

1. **Coverage dashboard**: `/tr/evidence` → dönemi seç → ilgili kontrolün sayımı +1 olmalı
2. **Gap ibresi**: Eğer bu submit ilgili kontroldeki ilk item ise, "Eksik Kontrol" kutucuğundan çıkmalı
3. **Prometheus**: 5 dakika içinde `personel_evidence_items_total{control="<X>"}` gauge'u 0'dan yukarı çıkmalı
4. **Audit log**: `/tr/audit` → ilgili `{access_review,incident,bcp_drill,backup}.*` action satırı görünmeli
5. **WORM bucket**: Platform ekibi `audit-worm/evidence/<tenant>/<period>/<id>.bin` objesinin varlığını doğrulayabilir

---

## 6. Hata Durumları

| HTTP | Anlam | Çözüm |
|---|---|---|
| 400 | Malformed JSON | JSON'u `jq .` ile validate et |
| 401 | Token yok/expired | Yeni token al |
| 403 | Rol yetersiz | DPO veya Admin rolünde oturum gerekli |
| 422 | Alan eksik / dual-control ihlali | Hata mesajını oku, eksik alanı ekle |
| 500 | API hata (WORM sink down?) | `/tr/evidence` dashboard'u hâlâ açılıyor mu? WORM sink durumunu platform ekibinden kontrol et |

---

## İlgili Dokümanlar

- `infra/runbooks/soc2-evidence-pack-retrieval.md` — aylık pack üretimi
- `docs/policies/access-review.md` — CC6.3 policy kapsamı
- `docs/policies/incident-response.md` — CC7.3 policy kapsamı
- `docs/policies/business-continuity-disaster-recovery.md` — CC9.1 policy kapsamı
- `docs/adr/0023-soc2-type2-controls.md` — kontrol matrisi
- `CLAUDE.md` §5 — Phase 3.0 kollektör durumu

---

*Versiyon 1.0 — Phase 3.0.5 — 2026-04-11*
