# Runbook — Grafana Dashboards Dağıtımı (Faz 8 #89)

> Hedef kullanıcı: Personel platform operatörü. Bu runbook Grafana
> dashboard'larının (özellikle `personel-uam.json`) nasıl dağıtıldığını,
> her kiracı için hangi kontrollerin yapıldığını ve sorun giderme adımlarını
> açıklar.

## 1. Ön gereklilikler

- `docker compose` tabanlı Personel stack çalışır durumda
- Grafana konteyneri (`personel-grafana`) ayakta
- `ClickHouse` datasource plugin yüklü (`grafana-clickhouse-datasource`)
- `personel_app` CH kullanıcı şifresi `.env`'de:
  `CLICKHOUSE_APP_PASSWORD=<...>`

## 2. Dosya yapısı

```
infra/compose/grafana/
├── provisioning/
│   ├── datasources/
│   │   ├── prometheus.yml          (önceden var)
│   │   └── clickhouse.yaml         ← Faz 8 #89 yeni
│   └── dashboards/
│       └── dashboards.yml          (önceden var — tüm JSON'ları otomatik yükler)
└── dashboards/
    ├── agents-health.json
    ├── capacity.json
    ├── gateway.json
    ├── kvkk-compliance.json
    ├── live-view-audit.json
    └── personel-uam.json            ← Faz 8 #89 yeni
```

## 3. İlk kurulum

### 3.1 Datasource plugin

ClickHouse datasource Grafana core'a dahil değil. İlk kurulumda şu
environment değişkenini `docker-compose.yaml` içindeki `grafana` servisine
ekleyin:

```yaml
environment:
  GF_INSTALL_PLUGINS: grafana-clickhouse-datasource
```

Ardından:

```bash
cd infra/compose
docker compose restart grafana
docker compose logs grafana | grep -i "clickhouse"
# Beklenen çıktı: "installed plugin grafana-clickhouse-datasource vX.Y.Z"
```

### 3.2 Provisioning doğrulama

```bash
docker compose exec grafana ls /etc/grafana/provisioning/datasources/
# Beklenen: clickhouse.yaml + prometheus.yml

docker compose exec grafana ls /var/lib/grafana/dashboards/
# Beklenen: personel-uam.json + diğer dashboard'lar
```

### 3.3 Bağlantı testi

1. Grafana UI'a admin olarak girin: `http://<host>:3000`
2. Configuration → Data sources → ClickHouse
3. "Save & test" → "Data source is working" görmelisiniz
4. Dashboards → Browse → "Personel — UAM Showcase" tıklayın
5. Tenant değişkeni **görünmez** (hide=2), ama sorgular org adını
   `$tenant_id` olarak kullanır

## 4. Yeni kiracı için dashboard erişimi

Grafana'da bir Personel kiracısının panelleri görebilmesi için:

1. **Org oluşturun** (kiracı UUID'si = org adı):
   ```bash
   TENANT_ID="be459dac-1a79-4054-b6e1-fa934a927315"
   curl -sS -u admin:$GRAFANA_PASS \
     -H "Content-Type: application/json" \
     -d "{\"name\":\"$TENANT_ID\"}" \
     http://localhost:3000/api/orgs
   ```

2. **Kiracı admin kullanıcısını ekleyin** (yalnızca bu org'a):
   ```bash
   # Kullanıcı oluşturma
   curl -sS -u admin:$GRAFANA_PASS \
     -H "Content-Type: application/json" \
     -d '{"name":"Kiracı Admin","email":"admin@kiraci.com","login":"kiraci-admin","password":"<güçlü-parola>"}' \
     http://localhost:3000/api/admin/users

   # Org'a Viewer olarak ekleme (Editor yetkisi DUPKI nedeniyle verilmez)
   curl -sS -u admin:$GRAFANA_PASS \
     -H "Content-Type: application/json" \
     -d '{"loginOrEmail":"kiraci-admin","role":"Viewer"}' \
     http://localhost:3000/api/orgs/<org-id>/users
   ```

3. **Dashboard otomatik erişilebilir olur** — provisioning her org'a aynı
   JSON'u yükler; sorgular org adını `$tenant_id` olarak kullanır.

4. **Doğrulama**: yeni kullanıcıyla login olun, "Personel — UAM Showcase"
   dashboard'unu açın. Paneldeki veriler yalnızca bu kiracıya ait olmalı.

## 5. Alert yönetimi

`personel-uam.json` dashboard'u üç alert içerir:

| Alert | Panel | Eşik | Sıklık |
|---|---|---|---|
| Low Productivity Average | Productivity 7d avg | `< 40` | 30m |
| Rising Policy Violations | Policy Violations 7d | `> 100` | 1m |
| DLP Redaction Spike | DLP Redactions sparkline | `> 500/5m` | 1m |

Bu alertler **her org'da ayrı çalışır** — kiracıya özgü eşiklere göre
uyarılmak isterseniz:

1. Org'u seçin
2. Dashboard → Panel → Edit → Alert tab → Thresholds değiştirin
3. Kaydetme: dashboard provisioning read-only olduğu için değişiklik
   kalıcı olmaz. Kalıcı değişiklik için:
   - `personel-uam.json` dosyasını düzenleyin
   - `git commit` → `git push`
   - Operator'a redeploy bildirin

## 6. Sorun giderme

### "No data" görünüyor

1. **Datasource test**: Configuration → Data sources → ClickHouse → Save & test
2. **CH tabloları**: `docker exec personel-clickhouse clickhouse-client -u personel_app -q "SELECT count() FROM personel.events_enriched"`
3. **Kiracı org adı yanlış**: Dashboard'un üstündeki "Query inspector" üzerinden
   interpolated SQL'i kontrol edin. `tenant_id = ''` görürseniz org adı
   düzgün ayarlanmamıştır.
4. **Kiracıda henüz veri yok**: normal; agent enrollment + events flow
   gerekiyor.

### "Permission denied" CH hatası

`personel_app` kullanıcısının sorguladığı tabloda SELECT yetkisi yok.
Beklenen tablo listesi:

```sql
-- infra/compose/clickhouse/init-permissions.sql içinde
GRANT SELECT ON personel.events_enriched TO personel_app;
GRANT SELECT ON personel.employee_signals TO personel_app;
GRANT SELECT ON personel.app_focus_daily TO personel_app;
GRANT SELECT ON personel.user_risk_current TO personel_app;
-- NOT GRANTED: keystroke_content_encrypted, live_view_recordings_encrypted
```

### Kiracı başka bir kiracının verisini görüyor

**KRİTİK KVKK ihlali.** Hemen:

1. Grafana'yı durdurun: `docker compose stop grafana`
2. DPO'ya bildirin (incident playbook)
3. Org adı dashboards query'lerinde doğru interpolasyon oluyor mu
   kontrol edin
4. `personel-uam.json` içindeki her `tenant_id = '$tenant_id'` filtresinin
   yerinde olduğunu doğrulayın
5. Çözümü test ettikten sonra Grafana'yı tekrar başlatın

## 7. İlgili doküman

- `docs/operations/grafana-tenant-isolation.md` — izolasyon mimarisi detayı
- `docs/compliance/kvkk-framework.md` §5 — proportionality
- `infra/compose/grafana/dashboards/personel-uam.json` — panel tanımları
- `infra/compose/grafana/dashboards/kvkk-compliance.json` — DSR / legal hold
