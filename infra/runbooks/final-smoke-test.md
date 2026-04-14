# Final Smoke Test — Operatör Runbook'u

> **Kapsam**: Faz 17 #187. Tam yığın üretim kurulumundan sonra 10 dakika
> içinde uçtan uca doğrulama. Pilot müşteri sign-off ve yeni sürüm kanarya
> kontrolü için zorunlu.
>
> **Script**: `infra/scripts/final-smoke-test.sh`
> **Süre bütçesi**: 10 dakika (hard ceiling, 600s)
> **Çıktı**: `/var/log/personel/final-smoke.{json,md}`

---

## 1. Amaç

`final-smoke-test.sh` üç ayrı doğrulama katmanını tek raporda birleştirir:

1. **Preflight** — OS gereksinimleri, disk, RAM, port bitişikliği
2. **Post-install validate** — 18 servisin `/healthz` veya TCP probe'u +
   hızlı NATS/ClickHouse akış testi
3. **QA smoke binary** (`apps/qa/cmd/smoke`) — API login → employee list →
   audit event insert → DSR create → state doğrulama
4. **Phase 1 exit harness** (`apps/qa/cmd/phase1-exit`) — 18 Faz 1 çıkış
   kriterinin otomatik doğrulaması, `thresholds.yaml` karşısında

Her aşama bağımsız çalışır; bir aşama başarısız olsa bile sonraki aşamalar
denenir. En sonda tek JSON + Markdown özetle biter. Bütçe aşılırsa kalan
aşamalar atlanır ve `overall=fail` olur.

## 2. Önkoşullar

- `kartal@vm3` (veya eşdeğer operatör) kullanıcısı
- `curl`, `jq`, `go` PATH'te (smoke/phase1-exit derlemesi için `go 1.22+`)
- `/opt/personel/infra/compose/.env` hazır, tüm servisler ayakta
- Geçerli bir admin JWT'si (Keycloak üzerinden alınmış)
- `thresholds.yaml` okunabilir (`apps/qa/ci/thresholds.yaml`)

## 3. Çalıştırma

```bash
cd /opt/personel/infra/scripts

# admin token al (Keycloak password grant, yalnızca test tenant için)
ADMIN_JWT=$(curl -s -X POST \
  "http://192.168.5.44:8080/realms/personel/protocol/openid-connect/token" \
  -d "grant_type=password" \
  -d "client_id=personel-admin-api" \
  -d "username=admin" \
  -d "password=${KEYCLOAK_ADMIN_PASSWORD}" \
  | jq -r .access_token)

./final-smoke-test.sh \
  --api-url=http://192.168.5.44:8000 \
  --gateway-url=http://192.168.5.44:9443 \
  --console-url=http://192.168.5.44:3000 \
  --portal-url=http://192.168.5.44:3001 \
  --admin-token="${ADMIN_JWT}" \
  --out=/var/log/personel/final-smoke.json \
  --md=/var/log/personel/final-smoke.md
```

### Hızlı mod (Phase 1 exit harness'i atla)

Kanarya / CI'da 10 dakikadan kısa pencere varsa:

```bash
./final-smoke-test.sh --skip-phase1 --budget=300
```

## 4. Başarılı Çıktı Örneği

```
================================================================
final-smoke-test complete: overall=PASS duration=418s
  json: /var/log/personel/final-smoke.json
  md:   /var/log/personel/final-smoke.md
================================================================
```

Markdown özeti pilot sign-off ticket'ına EK olarak yüklenmelidir.

## 5. Sorun Giderme

| Belirti | Sebep | Çözüm |
|---|---|---|
| `ERROR: required binary not found: jq` | `jq` paketi eksik | `sudo apt install -y jq` |
| `preflight FAIL rc=2` | RAM/disk/port çakışması | `preflight-check.sh` çıktısını oku |
| `post-install-validate fail notes="exit 1"` | Bir servis sağlıksız | `docker compose ps`, ardından `./troubleshoot.sh` |
| `smoke-build FAIL` | `go` PATH'te değil veya modül çözülemiyor | `go version`, `cd apps/qa && go mod tidy` |
| `phase1-exit FAIL` + `#9 keystroke leak detected` | **BLOCKER** — ADR 0013 ihlali | Derhal sürümü durdur, DPO'ya haber ver, `audit-redteam` output'unu topla |
| `budget 600s exhausted` | Ağ veya CH yavaşlığı | Budget'ı `--budget=900` ile geçici artır, altyapı nedenini araştır |

## 6. Hata Durumunda Bildirilmesi Gerekenler

Herhangi bir aşama `fail` ise operator aşağıdaki artefaktları ticket'a eklemelidir:

