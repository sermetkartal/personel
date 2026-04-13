# NATS Üretim Yetkilendirme Geçişi — Operasyonel Runbook

> **Hedef kitle**: Personel platform DevOps / SRE  
> **Süre**: ~30 dakika (kesintisiz, doğru sırada)  
> **Kapsam**: Faz-1 dev NATS (auth yok) → Faz-5 üretim NATS (operator JWT + JetStream at-rest encryption)  
> **Roadmap madde**: #47

Bu runbook, Personel platformundaki NATS sunucusunun açık (auth-less) Faz-1 yapılandırmasından operator-mode JWT + NKey + at-rest encryption kullanan üretim yapılandırmasına geçirilmesi adımlarını açıklar.

---

## 1. Önkoşullar

Tüm önkoşullar Ubuntu pilot makinesinde (`192.168.5.44`) sağlanmalıdır.

- [ ] `nsc` CLI kurulmuş (>= 2.10) — `https://github.com/nats-io/nsc`
- [ ] `openssl` binary mevcut (zaten gelir)
- [ ] `/etc/personel/tls/{nats.crt,nats.key,root_ca.crt}` Vault PKI tarafından üretilmiş
- [ ] Çalışan NATS instance'ı (Faz-1 dev profili)
- [ ] gateway, enricher, api servislerinin son durumda sağlıklı olması (geçişten önce baseline)
- [ ] **Bakım penceresi** ilan edilmiş — geçiş sırasında olay ingest'i 1-3 dakika kesintiye uğrar

---

## 2. Bootstrap — Operator + Account + User üretimi

```bash
sudo /home/kartal/personel/infra/scripts/nats-bootstrap.sh
```

Script idempotent çalışır. İlk çalıştırmada şu nesneleri üretir:

| Nesne | Yol | Mod |
|---|---|---|
| Operator JWT | `/etc/personel/nats/operator.jwt` | 0644 |
| Resolver JWT'leri | `/etc/personel/nats/resolver/*.jwt` | 0600 |
| gateway-publisher creds | `/etc/personel/nats-creds/gateway.creds` | 0600 |
| enricher-consumer creds | `/etc/personel/nats-creds/enricher.creds` | 0600 |
| api-controlplane creds | `/etc/personel/nats-creds/api.creds` | 0600 |
| JetStream at-rest key | `/etc/personel/secrets/nats-encryption.key` | 0600 |

> **DİKKAT**: Encryption key tek-yönlüdür. Bir kez üretildikten sonra **kaybolursa JetStream depolaması okunamaz hale gelir**. Yedek alın (`gpg --symmetric` ile) ve felaket kurtarma kasasında saklayın.

### Kullanıcı izinleri (önemli)

| Kullanıcı | Yayın (publish) | Abonelik (subscribe) |
|---|---|---|
| `gateway-publisher` | `events.raw.>`, `events.sensitive.>`, `agent.health.>`, `pki.events.>` | _(yok)_ |
| `enricher-consumer` | `live_view.>` | `events.>`, `agent.health.>` |
| `api-controlplane` | `policy.v1`, `policy.v1.>`, `live_view.>` | `agent.health.>`, `live_view.>` |

Bu ayrım üç saldırı vektörünü kapatır:

1. **Sızıntılı agent**: gateway creds çalınırsa saldırgan ne policy yayınlayabilir, ne canlı izleme komutu gönderebilir, ne de sensitive bucket'tan event okuyabilir. Sadece kendi event'lerini yayınlayabilir.
2. **Sızıntılı enricher**: enricher creds çalınırsa saldırgan event üretemez (ham olay ingest'ini taklit edemez). Sadece tüketebilir.
3. **Sızıntılı api**: api creds çalınırsa saldırgan ham olay yayınlayamaz. Yalnızca control plane subject'lerine erişebilir; bunlar zaten audit log'a düşer.

---

## 3. Anlık doğrulama (compose'a bağlanmadan)

NATS'i prod conf ile **manuel** başlatıp tek kullanıcının çalışıp çalışmadığını görmek için:

```bash
docker run --rm -it \
  -v /etc/personel/nats:/etc/personel/nats:ro \
  -v /etc/personel/tls:/etc/personel/tls:ro \
  -v /etc/personel/secrets/nats-encryption.key:/etc/personel/secrets/nats-encryption.key:ro \
  -v $(pwd)/infra/compose/nats/nats-server.prod.conf:/etc/nats/nats-server.conf:ro \
  --network personel_default \
  --name nats-test \
  -p 4222 \
  nats:2.10-alpine \
  -c /etc/nats/nats-server.conf
```

Başka bir terminalden:

```bash
nats --creds /etc/personel/nats-creds/gateway.creds \
     --tlsca /etc/personel/tls/root_ca.crt \
     --server tls://localhost:4222 \
     pub events.raw.smoke '{"hello":"world"}'
```

Beklenen çıktı: `Published 17 bytes to events.raw.smoke`.

Şunu da test edin (yetkisiz subject — fail etmeli):

```bash
nats --creds /etc/personel/nats-creds/gateway.creds \
     --tlsca /etc/personel/tls/root_ca.crt \
     --server tls://localhost:4222 \
     pub policy.v1 '{}'
# Beklenen: nats: error: nats: permission violation
```

Test container'ı durdurun (`docker stop nats-test`) — gerçek geçiş aşağıda.

---

## 4. Geçiş sırası (kritik)

> **Servisleri yanlış sırada yeniden başlatırsanız event kaybı yaşarsınız.** Aşağıdaki sıra olay akışını korur:

### 4.1 Stack'i prod override ile başlat

```bash
cd /home/kartal/personel/infra/compose
docker compose \
  -f docker-compose.yaml \
  -f nats/docker-compose.prod-override.yaml \
  up -d nats
```

NATS'in yeni JWT auth ile geldiğini doğrulayın:

```bash
docker compose logs nats | grep -E "Operator|System Account|JetStream"
```

Beklenen log satırları:
- `Trusted Operators` (operator JWT yüklendi)
- `System Account: SYS`
- `Starting JetStream` + `Encryption: enabled`

### 4.2 Enricher'ı yeniden başlat (önce)

Enricher önce başlatılır çünkü tüketicidir. Yeni bağlantı kurarken JetStream ack mekanizması ile mevcut backlog'u kaybetmeden devam edebilir.

```bash
docker compose \
  -f docker-compose.yaml \
  -f nats/docker-compose.prod-override.yaml \
  up -d enricher
```

Logları izleyin:

```bash
docker compose logs -f enricher | grep -E "nats|connected|consumer"
```

### 4.3 Gateway'i en son yeniden başlat

Gateway en son başlatılır çünkü ham event üreticisidir. Önce enricher hazır olmalı ki ürettiği event'ler ack alabilsin.

```bash
docker compose \
  -f docker-compose.yaml \
  -f nats/docker-compose.prod-override.yaml \
  up -d gateway
```

### 4.4 API'yi yeniden başlat

```bash
docker compose \
  -f docker-compose.yaml \
  -f nats/docker-compose.prod-override.yaml \
  up -d api
```

API'nin policy publish ve heartbeat consume yeteneğini doğrulayın:

```bash
curl -k https://localhost:8000/healthz
docker compose logs api | grep -i "nats"
# Beklenen: "nats client connected" + creds_file referansı
```

---

## 5. Smoke test

Windows test client (`192.168.5.30`) üzerinden bir agent'ı yeniden bağlayın ve event akışını izleyin:

```bash
# Ubuntu üzerinde:
nats --creds /etc/personel/nats-creds/enricher.creds \
     --tlsca /etc/personel/tls/root_ca.crt \
     --server tls://localhost:4222 \
     stream report
```

`EVENTS` stream'inde mesaj sayısının arttığını görmelisiniz.

---

## 6. Geri alma (rollback)

Geçiş sırasında bir şey ters giderse:

```bash
cd /home/kartal/personel/infra/compose
docker compose -f docker-compose.yaml up -d nats enricher gateway api
```

Bu, override dosyasını devre dışı bırakır ve Faz-1 dev konfigürasyonuna döner. Bootstrap script'inin ürettiği creds dosyaları yerinde kalır — bir sonraki denemede tekrar kullanılır.

> **Önemli**: JetStream encryption key'i bir kez aktive edilmişse rollback **veri kaybına** yol açar (eski JetStream depolaması anahtarsız okunamaz). Bu nedenle önce §3'teki manuel test ile encryption'ın çalıştığını teyit edin, sonra §4'e geçin.

---

## 7. Anahtar rotasyonu (yıllık)

Operator + user JWT'leri yılda bir kez (veya bir creds sızıntısı şüphesi varsa) rotate edilmelidir:

```bash
sudo /home/kartal/personel/infra/scripts/nats-bootstrap.sh --force
```

Bu komut tüm eski JWT'leri geçersiz kılar ve yeni creds dosyaları üretir. Sonrasında §4'teki sırayı tekrar uygulayın.

JetStream encryption key rotasyonu daha karmaşıktır — NATS 2.10 in-place rekey'i desteklemez. Anahtarı değiştirmek için stream'i export edin, yeni anahtarla yeni stream oluşturun, replay edin. Bu prosedür ayrı bir runbook'tadır (henüz yazılmadı — Faz-5 borç listesinde).

---

## 8. Audit izi

Bootstrap script çalıştırılırken aşağıdaki audit kayıtları otomatik düşmez (script Vault auth kullanmaz). Manuel olarak audit log'a kaydedin:

```bash
PGPASSWORD="$POSTGRES_PASSWORD" psql -h localhost -U app_admin_api -d personel -c "
INSERT INTO audit.audit_log (tenant_id, actor, action, target, payload, created_at)
VALUES (
  (SELECT id FROM tenants LIMIT 1),
  'devops:$USER',
  'system.nats_auth_migration',
  'nats',
  '{\"phase\":\"5\",\"item\":47,\"runbook\":\"docs/operations/nats-prod-auth-migration.md\"}'::jsonb,
  now()
);
"
```

Bu kayıt SOC 2 Type II observation window'unda CC8.1 (change authorization) kontrolü için gereklidir.

---

*Versiyon 1.0 — 2026-04-13 — Faz 5 madde #47 scaffold*
