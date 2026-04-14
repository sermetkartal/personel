# Wave 8 Deploy Runbook — Operatör Kılavuzu

> **Hedef kitle**: Personel platform SRE / DevOps (vm3 primary) + Windows pilot
> operatörü (192.168.5.30)
> **Süre**: ~30 dakika (API rebuild dahil, agent build hariç)
> **Kapsam**: Wave 8 dört commit'inin canlı vm3 stack'ine ve Windows pilot
> agent'ına uygulanması
> **Sahip**: SRE — pilot sign-off gerekli

---

## 1. Context

Wave 8 üç gerçek düzeltme + bir UI temizliği içerir. Dördü de main'e merge
edilmiştir ancak vm3'e deploy edilmemiştir.

| Commit | Kapsam | Etkilenen bileşenler |
|---|---|---|
| `b6189bc` | Screenshot size presets + Settings UI | agent, api (migration 0037), console |
| `50ef60f` | HKEY_USERS fallback (user_sid session-0 isolation) | agent (personel-collectors) |
| `babc88a` | Keystroke meta diagnostic + flush shortening | agent (personel-collectors) |
| `ee4696b` | Konsol "Phase 2" / "Faz 2" iç etiket gizleme | console |

Semptom-çözüm eşlemesi:

- **`S-1-5-18` her olayda** → `50ef60f` düzeltir (HKU fallback)
- **Screenshot boyutu sabit ve büyük** → `b6189bc` preset'leri açar
- **`keystroke.window_stats` ClickHouse'a düşmüyor** → `babc88a` diagnostic
  + flush cadence (30s → 10s) düzeltir
- **Konsolda "Phase 2" / "Faz 2" badge'ları görünüyor** → `ee4696b` gizler

## 2. Önkoşullar

**vm3 (192.168.5.44)**
- `ssh kartal@192.168.5.44` (şifre `qwer123!!`)
- `sudo` yetkisi (systemd servislerini durdur/başlat)
- `/home/kartal/personel` temiz çalışma dizini
- Postgres superuser erişimi (migration 0037 apply)
- Docker Compose v2 + `go 1.22+` + `pnpm 9+` PATH'te
- `docker exec personel-postgres psql` çalışıyor

**Windows VM (192.168.5.30)**
- `ssh kartal@192.168.5.30` veya direkt RDP
- `C:\personel` temiz çalışma dizini
- Rust toolchain 1.88+ (rustup), MSVC + FireDaemon OpenSSL env var'ları
  CLAUDE.md §0'da belirtilen şekilde export edilmiş
- `C:\Program Files (x86)\Personel\Agent\` altında mevcut kurulum
- Servisi durdurma/başlatma yetkisi (`net stop personel-agent`)

**Artefakt**:
- Bu runbook vm3 ve Windows VM'de `git pull` sonrası erişilebilir olmalı.
- Wave 8 dört commit'i de `origin/main` üzerinde görünür olmalı:
  ```bash
  git log --oneline | grep -E 'b6189bc|50ef60f|babc88a|ee4696b'
  ```

## 3. Adım 1 — vm3 Repo Pull + Migration 0037 Apply

```bash
ssh kartal@192.168.5.44
cd /home/kartal/personel
git fetch --prune
git log --oneline origin/main -6   # 4 Wave 8 commit'i görünmeli
git pull --ff-only origin main
```

Migration 0037'yi uygula (tenants tablosuna `screenshot_preset` kolonu ekler):

```bash
docker exec -i personel-postgres psql -U postgres -d personel \
  < apps/api/internal/postgres/migrations/0037_tenant_screenshot_preset.up.sql

# Doğrulama:
docker exec personel-postgres psql -U postgres -d personel -c "\d tenants" \
  | grep screenshot_preset
# Beklenen: screenshot_preset | text | ... | "high"
```

Migration hatası durumunda STOP — önce rollback bölümüne git.

## 4. Adım 2 — Admin API Rebuild + Restart

```bash
cd /home/kartal/personel/apps/api
go build -o /tmp/personel-api ./cmd/api

# Binary'nin doğruluğunu hızlıca doğrula:
/tmp/personel-api --version 2>/dev/null || echo "OK (no --version flag)"
file /tmp/personel-api   # ELF 64-bit beklenen

sudo systemctl stop personel-api
sudo cp /tmp/personel-api /usr/local/bin/personel-api
sudo systemctl start personel-api
sudo systemctl status personel-api --no-pager | head -20
```

Health check:

```bash
curl -sf http://127.0.0.1:8000/healthz && echo OK
curl -sf http://127.0.0.1:8000/v1/tenants/me/screenshot-preset \
  -H "Authorization: Bearer ${ADMIN_JWT}" | jq .