1. `/var/log/personel/final-smoke.json`
2. `/var/log/personel/final-smoke.md`
3. `docker compose ps -a`
4. `docker compose logs --tail=200 <failing-service>`
5. `kubectl`/`docker` olmayan hataysa `journalctl -u personel-*.service --since '30 min ago'`

## 7. Geçerlilik

- Script `infra/runbooks/phase-1-exit-criteria.md` ile eşleştirilmelidir.
- Sürüm bump'larında hem `thresholds.yaml` hem bu runbook güncellenir.
- Yılda bir kez DPO incelemesi (denetim hazırlığı için).

## 8. Wave 8 + Wave 9 sonrası ek doğrulama adımları

Wave 8 (screenshot preset + user_sid HKU + keystroke diagnostic) ve Wave 9
(KVKK menü + Settings genişletme + Admin bypass) deploy edildikten sonra
`final-smoke-test.sh` çıktısına ek olarak şu manuel kontroller koşulmalı.

### Wave 8 — Agent pipeline

```bash
# 1. Real user SID (LocalSystem değil, gerçek S-1-5-21-* SID)
docker exec personel-clickhouse clickhouse-client --user=personel_admin \
  --password=clickhouse_admin_pass --query \
  "SELECT DISTINCT user_sid FROM personel.events_raw WHERE received_at > now() - INTERVAL 5 MINUTE"
# Beklenen: S-1-5-21-... (S-1-5-18 değil)

# 2. Screenshot boyut preset'e uyuyor mu
docker exec personel-clickhouse clickhouse-client --user=personel_admin \
  --password=clickhouse_admin_pass --query \
  "SELECT event_type, max(length(payload)) FROM personel.events_raw
   WHERE event_type='screenshot.captured' AND received_at > now() - INTERVAL 10 MINUTE GROUP BY event_type"
# Beklenen high preset'te < 60 KB, max preset'te < 130 KB

# 3. Keystroke events 30s sonrası görünüyor mu
# Windows'ta Notepad'e 10+ karakter yaz, 35s bekle, sonra:
docker exec personel-clickhouse clickhouse-client --user=personel_admin \
  --password=clickhouse_admin_pass --query \
  "SELECT count() FROM personel.events_raw
   WHERE event_type='keystroke.window_stats' AND received_at > now() - INTERVAL 5 MINUTE"
# Beklenen: >= 1
```

### Wave 9 — KVKK + Settings

```bash
# 1. /v1/kvkk/verbis endpoint aktif mi
curl -sk -H "Authorization: Bearer $JWT" https://192.168.5.44:8000/v1/kvkk/verbis
# Beklenen: 200 ile {registration_number: null, registered_at: null}

# 2. Yeni settings endpoint'leri
curl -sk -H "Authorization: Bearer $JWT" https://192.168.5.44:8000/v1/settings/retention
# Beklenen: KVKK defaults {audit_years:5, event_days:365, ...}

curl -sk -H "Authorization: Bearer $JWT" https://192.168.5.44:8000/v1/settings/ca-mode
# Beklenen: {mode:"internal", config:{}}

curl -sk -H "Authorization: Bearer $JWT" https://192.168.5.44:8000/v1/settings/integrations
# Beklenen: {"items":[]}

# 3. Admin bypass — live view request
# Admin JWT ile /v1/liveview/request çağırıldığında session direkt "approved"
# state'inde açılmalı. details.admin_bypass=true audit log'da görünmeli.
curl -sk -H "Authorization: Bearer $ADMIN_JWT" https://192.168.5.44:8000/v1/audit \
  | jq '.items[] | select(.details.admin_bypass==true) | {action, actor_id, created_at}'
```

### Console UI doğrulama (tarayıcı)

- `/tr/kvkk/guide` → KVKK rehberi sayfası yükleniyor, 7 adım görünür
- `/tr/kvkk/dsr` → eski `/tr/dsr` oradan 301 ile yönleniyor mu test et
- `/tr/settings/integrations` → 5 servis kartı (MaxMind pre-fill 891169)
- `/tr/settings/retention` → 6 alan + KVKK minimum hint
- `/tr/settings/backup` → "Yeni Storage Ekle" modal'ı 7 backend türü gösteriyor
- `/tr/settings/security/tls` → 3 mode radio card (Internal default)

### Phase 2 / Faz 2 kelimesi kalmadı mı

```bash
grep -rE "[Pp]hase.?2|[Ff]az.?2" apps/console/messages/ apps/portal/messages/
# Beklenen: sadece kod yorumları varsa, user-facing text görünmemeli
```

Wave 8 + Wave 9 deploy doğrulama tamamlanmadan Phase 1 exit kontrolü
otomatik "stale" sayılır — bu ek adımlar `final-smoke-test.sh` v2'ye
dahil edilecek (ileri sprint).

---

*Son güncelleme*: 2026-04-14 — Wave 9 closeout (Sprint 6).
