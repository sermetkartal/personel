# OpenSearch 2-node + Keycloak 2-node HA Runbook

> **Faz 5 — Madde 51 + 52**
> **Hedef**: OpenSearch ve Keycloak için vm3 (192.168.5.44) + vm5 (192.168.5.32) üzerinde iki-node HA topoloji kurmak.
> **Sahibi**: SRE / DevOps — DPO bilgilendirilir (KVKK m.12 uyarınca veri replikasyonu duyurulmalı)
> **Bekleme süresi**: ~45 dakika (her iki servis için toplam)

---

## 1. Mimari

### OpenSearch 2-node cluster

```
                 +------------------------+
  gateway +----> | opensearch-01 (vm3)    |  <--- HTTP 9200 (intra-stack)
  enricher +---> |  - master + data       |
  api       |    |  - heap 1 GB           |
            |    +----------+-------------+
            |               | transport 9300 (LAN, mTLS)
            |               v
            |    +------------------------+
            +--> | opensearch-02 (vm5)    |
                 |  - master + data       |
                 |  - heap 1 GB           |
                 +------------------------+
```

- Her iki node master + data rolünde (Faz 1 için basit topoloji)
- `cluster.initial_master_nodes = [opensearch-01, opensearch-02]` ilk bootstrap için
- Transport (9300) vm5'e LAN üzerinden açık; mTLS Vault PKI server-cert ile
- Shards: her index için `number_of_shards=2`, `number_of_replicas=1` — yani her shard'ın bir kopyası diğer node'da
- Heap gerekçesi: vm5'te 7.7 GB RAM var; diğer servisler + OS için ~3 GB bırakıldıktan sonra OpenSearch için 1 GB heap + ~500 MB off-heap (Lucene direct buffers, segment cache) rahat sığar
- Node-02 tek başına compose olarak çalışıyor; vm3 ana stack'e bir overlay ile ekleniyor

### Keycloak 2-node HA

```
   console +--+
   portal  +--+
   api     +--+---> nginx/load balancer (Faz 2) <---+
                                                    |
                 +------------------------+         |
                 | keycloak-01 (vm3)      | <-------+  Faz 1: API ve UI'lar
                 |  - Infinispan + JDBC   |            node-01'e pin'lenir
                 |    PING2               |
                 |  - heap 1.5 GB         |            node-02 sıcak bekleme
                 +----------+-------------+            (Faz 2: round-robin)
                            | JGroups 7800 (LAN, TLS)
                            v
                 +------------------------+
                 | keycloak-02 (vm5)      |
                 |  - Infinispan + JDBC   |
                 |    PING2               |
                 +-----------+------------+
                             |
                             v
              +---------------------------------+
              | Postgres (vm3)                  |
              |  - personel_keycloak (schema)   |
              |  - jgroups_ping (discovery)     |
              +---------------------------------+
```