# Beklenen: {"preset":"high"} veya migration-default
```

Başarısız ise API journal'ı kontrol et:

```bash
sudo journalctl -u personel-api --since '5 min ago' | tail -80
```

## 5. Adım 3 — Console Rebuild + Restart

```bash
cd /home/kartal/personel/apps/console
pnpm install --frozen-lockfile
pnpm build

sudo systemctl stop personel-console
sudo systemctl start personel-console
sudo systemctl status personel-console --no-pager | head -20
```

Tarayıcıdan doğrulama:

1. `https://<vm3-console>/tr/settings/general` → yeni "Ekran Görüntüsü
   Yakalama" bölümü gözükmeli, dropdown'da 5 preset (`minimal`, `low`,
   `medium`, `high`, `max`) listeli.
2. Dropdown'u `medium` yap → optimistik kaydet bildirimi çıkmalı.
3. Herhangi bir sayfa kenarında "Phase 2" / "Faz 2" badge'i artık
   görünmemeli (`ee4696b`).

## 6. Adım 4 — Windows Agent Binary Deploy

Windows VM (192.168.5.30) üzerinde PowerShell ile:

```powershell
# vcvars + env var'lar CLAUDE.md §0'da (OPENSSL_DIR vb)
cd C:\personel
git fetch --prune
git pull --ff-only origin main
git log --oneline -6   # 4 commit'i doğrula

cd C:\personel\apps\agent
cargo build --release -p personel-agent -p personel-watchdog -p personel-enroll
```

Build hatası yoksa binary deploy:

```powershell
net stop personel-agent

Copy-Item -Force `
  C:\personel\apps\agent\target\release\personel-agent.exe `
  "C:\Program Files (x86)\Personel\Agent\personel-agent.exe"

Copy-Item -Force `
  C:\personel\apps\agent\target\release\personel-watchdog.exe `
  "C:\Program Files (x86)\Personel\Agent\personel-watchdog.exe"

net start personel-agent
sc.exe query personel-agent
```

İsteğe bağlı — MSI yeniden üretimi (pilot dağıtımı gerekiyorsa):

```powershell
cd C:\personel\apps\agent\installer
.\build-msi.ps1
# Çıktı: C:\personel\apps\agent\installer\dist\personel-agent.msi
```

Agent log kuyruğunu kontrol et:

```powershell
Get-Content `
  "C:\ProgramData\Personel\agent\logs\personel-agent.log" `
  -Tail 60 -Wait
```

Beklenen log satırları:

- `screenshot preset: high max_h=1080 q=65 cadence=60s`
  (veya tenant'a göre farklı preset)
- Session 0 izolasyonu varsa: `WTSQueryUserToken failed win32_error=1314
  ... falling back to HKU enum` + `resolved active sid via HKU`
- `keystroke cb_fired > 0` — flush cadence 10s'de bir diagnostic log

## 7. Adım 5 — Uçtan Uca Doğrulama

vm3'te, Windows agent 5 dakika çalıştıktan sonra:

### 5.1 Gerçek user SID (ClickHouse)

```bash
docker exec personel-clickhouse clickhouse-client --query "
  SELECT user_sid, count() AS n
  FROM personel.events_raw
  WHERE received_at > now() - INTERVAL 5 MINUTE
    AND endpoint_id = '<kartal-endpoint-id>'
  GROUP BY user_sid
  ORDER BY n DESC
  LIMIT 5
"
```

**Beklenen**: En büyük satır `S-1-5-21-...` (gerçek kullanıcı SID'i), NOT
`S-1-5-18`. `S-1-5-18` satırı varsa tamamen gitmemiş olabilir ama baskın
satır gerçek SID olmalı.

**FAIL durumu**: Tüm satırlar `S-1-5-18` → `50ef60f` commit'i agent'ta
yok. Adım 4'ü tekrar çalıştır + binary zaman damgasını kontrol et.

### 5.2 Screenshot boyutu preset'e uygun

Tenant preset'i `medium` olarak bırak, agent policy refresh sonrası:

```bash
docker exec personel-minio mc ls personel-minio/personel-screenshots \
  --recursive | tail -10
# Boyutlar preset medium için ~25 KB civarında olmalı (±10 KB)
```

Veya ClickHouse event'larından:

```bash
docker exec personel-clickhouse clickhouse-client --query "
  SELECT
    avg(toInt64(JSONExtractString(details, 'width'))) AS avg_w,
    avg(toInt64(JSONExtractString(details, 'height'))) AS avg_h,
    avg(toInt64(JSONExtractString(details, 'size_bytes'))) AS avg_size
  FROM personel.events_raw
  WHERE event_kind = 'screen_captured'
    AND received_at > now() - INTERVAL 5 MINUTE
"
# Preset 'medium' → avg_h ~900, avg_size ~25000
```

### 5.3 Keystroke window stats akışı

```bash
docker exec personel-clickhouse clickhouse-client --query "
  SELECT count() FROM personel.events_raw
  WHERE event_kind = 'keystroke.window_stats'
    AND received_at > now() - INTERVAL 30 SECOND
"
# Beklenen: >= 1 (flush cadence 10s'ye indirildi)
```

**FAIL durumu**: 0 satır → agent log'da `keystroke cb_fired` değerini
gör, 0 ise ETW session kayıt olmamış, işletim sistemi izinleri kontrol et
(`sc.exe qprivs personel-agent`).

### 5.4 Agent log — diagnostic counter'lar

Windows VM'de:

```powershell
Select-String -Path "C:\ProgramData\Personel\agent\logs\personel-agent.log" `
  -Pattern "cb_fired|window_stats|flushed" -Tail 50
```

Beklenen: `cb_fired > 0` + `window_stats flushed count=<N>` periyodik.

### 5.5 Konsol UI — Phase 2 badge kayboldu

Tarayıcıdan 5 sayfa gez (`/tr/endpoints`, `/tr/live-view`,
`/tr/audit`, `/tr/dsr`, `/tr/settings`). "Phase 2" / "Faz 2" / "Phase 3"
text'i görünmemeli. Sidebar label'ları temiz olmalı.

## 8. Rollback

Sıra önemli — binary rollback ÖNCE, migration sonra.

### 8.1 API rollback

```bash
# vm3'te önceki binary yedeklenmiş ise:
sudo systemctl stop personel-api
sudo cp /usr/local/bin/personel-api.prev /usr/local/bin/personel-api
sudo systemctl start personel-api

# Yoksa önceki commit'ten rebuild:
cd /home/kartal/personel
git checkout b7089b7~1  # 4791685, Wave 8 öncesi
cd apps/api && go build -o /tmp/personel-api ./cmd/api
sudo systemctl stop personel-api
sudo cp /tmp/personel-api /usr/local/bin/personel-api
sudo systemctl start personel-api
cd /home/kartal/personel && git checkout main
```

### 8.2 Console rollback

```bash
cd /home/kartal/personel && git checkout 4791685  # Wave 8 öncesi console
cd apps/console && pnpm install --frozen-lockfile && pnpm build
sudo systemctl restart personel-console
cd /home/kartal/personel && git checkout main
```

### 8.3 Migration 0037 rollback (SADECE API rollback yapıldıysa)

```bash
docker exec -i personel-postgres psql -U postgres -d personel \
  < apps/api/internal/postgres/migrations/0037_tenant_screenshot_preset.down.sql

docker exec personel-postgres psql -U postgres -d personel -c "\d tenants" \
  | grep screenshot_preset
# Beklenen: boş (kolon drop edildi)
```

### 8.4 Windows agent rollback

Önceki `personel-agent.exe` yedeğini `C:\Program Files (x86)\Personel\
Agent\personel-agent.exe.bak` olarak tuttuysanız:

```powershell
net stop personel-agent
Copy-Item -Force `
  "C:\Program Files (x86)\Personel\Agent\personel-agent.exe.bak" `
  "C:\Program Files (x86)\Personel\Agent\personel-agent.exe"
net start personel-agent
```

Yedek yoksa: `git checkout 09127e7` (Wave 7 commit'i) → `cargo build
--release -p personel-agent` → deploy.

## 9. KVKK / Audit Notu

Wave 8 deploy'u:

- `b6189bc` — ekran görüntüsü boyutu tenant kontrollü → KVKK m.5 veri
  minimizasyonu pozitif etki, DPIA "screenshot_size_preset_enum"
  alanına yeni değer olarak işaretlenmeli.
- `50ef60f` — user_sid doğruluğu → audit trail'de gerçek kullanıcı
  atanması, KVKK m.12 (veri güvenliği) + audit bütünlüğü için kritik.
- `babc88a` — keystroke meta (KVKK m.6 özel nitelik olmayan metadata)
  teslimat oranı → ADR 0013 "default OFF" kuralı değişmedi.
- `ee4696b` — sadece UI temizliği, KVKK etkisi yok.

Deploy sonrası `infra/scripts/verify-audit-chain.sh` zorunlu; checkpoint
farkı ortaya çıkarsa bu runbook referans gösterilerek explain edilir.

---

*Son güncelleme*: 2026-04-14 — Wave 9 Sprint 5 runbook teslimatı.
