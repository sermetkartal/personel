# NATS Cluster + MinIO Site Replication Runbook (Faz 5 #46 + #48)

> **Hedef kitle**: DPO + DevOps + on-call SRE. Türkçe gövde, kritik komutlar
> İngilizce. Bu runbook iki konuyu birlikte kapsar çünkü Faz 5 Wave 2'de
> ikisi de aynı host çiftinde (vm3 + vm5) deploy ediliyor.

## 1. Kapsam ve mimari özet

Bu çalışma Personel pilot stack'ini iki host'a yayar:

- **vm3 (192.168.5.44)** — birincil host, mevcut 12 servisin tamamı burada.
- **vm5 (192.168.5.32)** — yeni Ubuntu 24.04 host. Sadece iki servis çalışır:
  `personel-nats-02` (Raft cluster ikinci voter) ve `personel-minio-mirror`
  (site replication peer).

### 1.1 NATS JetStream Raft cluster (2 düğümlü)

```
                 ┌─────────────────────────┐
                 │   PersonelCluster       │
                 │   Raft consensus group  │
                 └───────────┬─────────────┘
                             │
              ┌──────────────┴───────────────┐
              │                              │
              ▼                              ▼
   ┌───────────────────┐         ┌───────────────────┐
   │ personel-nats-01  │◄──6222─►│ personel-nats-02  │
   │ vm3 192.168.5.44  │  mTLS   │ vm5 192.168.5.32  │
   │ JS store: 50 GB   │  Raft   │ JS store: 50 GB   │
   │ chachapoly + key  │         │ chachapoly + key  │
   └─────────┬─────────┘         └─────────┬─────────┘
             │                             │
             └─────────────┬───────────────┘
                           │
                  4222 (TLS + JWT)
                           │
              ┌────────────┴────────────┐
              │ Application clients     │
              │ gateway, enricher, api  │
              │ URL pool = [vm3, vm5]   │
              └─────────────────────────┘
```

Streamler `replicas=2` ile çalışır: her stream'in bir kopyası her iki düğümde
durur, Raft commit yazma için iki düğüm arasında çoğunluk ister.

### 1.2 MinIO site replication

```
   ┌──────────────────────────────┐         ┌──────────────────────────────┐
   │ vm3 personel-minio (primary) │         │ vm5 personel-minio-mirror    │
   │ 192.168.5.44:9000            │ async   │ 192.168.5.32:9000            │
   │                              │ replica │                              │
   │ NON-WORM bucket'lar:         │◄───────►│ NON-WORM bucket'lar:         │
   │   screenshots, blobs,        │         │   screenshots, blobs,        │
   │   policies, dsr, livestream, │         │   policies, dsr, livestream, │
   │   logs                       │         │   logs                       │
   │                              │         │                              │
   │ WORM bucket'lar (HARİÇ):     │   X     │ (yok — bilerek kopyalanmaz)  │
   │   audit-worm                 │  X X    │                              │
   │   evidence-worm              │ X X X   │                              │
   └──────────────────────────────┘         └──────────────────────────────┘
            │
            │ ayrı bir off-site backup hattı (backup.sh)
            ▼
   müşteri kontrolündeki cold storage
```

**WORM hariç tutma gerekçesi**: `audit-worm` ve `evidence-worm` Wave 1 #50
tarafından **COMPLIANCE Object Lock** modunda 5 yıl saklamayla oluşturuldu.
MinIO site replication ile Object Lock birlikte temiz çalışmaz (mirror tarafı
kaynağın `RetainUntilDate` damgasını miras alamaz, denetim tek otoriter
WORM kopyası ister, Phase 3.0 evidence locker tek bir
chain-of-custody bekler). Bu iki bucket için off-site yedek `backup.sh`
ayrı kanalıyla yapılır.

## 2. 2-DÜĞÜMLÜ QUORUM UYARISI (NATS)

> Bu cluster **iki voter** içerir. Raft majority kuralı = `floor(N/2)+1 = 2`.
> Yani **iki düğüm de ayakta olmak zorunda**, yoksa JetStream **YAZMA halt
> eder**. Tek düğüm kaybı:
>
> - **Tüm yazma (publish + ack)** durur
> - Mevcut consumer'lar yeni mesaj okuyamaz
> - Read-only `nats stream view` çalışmaya devam edebilir
>
> Bu bir **staging** topolojisi. Üretim için **3. voter** zorunlu — küçük
> bir "arbiter" NATS düğümü (JetStream stream replica sayılmadan sadece
> Raft oyu için). Cust prod cutover öncesi 3. host (vm6) sağlanmalı.

