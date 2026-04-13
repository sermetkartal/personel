# Grafana — Kiracı İzolasyonu (Faz 8 #89)

> Bu belge Personel platformunun Grafana panellerinde çok kiracılı güvenliği
> nasıl sağladığını açıklar. Kısa cevap: **bir Grafana organizasyonu = bir
> Personel kiracısı**. Panel sorguları `tenant_id = $tenant_id` filtresiyle
> parametrelenir; `$tenant_id` Grafana org adından okunur.

## 1. Neden Grafana-seviye izolasyon?

ClickHouse `personel_app` kullanıcısı `personel.*` üzerinde SELECT yetkisine
sahiptir ve kiracı filtrelemesi yapmaz. Bu bilinçli bir karar:

- Uygulama katmanı (Admin API) zaten her sorguda `tenant_id = $1` gönderiyor
  ve row-level security'yi o katmanda yapıyor.
- Grafana sorgularını reverse-proxy'den (Admin API) çevirmek pratik değil
  (Grafana native datasource gerektiriyor). O yüzden Grafana'yı doğrudan CH'a
  bağlıyoruz ama **panel sorgusu seviyesinde** izolasyon uyguluyoruz.
- İkinci savunma: `personel_app` kullanıcısının raw keystroke content
  tablosuna (`personel.keystroke_content_encrypted`) `SELECT` yetkisi YOK.
  Grafana bu tabloya baksa bile (ki panellerde yok) KVKK m.6 değişmezi
  kırılmaz.

## 2. Org ↔ Kiracı eşlemesi

### Operator adımı (yeni kiracı eklerken)

1. Yeni bir Grafana org'u oluşturun; org **adı** kesinlikle kiracı UUID'si
   olsun (`be459dac-1a79-4054-b6e1-fa934a927315`):

   ```bash
   curl -sS -u admin:$GRAFANA_ADMIN_PASSWORD \
     -H "Content-Type: application/json" \
     -d '{"name":"be459dac-1a79-4054-b6e1-fa934a927315"}' \
     http://grafana:3000/api/orgs
   ```

2. Org ID'yi not edin (`{"orgId":5}`). Aynı `personel-uam` dashboard'u o
   org'da otomatik provision edilecek çünkü `dashboards.yml` tüm org'lar
   için geçerli.

3. Admin kullanıcıyı / SSO grup eşlemesini bu yeni org'a bağlayın. Kiracının
   kendi IT yöneticisi yalnızca kendi org'una erişmeli — **asla** birden fazla
   org'a.

4. (Opsiyonel) Org için `Viewer` varsayılan rolü atayın. Editör yetkisi
   yalnızca Personel operatöründe kalmalı.

### Dashboard değişken akışı

`personel-uam.json` dashboard'unda şu template değişken tanımlıdır:

```json
{
  "name": "tenant_id",
  "type": "constant",
  "hide": 2,
  "query": "${__org.name}"
}
```

- `__org.name` Grafana'nın built-in değişkeni — o anki org adını döner.
- `hide: 2` → değişken panel toolbar'ında **görünmez** (kullanıcı
  değiştiremez).
- `type: constant` → sorgu interpolasyonuna yazım hatası olmadan düz string
  olarak girer.

Her ClickHouse sorgusu şu şekilde filtrelenir:

```sql
SELECT ...
FROM personel.events_enriched
WHERE tenant_id = '$tenant_id'
  AND event_time >= now() - INTERVAL 1 HOUR
```

`$tenant_id` Grafana datasource proxy katmanında, SQL'e gönderilmeden önce
değiştirilir. Kullanıcı bir paneli "Explore" moduna aldığında bile değişken
otomatik bağlanır — elle `SELECT ... FROM personel.events_enriched` yazarsa
filtresiz sorgu CH'a ulaşır, bu yüzden **Explore yetkisi paylaşılmamalı**.

## 3. Kontrol listesi (operatör)

- [ ] Her Personel kiracısı için ayrı bir Grafana org oluşturuldu
- [ ] Org adı **tam olarak** kiracı UUID'si (ne eksik ne fazla boşluk)
- [ ] Kiracı admin kullanıcıları yalnızca kendi org'larına üye
- [ ] `personel_app` CH kullanıcı şifresi `secureJsonData.password` olarak
      environment değişkeninden besleniyor (commit edilmemiş)
- [ ] Dashboard Explore yetkisi yalnızca Personel operatörüne açık
- [ ] `personel_app` CH rolünün yalnızca `personel.events_enriched`,
      `personel.employee_signals`, `personel.app_focus_daily`,
      `personel.user_risk_current` tablolarında `SELECT` yetkisi var
- [ ] `keystroke_content_encrypted` + `live_view_recordings_encrypted`
      tablolarında `personel_app` kullanıcısına **hiçbir** izin verilmedi

## 4. Bilinen sınırlamalar

- Grafana'nın Explore panelinde SQL düzenleme yetkisi olan kullanıcı
  `$tenant_id` filtresini kaldırabilir. Bu yüzden Explore yetkisi Viewer
  rolüne verilmemeli.
- Datasource `editable: false` olarak provision ediliyor; operatörün
  datasource'u UI'dan değiştirmesi mümkün değil. Değişiklikler bu YAML
  üzerinden commit edilip redeploy ile uygulanır.
- Grafana alert'leri org bazlı çalışır; bir alert'in hangi kiracıya ait
  olduğunu `tenant_id` label'ı üzerinden ayırt edin — alert rule'larını
  org-scoped tanımlayın.

## 5. Referanslar

- `infra/compose/grafana/provisioning/datasources/clickhouse.yaml`
- `infra/compose/grafana/dashboards/personel-uam.json`
- `infra/runbooks/grafana-dashboards.md` — dashboard deployment
- `docs/compliance/kvkk-framework.md` §5 proportionality
- Grafana built-in variables:
  <https://grafana.com/docs/grafana/latest/dashboards/variables/>