- Her iki node ortak Postgres `keycloak` database'ine bağlanır (vm3 üzerinde)
- Cluster keşfi: **JDBC_PING2** — node'lar `jgroups_ping` tablosuna transport adreslerini yazar, birbirlerinin girişlerini okur. UDP multicast YOK (on-prem LAN'larda sık kapalı), harici DNS bağımlılığı YOK
- Transport: TCP port 7800 (vm5'e LAN üzerinden açık), TLS Vault PKI server-cert ile
- Infinispan distributed caches: `sessions`, `authenticationSessions`, `offlineSessions`, `clientSessions`, `offlineClientSessions`, `loginFailures`, `actionTokens`, `work` — hepsi `owners=2` yani her giriş iki node'da da var
- Faz 1 akış modeli: API ve UI'lar node-01'e doğrudan pin'lenir; node-02 hot standby olarak Infinispan replikalarını alır ve node-01 düşerse anında hazır olur
- Faz 2'de önüne nginx/envoy round-robin LB konacak (bu runbook kapsamı dışında)

---

## 2. Ön koşullar

- [ ] Vault initialized + unsealed; Vault PKI mount `pki`, rol `server-cert` mevcut
- [ ] `infra/scripts/ca-bootstrap.sh` çalıştırılmış — `/etc/personel/tls/tenant_ca.crt` var
- [ ] vm3 üzerinde Docker + Compose v2 + `jq` + `openssl` + `vault` CLI + `curl`
- [ ] vm5 üzerinde Docker Engine 29+, 7.7 GB RAM, fresh Ubuntu 24.04
- [ ] vm3 → vm5 TCP erişimi (22, 9300 OpenSearch, 7800 JGroups Keycloak)
- [ ] vm5 → vm3 TCP erişimi (5432 Postgres, 8200 Vault, 9300, 7800)
- [ ] Postgres container `personel-postgres` ayakta, `app_keycloak` kullanıcı mevcut
- [ ] Mevcut single-node `personel-opensearch` ve standalone `personel-keycloak` çalışıyor (export için gerekli — migrasyon sırasında durdurulacak)

---

## 3. OpenSearch bring-up (Madde 51)

### 3.1 Bootstrap script

```bash
sudo VAULT_TOKEN=hvs.xxxx \
  /home/kartal/personel/infra/scripts/opensearch-cluster-bootstrap.sh
```

Script şunları yapar:
1. Vault PKI'dan `opensearch-01.crt` ve `opensearch-02.crt` üretir (CN + SAN hem hostname hem IP içerir)
2. `/var/lib/personel/staging/opensearch-cluster/opensearch-cluster-node2-bundle.tar.gz` oluşturur
3. Operatörün izleyeceği run sırasını ekrana basar

**Idempotent**: Sertifika > 7 gün geçerliyse yeniden üretmez. `--force` ile zorlanabilir.

### 3.2 Eski single-node'u durdur

```bash
cd /home/kartal/personel/infra/compose
docker compose stop opensearch
```

Veri dizinini yeni volume'a kopyala (sıcak başlangıç):

```bash
sudo mkdir -p /var/lib/personel/opensearch-01/data
sudo rsync -aHAX --numeric-ids \
  /var/lib/personel/opensearch/data/ \
  /var/lib/personel/opensearch-01/data/
sudo chown -R 1000:1000 /var/lib/personel/opensearch-01/data
```

### 3.3 Node-01'i başlat

```bash
cd /home/kartal/personel/infra/compose
docker compose \
  -f docker-compose.yaml \
  -f opensearch/docker-compose.cluster-node1.yaml \
  up -d opensearch-01

docker logs -f personel-opensearch-01
# 'started' logu gelene kadar bekle (~90 saniye)
```

### 3.4 Bundle'ı vm5'e kopyala

```bash
scp /var/lib/personel/staging/opensearch-cluster/opensearch-cluster-node2-bundle.tar.gz \
    kartal@192.168.5.32:/tmp/
```

### 3.5 vm5 üzerinde node-02'yi başlat

```bash
ssh kartal@192.168.5.32

sudo mkdir -p /etc/personel/tls /var/lib/personel/opensearch-02/data
sudo chown -R 1000:1000 /var/lib/personel/opensearch-02/data
sudo tar -C /tmp -xzf /tmp/opensearch-cluster-node2-bundle.tar.gz
sudo install -m 0640 /tmp/tls/*              /etc/personel/tls/
sudo install -m 0644 /tmp/tls/tenant_ca.crt  /etc/personel/tls/

mkdir -p ~/personel-opensearch-node2
cp /tmp/compose/* ~/personel-opensearch-node2/
cd ~/personel-opensearch-node2

cat > .env <<'EOF'
OPENSEARCH_ADMIN_PASSWORD=<vm3-ile-aynı-parola>
EOF

docker compose -f docker-compose.cluster-node2.yaml up -d
docker logs -f personel-opensearch-02
```

### 3.6 Cluster yellow bekle + reindex

vm3 üzerinde:

```bash
curl -sku admin:$OPENSEARCH_ADMIN_PASSWORD \
  'https://127.0.0.1:9200/_cluster/health?wait_for_status=yellow&timeout=60s'
```

Mevcut indekslerin replika sayısını 1'e yükselt:

```bash
OPENSEARCH_ADMIN_PASSWORD=... \
  /home/kartal/personel/infra/scripts/opensearch-cluster-reindex.sh
```

### 3.7 Doğrulama

```bash
OPENSEARCH_ADMIN_PASSWORD=... \
  /home/kartal/personel/infra/scripts/opensearch-cluster-test.sh
```

Script:
- node-01'e test dokümanı yazar
- node-02'den (192.168.5.32:9200) aynı dokümanı okur
- Cluster status + shard dağılımını raporlar

Çıkış kodu 0 = geçti.

---

## 4. Keycloak bring-up (Madde 52)

### 4.1 Realm export + bootstrap

```bash
sudo VAULT_TOKEN=hvs.xxxx \
  /home/kartal/personel/infra/scripts/keycloak-ha-bootstrap.sh
```

Script:
1. Pre-flight (Vault, Postgres, mevcut container, vm5 reachability)
2. Postgres `keycloak` database yoksa oluşturur
3. `keycloak-01.crt` + `keycloak-02.crt` Vault PKI'dan alır
4. Eski standalone container'dan `personel` realm'ini `kc.sh export` ile alır ve `realm-personel.exported.json` olarak kaydeder
5. vm5 bundle'ı `/var/lib/personel/staging/keycloak-ha/` altında hazırlar
6. Run sıralamasını ekrana basar

**Idempotent**: Sertifikalar > 7 gün geçerliyse skip; database yoksa create, varsa skip; export her çağrıda timestamped dizine yazılır.

### 4.2 Eski standalone container'ı durdur

```bash
docker stop personel-keycloak || true
docker rm   personel-keycloak || true
```

### 4.3 Node-01'i başlat

```bash
cd /home/kartal/personel/infra/compose
docker compose \
  -f docker-compose.yaml \
  -f keycloak/docker-compose.ha-node1.yaml \
  up -d keycloak-01

docker logs -f personel-keycloak-01
# 'Keycloak 25.x.x started' logu gelene kadar bekle (~120 saniye)
```

JGroups view'ı kontrol et:

```bash
docker logs personel-keycloak-01 2>&1 | grep -E 'ISPN000094|view:' | tail -5
```

Tek üye olarak görünmeli: `view: [keycloak-01|0] (1)`.

### 4.4 Bundle'ı vm5'e kopyala ve node-02'yi başlat

```bash
scp /var/lib/personel/staging/keycloak-ha/keycloak-ha-node2-bundle.tar.gz \
    kartal@192.168.5.32:/tmp/

ssh kartal@192.168.5.32

sudo mkdir -p /etc/personel/tls
sudo tar -C /tmp -xzf /tmp/keycloak-ha-node2-bundle.tar.gz
sudo install -m 0640 /tmp/tls/*              /etc/personel/tls/
sudo install -m 0644 /tmp/tls/tenant_ca.crt  /etc/personel/tls/

mkdir -p ~/personel-keycloak-node2
cp /tmp/compose/* ~/personel-keycloak-node2/
cd ~/personel-keycloak-node2

cat > .env <<'EOF'
KEYCLOAK_DB_USER=app_keycloak
KEYCLOAK_DB_PASSWORD=<vm3-ile-aynı-parola>
KEYCLOAK_DB_URL=jdbc:postgresql://192.168.5.44:5432/keycloak
KEYCLOAK_HOSTNAME=keycloak.personel.internal
EOF

docker compose -f docker-compose.ha-node2.yaml up -d
docker logs -f personel-keycloak-02
```

### 4.5 JGroups view birleşmesini doğrula

vm3 üzerinde:

```bash
docker logs personel-keycloak-01 2>&1 | grep -E 'ISPN000094|view:' | tail -3
```

Beklenen çıktı:

```
view: [keycloak-01|1, keycloak-02|1] (2)
```

### 4.6 Realm import kontrolü

Node-01 başlarken `--import-realm` ile `realm-personel.json` zaten import etti. Veritabanı bazlı olduğu için node-02 ayrıca import yapmaz — direkt tablolardan okur.

Hızlı kontrol:

```bash
curl -s http://127.0.0.1:8080/realms/personel/.well-known/openid-configuration \
  | jq -r '.issuer'
```

Beklenen: `https://keycloak.personel.internal/realms/personel`.

### 4.7 HA doğrulama

```bash
KEYCLOAK_NODE1_URL=http://127.0.0.1:8080 \
KEYCLOAK_NODE2_URL=http://192.168.5.32:8080 \
KEYCLOAK_NODE1_MGMT_URL=http://127.0.0.1:9000 \
KEYCLOAK_NODE2_MGMT_URL=http://192.168.5.32:9000 \
KEYCLOAK_ADMIN_USER=admin \
KEYCLOAK_ADMIN_PASSWORD=admin123 \
  /home/kartal/personel/infra/scripts/keycloak-ha-test.sh
```

Script node-01'den admin token alır, aynı token'ı node-02'ye doğrudan gönderir ve `/admin/realms` + `userinfo` çağrılarının başarılı olduğunu doğrular. Bu, Infinispan session replikasyonunun gerçekten çalıştığını kanıtlar.

---

## 5. Failover davranışı

### OpenSearch — tek node kaybı

- Cluster status **green → yellow**; sorgular ve yazmalar ÇALIŞMAYA DEVAM EDER (diğer node primary + replica shard'ları hâlâ servis edebilir)
- Split-brain koruması: `cluster.initial_master_nodes` yalnızca ilk bootstrap'ta kullanılır; sonraki master seçimlerinde OpenSearch voting configuration quorum'u (2/2 node'da n/2+1 = 2 gerekli → aslında 2-node cluster tek node kaybında master seçemez)
- **Faz 1 kabul**: Tek node kaybında cluster yellow'da kalır, yeni index yaratma master seçimi gerektirdiği için başarısız olabilir. Read/write mevcut indekslerde sürer. Ops uyarısı: 2-node cluster'da bir master node kaybı uzun sürerse 3. node eklemek gerekir (Faz 2)

### Keycloak — tek node kaybı

- Sessions `owners=2` olduğu için tüm aktif session'lar diğer node'da mevcut
- Kullanıcı tekrar login olmaz (eğer LB sticky session kullanmıyorsa)
- Faz 1 pin modelinde API node-01'e bağlı olduğu için node-01 düşerse el ile LB/DNS'i node-02'ye çevirmek gerek (manuel failover ~30sn)
- Faz 2 nginx round-robin ile otomatikleşecek

---

## 6. Rollback

### OpenSearch

```bash
# vm5:
docker compose -f ~/personel-opensearch-node2/docker-compose.cluster-node2.yaml down

# vm3:
cd /home/kartal/personel/infra/compose
docker compose stop opensearch-01
docker compose rm   opensearch-01
docker compose up -d opensearch   # eski single-node, eski volume opensearch-data
```

Veri dizinleri bindings olduğu için eski `/var/lib/personel/opensearch/data/` bozulmaz. Rsync yalnızca node-01 için yeni yola kopyaladı, eski yol dokunulmadı.

### Keycloak

```bash
# vm5:
docker compose -f ~/personel-keycloak-node2/docker-compose.ha-node2.yaml down

# vm3:
docker compose -f /home/kartal/personel/infra/compose/docker-compose.yaml stop keycloak-01
docker compose -f /home/kartal/personel/infra/compose/docker-compose.yaml rm   keycloak-01

# Eski standalone'u geri başlat:
# (CLAUDE.md §0 eski KC container'ın nasıl başlatıldığını belgeliyor —
# o `docker run` komutunu tekrar çalıştır.)
```

Veritabanı geri dönmez: HA node'ları `keycloak` database'ini kullanırken eski standalone `personel_keycloak` kullanıyordu (farklı DB isimleri). Dolayısıyla eski container çalışır durumda kalır ve şeması değişmez.

---

## 7. KVKK notları

### OpenSearch

- OpenSearch'te Personel audit log full-text araması + Enricher tarafından indekslenen olay metadata'sı saklanıyor. Bu veri **kimlik tanımlayıcı** içerebilir (user ID, endpoint serial, tenant ID). Replikasyon KVKK m.4 (veri işleme prensibi) bakımından ek bir işleme değildir (aynı amaçla aynı veri, sadece fail-safe) fakat **veri işleme ekindeki alt-işleyen listesine** iki fiziksel host eklemesi gerek → `docs/compliance/verbis-kayit-rehberi.md` güncellenmeli
- Retention değişmez — lifecycle policy index bazında geçerli, replikasyon retention'ı etkilemez
- Shard encryption at rest: Mevcut durumda disk bazlı değil (Faz 2 için LUKS + key escrow planlanıyor); iki node arası transport mTLS ile şifreli

### Keycloak

- Keycloak session tablosu kullanıcı kimlikleri + IP + user-agent içerir (KVKK m.6 özel nitelikli değildir ama kişisel veridir)
- Infinispan dağıtılmış cache Postgres üzerinden geçtiği için veri **diskte sadece vm3 Postgres'inde** tutulur; vm5 Keycloak node'u in-memory cache tutar, kalıcı state yoktur
- JGroups transport TLS ile şifrelidir; node-02 çıkış logları PII içermez
- DPO bilgilendirme metni: "Kimlik doğrulama servisi yüksek erişilebilirlik için iki fiziksel sunucuda (vm3 + vm5) çalıştırılmaktadır. Kullanıcı kimlik verisi yalnızca merkezi veritabanında tutulur; ikinci sunucu sadece geçici oturum önbelleği barındırır."

---

## 8. Ek operasyonel notlar

- **Heap tuning**: `OPENSEARCH_NODE1_JAVA_OPTS=-Xms1g -Xmx1g` ve `OPENSEARCH_NODE2_JAVA_OPTS=-Xms1g -Xmx1g` .env'de override edilebilir. vm5 gözlemi altında 24 saat çalıştıktan sonra heap kullanım %70'i aşıyorsa 1.5g'ye çıkar
- **JGroups port 7800**: vm3 ve vm5 arasında karşılıklı açık olmalı. Firewall'da izin verilmezse Keycloak node'ları JGroups view oluşturmaz; `jgroups_ping` tablosunda girişler birikir ama TCP handshake olmaz
- **DB bağlantısı**: vm5 Keycloak node'u vm3 Postgres'ine bağlanır. Postgres `pg_hba.conf`'ında vm5 IP'si için `host keycloak app_keycloak 192.168.5.32/32 scram-sha-256` satırı gerekli. Değilse node-02 başlarken `password authentication failed` log'ar
- **Clock skew**: JGroups view merge için iki host arasında NTP senkron şart. Max 5 saniye tolerans