3. düğüm eklenmeden önce **uzun süreli bakım** veya **planlı vm5 reboot**
gerekirse:
1. Önce `nats-cluster-migrate-streams.sh` ile streamleri geçici olarak
   `replicas=1` yap (sadece vm3 üzerinde tutar — komutu `--replicas 1`
   parametresiyle elden çağır)
2. vm5'i durdur, bakım yap, geri al
3. Streamleri tekrar `replicas=2` yap

## 3. Ön gereksinimler

Çalıştırmadan önce şunları doğrula:

- [ ] Wave 1 #47 (`nats-bootstrap.sh`) çalıştırılmış, vm3 üzerinde bu dosyalar
      mevcut:
  - `/etc/personel/nats/operator.jwt`
  - `/etc/personel/nats/resolver/<account_pub>.jwt` (SYS + PersonelMain)
  - `/etc/personel/nats-creds/{gateway,enricher,api}.creds`
  - `/etc/personel/secrets/nats-encryption.key`
- [ ] Wave 1 #50 (`minio-worm-bootstrap.sh`) çalıştırılmış, vm3 üzerinde
      `audit-worm` ve `evidence-worm` bucket'ları COMPLIANCE modunda.
- [ ] vm5'te Ubuntu 24.04 + Docker 29.4 kurulu, root olarak SSH erişimi var.
- [ ] vm5'in DNS'i 1.1.1.1, hostname benzersiz, NTP senkron.
- [ ] vm3 → vm5 ICMP + TCP 6222 + TCP 9000 + TCP 4222 firewall'da açık.
- [ ] vm5 → vm3 TCP 6222 + TCP 9000 firewall'da açık.
- [ ] vm5 host firewall'unda yalnızca yukarıdaki portlar 192.168.5.44/32
      kaynağından kabul edilir; başka kaynaklara kapalı.
- [ ] vm3'teki Vault unsealed ve `tenant_ca` PKI engine ayakta.

## 4. NATS bring-up sırası

> Aşağıdaki komutların **tamamı vm3** üzerinde çalıştırılır, sadece tarball
> aktarımı için kısa bir SSH atılır.

### 4.1 Cluster staging (vm5 bundle hazırla)

```bash
sudo infra/scripts/nats-cluster-bootstrap.sh
```

Bu script:
- Wave 1 prereqlerini doğrular (operator JWT, resolver, encryption key)
- vm5 reachability ICMP probe'u atar
- Vault PKI'den vm5 server cert'i issue eder (CN=`personel-nats-02`,
  IP SAN=`192.168.5.32`)
- Tüm vm5 dosyalarını içeren tarball üretir:
  `/var/lib/personel/cluster-staging/nats-vm5-bundle-<ts>.tar.gz`

### 4.2 Tarball'ı vm5'e kopyala (operator manuel)

```bash
scp /var/lib/personel/cluster-staging/nats-vm5-bundle-<ts>.tar.gz \
    kartal@192.168.5.32:/tmp/

ssh kartal@192.168.5.32 'sudo tar -xzpf /tmp/nats-vm5-bundle-<ts>.tar.gz -C /'
ssh kartal@192.168.5.32 'sudo install -d -m 0700 /var/lib/personel/nats-02/data'
```

### 4.3 vm3'te eski single-node NATS'ı durdur

```bash
cd /home/kartal/personel/infra/compose
docker compose -f docker-compose.yaml \
               -f nats/docker-compose.prod-override.yaml \
               stop nats
```

Bu kısa bir kesinti yaratır (yaklaşık 5-10 saniye). Gateway ve enricher
JetStream durumunu yerel disk consumer'da koruduğu için mesaj kaybı yok.

### 4.4 vm5'te node-02'yi başlat

```bash
ssh kartal@192.168.5.32
cd /opt/personel/nats
sudo docker compose up -d
sudo docker compose logs -f personel-nats-02
```

