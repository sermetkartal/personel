# MaxMind GeoLite2 Kurulum Rehberi

> Bu doküman Personel enricher'ının sunucu tarafı GeoIP zenginleştirmesi
> için MaxMind GeoLite2-City veritabanının kurulumunu anlatır. Müşteri
> ortamında her pilot için operatör bu adımları bir kez yapar; sonrasında
> haftalık `personel-maxmind-download.timer` otomatik günceller.

## 1. Ön Gereksinimler

- MaxMind hesabı (https://www.maxmind.com/en/geolite2/signup)
- Account ID (numerik, ör: `891169`)
- License Key (hesap panelinden üretilir, `Manage License Keys`)
- Host'ta `curl`, `tar`, `sha256sum` (varsayılan olarak Ubuntu'da var)
- Outbound HTTPS erişimi: `download.maxmind.com:443`

## 2. Dizin Hazırlığı

```bash
sudo install -d -o personel -g personel -m 0755 /var/lib/personel/geolite2
```

## 3. EnvironmentFile Oluştur

`/etc/personel/maxmind.env` dosyasını root olarak yaz. İçerik:

```env
# /etc/personel/maxmind.env
# SADECE root okuyabilir, personel grubu okuyabilir — mode 0640
MAXMIND_ACCOUNT_ID=891169
MAXMIND_LICENSE_KEY=xxxxxxxxxxxxxxxx
```

İzinleri sıkılaştır:

```bash
sudo chown root:personel /etc/personel/maxmind.env
sudo chmod 0640         /etc/personel/maxmind.env
```

> **Güvenlik notu**: Bu dosya repo'ya commit EDİLMEZ. MaxMind license
> key'i müşteri kurumunun hesabına aittir; sızıntı durumunda MaxMind
> panelinden revoke + rotate yap.

## 4. İlk İndirme (Manuel)

```bash
sudo systemctl start personel-maxmind-download.service
sudo journalctl -u personel-maxmind-download.service -n 50
```

Başarılı çıktı:

```
maxmind-download: fetching GeoLite2-City tarball…
maxmind-download: fetching GeoLite2-City sha256…
maxmind-download: sha256 verified (abcdef…)
maxmind-download: GeoLite2-City.mmdb updated at /var/lib/personel/geolite2
```

Dosyanın var olduğunu doğrula:

```bash
ls -l /var/lib/personel/geolite2/GeoLite2-City.mmdb
# -rw-r--r-- 1 personel personel ~70M … GeoLite2-City.mmdb
```

## 5. Haftalık Timer'ı Enable Et

```bash
sudo systemctl enable --now personel-maxmind-download.timer
sudo systemctl list-timers | grep maxmind
```

Beklenen çıktı bir sonraki Pazartesi 03:00 ± 30dk'yı gösterir.

## 6. Enricher Config'e mmdb Path Ekle

`apps/gateway/configs/enricher.yaml` (prod override ya da
docker-compose env var):

```yaml
geoip:
  mmdb_path: /var/lib/personel/geolite2/GeoLite2-City.mmdb
```

Docker Compose kullanıyorsan dosyayı container'a mount et:

```yaml
services:
  enricher:
    volumes:
      - /var/lib/personel/geolite2:/var/lib/personel/geolite2:ro
    environment:
      PERSONEL_ENRICHER_GEOIP__MMDB_PATH: /var/lib/personel/geolite2/GeoLite2-City.mmdb
```

Enricher'ı restart et:

```bash
sudo docker compose restart enricher
sudo docker compose logs -f enricher | grep -i geo
```

Başarılı boot log'u:

```
enricher: geoip lookup initialised path=/var/lib/personel/geolite2/GeoLite2-City.mmdb
```

mmdb yoksa veya path hatalıysa enricher warning basar ama çökmez —
GeoIP zenginleştirmesi devreden çıkar, diğer pipeline çalışmaya devam eder.

## 7. Doğrulama

```bash
# ClickHouse'a bir pubic IP içeren network eventi gönderildikten sonra:
docker exec personel-clickhouse clickhouse-client -q \
  "SELECT JSONExtractString(payload, 'geo_country_code') AS cc,
          JSONExtractString(payload, 'geo_city_name') AS city,
          count()
   FROM personel.events_raw
   WHERE event_type LIKE 'network.%' AND occurred_at > now() - INTERVAL 1 HOUR
   GROUP BY cc, city
   ORDER BY count() DESC LIMIT 10"
```

Beklenen: IP'nin karşılık geldiği ülke kodu + şehir adı.

## 8. Hata Senaryoları

| Belirti | Sebep | Çözüm |
|---|---|---|
| `401 Unauthorized` indirmede | Yanlış account id veya license key | MaxMind panelinden license key revoke + yeni anahtar üret, `/etc/personel/maxmind.env` güncelle |
| `SHA256 mismatch` | Kısmi indirme, proxy bozukluğu | Timer'ı elle tekrar tetikle (`systemctl start personel-maxmind-download.service`) |
| Enricher log `geoip lookup disabled` | mmdb path boş veya dosya yok | Adım 3-4'ü tekrarla, docker volume mount'u kontrol et |
| Enricher log `geoip open failed` | mmdb dosyası bozuk / yanlış edition | `MMDB_EDITION` ve dosya boyutunu kontrol et (GeoLite2-City ~70MB) |

## 9. Lisans ve Hukuki Notlar

MaxMind GeoLite2 lisansı **2019'dan itibaren geofeed.maxmind.com üzerinden
abonelik gerektirir** ve şu şartları içerir (2026-04 itibarıyla):

- **İzin verilen**: Kurum içi iş kullanımı, sunucu tarafı log
  zenginleştirme, güvenlik analitiği, fraud detection
- **Yasak**: Veriyi üçüncü taraflara yeniden dağıtmak, ticari olarak
  satmak, API veya ürün olarak yeniden paketlemek

Personel'in kullanımı yalnızca **sunucu tarafı event zenginleştirmesi**
olduğundan GeoLite2 ücretsiz lisansı yeterlidir. Eğer müşteri yüksek
doğruluk ister veya mmdb'yi başka servislerle paylaşmak isterse
**GeoIP2** (paid) lisansına yükseltilmesi gerekir.

VERBİS / KVKK açısından: IP'den ülke/şehir çözümlemesi **kişisel veri
kategorisinde kimlik belirleyici bir artış üretmez** (zaten IP
saklanıyor), ancak `docs/compliance/dpia-sablonu.md`'de "uluslararası
veri transferi" risk maddesi güncellenmelidir: MaxMind sunucusundan
indirme sırasında ABD'ye HTTPS üzerinden hesap ID + license key
iletilir, ancak bu akış sadece veritabanı indirmedir — pilot event
verisi asla MaxMind'a gönderilmez.

## 10. Kaynaklar

- MaxMind download endpoint: https://download.maxmind.com/app/geoip_download
- GeoLite2 dokümantasyonu: https://dev.maxmind.com/geoip/docs/databases
- Personel enricher config: `apps/gateway/configs/enricher.yaml`
- Download script: `infra/scripts/maxmind-download.sh`
- systemd unit: `infra/systemd/personel-maxmind-download.{service,timer}`