Logda şunları gör:
- `Server is ready`
- `JetStream cluster bootstrap`
- `route connection ... created` (vm3'e bağlanıyor — ama vm3 kapalı, OK)

### 4.5 vm3'te node-01'i cluster overlay ile başlat

```bash
cd /home/kartal/personel/infra/compose
docker compose -f docker-compose.yaml \
               -f nats/docker-compose.cluster-node1.yaml \
               up -d nats
docker compose logs -f nats
```

Beklenen log:
- `Server is ready`
- `JetStream cluster reset complete`
- `route connection ... 192.168.5.32:6222 created`
- `JetStream cluster new metaleader: nats-01` (veya nats-02)

### 4.6 Cluster oluşumunu doğrula

```bash
nats --server tls://192.168.5.44:4222 \
     --creds /etc/personel/nats-creds/api.creds \
     --tlsca /etc/personel/tls/root_ca.crt \
     server list
```

Çıktıda **iki düğüm** görmelisin:

```
+----------------------+---------+----------+...
| personel-nats-01     | ...     | OK       |
| personel-nats-02     | ...     | OK       |
+----------------------+---------+----------+...
```

### 4.7 Streamleri replicas=2'ye taşı

```bash
sudo infra/scripts/nats-cluster-migrate-streams.sh
```

Beklenen:
- `cluster has 2 active peers — proceeding`
- Her stream için `replicas=1 -> 2` ve sonra `current` raporu

### 4.8 Cluster doğrulama testi

```bash
sudo infra/scripts/nats-cluster-test.sh
```

Beklenen son satır:
```
[nats-cluster-test] PASS: 2-node NATS cluster pub-on-1 / sub-on-2 round trip OK
```

## 5. MinIO bring-up sırası

### 5.1 vm5 mirror için root creds + cert

vm3'teki `/etc/personel/secrets/minio-root.env`'deki `MINIO_ROOT_USER` +
`MINIO_ROOT_PASSWORD` değerleri vm5'e **birebir aynı** olarak kopyalanmalı.
Site replication, root creds eşleşmediğinde authenticate etmez.

```bash
scp /etc/personel/secrets/minio-root.env kartal@192.168.5.32:/tmp/
ssh kartal@192.168.5.32 'sudo install -d -m 0700 /etc/personel/secrets && \
                         sudo mv /tmp/minio-root.env /etc/personel/secrets/ && \
                         sudo chmod 600 /etc/personel/secrets/minio-root.env'
```

vm5 server cert'i Vault PKI'den issue et (CN=`192.168.5.32`):

```bash
# vm3 üzerinden Vault PKI çağrısı
VAULT_TOKEN="$(cat /etc/personel/secrets/vault-root.token)"
curl -sf --cacert /etc/personel/tls/root_ca.crt \
  -H "X-Vault-Token: ${VAULT_TOKEN}" \
  -H "Content-Type: application/json" \
  -X POST -d '{
    "common_name": "192.168.5.32",
    "ip_sans": "192.168.5.32",
    "ttl": "8760h"
  }' \
  https://127.0.0.1:8200/v1/tenant_ca/issue/server-cert \
  > /tmp/vm5-minio-cert.json

jq -r '.data.certificate' /tmp/vm5-minio-cert.json > /tmp/vm5-minio-public.crt
jq -r '.data.private_key' /tmp/vm5-minio-cert.json > /tmp/vm5-minio-private.key

scp /tmp/vm5-minio-public.crt kartal@192.168.5.32:/tmp/public.crt
scp /tmp/vm5-minio-private.key kartal@192.168.5.32:/tmp/private.key
scp /etc/personel/tls/root_ca.crt kartal@192.168.5.32:/tmp/root_ca.crt

ssh kartal@192.168.5.32 '
  sudo install -d -m 0755 /etc/personel/tls/CAs
  sudo install -m 0644 /tmp/public.crt /etc/personel/tls/public.crt
  sudo install -m 0600 /tmp/private.key /etc/personel/tls/private.key
  sudo install -m 0644 /tmp/root_ca.crt /etc/personel/tls/CAs/root_ca.crt
'
```

### 5.2 vm5'te mirror compose'u kur

```bash
scp infra/compose/minio/docker-compose.mirror-vm5.yaml \
    kartal@192.168.5.32:/tmp/docker-compose.yaml

ssh kartal@192.168.5.32 '
  sudo install -d -m 0755 /opt/personel/minio
  sudo mv /tmp/docker-compose.yaml /opt/personel/minio/docker-compose.yaml
  sudo install -d -m 0700 /var/lib/personel/minio-mirror/data
  cd /opt/personel/minio && sudo docker compose up -d
  sudo docker compose logs --tail 50 personel-minio-mirror
'
```

### 5.3 Site replication setup'ı çalıştır (vm3 üzerinden)

```bash
sudo infra/scripts/minio-site-replication-setup.sh
```

Beklenen:
- `Waiting for primary ... ready`
- `Waiting for mirror ... ready`
- `Adding sites to replication config ...  sites added`
- `verification ... both sites present in replication config`
- `Sanity check ... OK: audit-worm on primary, absent on mirror`
- `Sanity check ... OK: evidence-worm on primary, absent on mirror`

### 5.4 Mirror end-to-end test

```bash
sudo infra/scripts/minio-mirror-test.sh
```

Beklenen son satır:
```
[minio-mirror-test] PASS: object PUT on primary appeared identically on mirror
```

## 6. Failover prosedürleri

### 6.1 NATS düğüm kaybı

| Senaryo | Sonuç | Aksiyon |
|---|---|---|
| vm3 NATS down | Yazma halt — quorum kayıp. Mevcut consumer'lar replay edemez. | Acil: vm3 NATS'ı geri getir. Donanım arızası: streamleri vm5 üzerinden read-only kurtar (`nats stream view` ile son state'i çıkar), 3. voter eklenene kadar üretim trafiğini geçici olarak durdur. |
| vm5 NATS down | Yazma halt — quorum kayıp. | vm5'i geri getir. vm5 kalıcı kayıpsa: vm3'te streamleri `replicas=1` yap (manuel `nats stream edit --replicas 1 <stream>` her stream için), trafiği aç, sonra yeni vm5 ile baştan bootstrap et. |
| Network partition vm3 ↔ vm5 | İki taraf da kendini follower zanneder, leader yok. Yazma halt. | Network'ü düzelt. Raft otomatik leader seçer. |
| Tek client connection vm3'e ulaşamıyor | Client URL pool'unu reconnect ile vm5'e yönlendirir. Service degradation yok. | Aksiyon yok — pool design'ı gereği. |

### 6.2 MinIO site kaybı

| Senaryo | Sonuç | Aksiyon |
|---|---|---|
| vm3 MinIO down | Application yazma fail — gateway / enricher screenshot upload eder olamaz. WORM bucket'lar erişilmez. | vm3 MinIO'yu geri getir. **Donanım kaybı**: müşteri DPO ile koordinasyon kur, vm5'teki mirror'dan NON-WORM bucket'ları kurtar (`mc cp --recursive mirror/screenshots primary/screenshots`). WORM bucket'lar kayıp — off-site backup'tan restore et. |
| vm5 MinIO down | Mirror eksilir, primary normal çalışır. Replication bekler. | vm5 MinIO'yu geri getir. Replication otomatik yetişir. Donanım kaybı: yeni vm5'i bootstrap et, `mc admin replicate add` baştan. |
| Network partition vm3 ↔ vm5 | Primary normal, mirror eskimiş kalır. | Network'ü düzelt. Replication otomatik yetişir. |

## 7. Rollback

### 7.1 NATS — single-node'a geri dön

```bash
# vm5 üzerinde node-02'yi durdur
ssh kartal@192.168.5.32 'cd /opt/personel/nats && sudo docker compose down'

# vm3 üzerinde streamleri replicas=1 yap (her stream için manuel)
nats --server tls://192.168.5.44:4222 \
     --creds /etc/personel/nats-creds/api.creds \
     --tlsca /etc/personel/tls/root_ca.crt \
     stream edit --replicas 1 --force events_raw
# ... ve diğer 4 stream için tekrarla

# vm3'te node-01'i durdur, prod-override ile single-node moduna geri dön
cd /home/kartal/personel/infra/compose
docker compose -f docker-compose.yaml -f nats/docker-compose.cluster-node1.yaml down nats
docker compose -f docker-compose.yaml -f nats/docker-compose.prod-override.yaml up -d nats
```

### 7.2 MinIO — site replication kaldır

```bash
# vm3'te mc admin replicate rm
docker run --rm --network host \
  -e "MC_HOST_primary=https://${MINIO_ROOT_USER}:${MINIO_ROOT_PASSWORD}@192.168.5.44:9000" \
  -v /etc/personel/tls/root_ca.crt:/root/.mc/certs/CAs/root_ca.crt:ro \
  minio/mc:latest --insecure admin replicate rm --all --force primary

# vm5'te mirror'ı durdur
ssh kartal@192.168.5.32 'cd /opt/personel/minio && sudo docker compose down -v'
```

> **DİKKAT**: vm5 mirror volume'u silinirse oradaki mirror nesneleri de
> kayıp. WORM bucket'lar zaten orada değildi, NON-WORM bucket'lar primary
> üzerinde saklı.

## 8. KVKK ve güvenlik notları

### 8.1 NATS at-rest encryption

JetStream `chachapoly` cipher'ı **iki düğümde de** aktif. Encryption key
(`/etc/personel/secrets/nats-encryption.key`) iki düğüm arasında **byte
identical** olmak zorunda — `nats-cluster-bootstrap.sh` bunu garanti eder
(aynı key dosyasını tarball'a koyar). Key rotation Phase 2 işidir;
şimdiki küçük disaster recovery prosedürü Wave 1 `nats-bootstrap.sh
--force` ile yapılır ama bu **tüm JetStream mesajlarını okunaksız
yapar** ve replay imkansız hale gelir.

### 8.2 NATS cluster gossip mTLS

Cluster routes kanalı `verify=true` mTLS kullanır — sahte bir peer
operator JWT'yi bilse bile Vault PKI tenant CA tarafından imzalanmamış
sertifikası nedeniyle gossip channel'a katılamaz. Wave 1 prod-override
single node olduğu için bu doğrulamayı `false` bırakmıştı; cluster
overlay onu `true`'ya çeker.

### 8.3 MinIO — replicated metadata kapsamı

Site replication şunları kopyalar:
- IAM user, group, policy
- Service account (anahtar ile beraber)
- Bucket lifecycle, versioning, encryption (SSE-KMS dahil)
- Object content + tag + custom metadata

KVKK perspektifinden: **kişisel veri içeren her NON-WORM bucket** vm5'e
de yansır. Bu, müşteri DPIA dökümanına eklenmesi gereken bir veri akışı
değişikliğidir — vm5 host'u veri saklama envanterine dahil edilmeli.
WORM bucket'lar dahil olmadığı için audit chain ve evidence locker
chain-of-custody bozulmaz.

### 8.4 Kritik dosya konumları

| Dosya | vm3 | vm5 |
|---|---|---|
| NATS operator JWT | `/etc/personel/nats/operator.jwt` | aynı (tarball'dan) |
| NATS resolver dir | `/etc/personel/nats/resolver/` | aynı (tarball'dan) |
| NATS encryption key | `/etc/personel/secrets/nats-encryption.key` | aynı bytes (tarball'dan) |
| NATS server cert | `/etc/personel/tls/nats.crt` (CN=192.168.5.44) | `/etc/personel/tls/nats.crt` (CN=192.168.5.32) |
| MinIO root creds | `/etc/personel/secrets/minio-root.env` | aynı (manuel scp) |
| MinIO server cert | `/etc/personel/tls/public.crt` | `/etc/personel/tls/public.crt` (vm5'e özel) |
| Root CA | `/etc/personel/tls/root_ca.crt` | aynı |

## 9. Monitoring ve alerting (todo)

Wave 2 sonrası Wave 3'te (Faz 5 #56-61) eklenecek:
- Prometheus alert: `personel_nats_jetstream_cluster_size < 2`
- Prometheus alert: `personel_nats_stream_replicas < 2 for any stream`
- Prometheus alert: `minio_replication_pending_count > 0 for 5m`
- Prometheus alert: `minio_replication_failed_count > 0`

## 10. Hızlı referans — script özet

| Script | Çalışır | Amaç |
|---|---|---|
| `nats-cluster-bootstrap.sh` | vm3 | vm5 sertifikası + bundle tarball üret |
| `nats-cluster-migrate-streams.sh` | vm3 | Stream'leri replicas=2'ye taşı |
| `nats-cluster-test.sh` | vm3 | Pub-on-1 / sub-on-2 round trip doğrula |
| `minio-site-replication-setup.sh` | vm3 | Site replication ekle + WORM hariç tut |
| `minio-mirror-test.sh` | vm3 | 1 MiB obje upload + mirror'da SHA256 doğrula |

---

*Versiyon 1.0 — Faz 5 Wave 2 #46 + #48 scaffold. Phase 1 staging-grade
2-düğüm topolojisi. Üretim için 3. NATS voter ve full Prometheus alert
seti gerekli.*
