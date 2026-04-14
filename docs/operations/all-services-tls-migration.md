# Tüm Servisler için Vault PKI TLS Geçiş Runbook'u

> **Faz 5 — Madde 53**
> **Hedef**: Personel stack'indeki 18 servisin tamamının (Vault, Postgres, ClickHouse, NATS, MinIO, OpenSearch, Keycloak, API, Gateway, Enricher, Console, Portal, DLP, ML-Classifier, OCR-Service, UBA-Detector, Livrec-Service, Grafana) TLS sertifikalarının Vault PKI tarafından üretilmesi.
> **Sahibi**: SRE / DevOps — DPO bilgilendirilir
> **Bekleme süresi**: 60-90 dakika (ilk geçiş), sonraki yenilemeler `rotate-certs.sh` (Madde 54) tarafından otomatik

---

## 1. Önkoşullar

- Vault initialized + unsealed. Root token elinde.
- PKI engine etkin: `vault secrets enable -path=pki pki`
- PKI root CA bootstrap edilmiş (`infra/scripts/ca-bootstrap.sh` koşmuş olmalı)
- `server-cert` rolü oluşturulmuş:
  ```
  vault write pki/roles/server-cert \
    allowed_domains=personel.internal \
    allow_subdomains=true \
    allow_ip_sans=true \
    server_flag=true \
    client_flag=false \
    max_ttl=720h \
    key_type=ec key_bits=256
  ```
- Sunucuda `jq`, `openssl`, `vault` CLI mevcut
- `/etc/personel/tls/` dizini var ve root yazabiliyor
- Tüm servislerin **şu anda** çalışıyor olması (kesintisiz geçiş için)

## 2. Pre-flight: Mevcut sertifikaların yedeği

```bash
sudo cp -a /etc/personel/tls /etc/personel/tls.bak.$(date +%Y%m%d-%H%M)
ls -la /etc/personel/tls.bak.*/
```

Yedeği sakla — geri dönüş senaryosunda 30 saniyede mevcut duruma dönülür.

## 3. Toplu sertifika üretimi

```bash
export VAULT_ADDR=https://127.0.0.1:8200
export VAULT_TOKEN=<root-token-veya-yetkili-token>

# Önce dry-run ile envanteri doğrula
sudo -E /opt/personel/infra/scripts/issue-all-service-certs.sh --dry-run

# Sonra gerçek üretim — idempotent (mevcut > 7 gün geçerli sertifikaları atlar)
sudo -E /opt/personel/infra/scripts/issue-all-service-certs.sh
```

Beklenen çıktı:
```
[issue-certs] === Personel TLS bulk issuance ===
[issue-certs]   ISSUE vault: cn=vault.personel.internal
[issue-certs]   OK vault → /etc/personel/tls/vault.crt
... (18 servis)
[issue-certs] === Summary ===
[issue-certs] issued : 18
[issue-certs] skipped: 0
[issue-certs] failed : 0
```

Hata olursa:
- Tek tek çağırma: `--service api` ile yalnızca o servisin sertifikasını üret
- Vault token yetkilerini kontrol et: `vault token capabilities pki/issue/server-cert`
- PKI rolü doğrula: `vault read pki/roles/server-cert`

## 4. Geçiş sırası (kesintisiz olmasını garantilemek için)

Toplu üretim TLS dizinine sertifikaları yazar; ancak hangi servisin **hangi sırayla restart edileceği** önemlidir. Aşağıdaki sırayı uygula. Her aşamada bir önceki tier sağlıklı olduğunu doğrula.

### Aşama A — Vault (kendi sertifikası)

```bash
docker compose -f infra/compose/docker-compose.yaml restart vault
infra/scripts/vault-unseal.sh
vault status
```

Vault healthy olunca devam et.

### Aşama B — Veri katmanı

```bash
docker compose restart postgres clickhouse nats minio opensearch
docker compose ps
```

Sağlık kontrolleri:
```bash
docker exec personel-postgres pg_isready -U postgres
docker exec personel-clickhouse clickhouse-client -q "SELECT 1"
docker exec personel-nats nats account info -s nats://127.0.0.1:4222
docker exec personel-minio mc admin info local/
curl -k https://127.0.0.1:9200/_cluster/health
```

### Aşama C — Auth katmanı

```bash
docker compose restart keycloak
curl -k https://127.0.0.1:8443/realms/master/.well-known/openid-configuration | jq .issuer
```

### Aşama D — Uygulama katmanı

```bash
docker compose restart api gateway enricher
curl -k https://127.0.0.1:8000/healthz
curl -k https://127.0.0.1:9443/healthz
```

### Aşama E — UI katmanı

```bash
docker compose restart console portal
curl -k https://127.0.0.1:3000/tr
curl -k https://127.0.0.1:3001/tr
```

### Aşama F — Faz 2 servisleri (varsa wired)

```bash
docker compose restart dlp ml-classifier ocr-service uba-detector livrec-service grafana
```

## 5. Konfigürasyon güncellemeleri (DSN değişiklikleri)

Sertifikalar yerinde olunca, **zorunlu** konfigürasyon değişiklikleri:

### API → Postgres

`apps/api/config/config.yaml` (veya .env override):
```
DATABASE_DSN=postgres://app_admin_api:***@postgres:5432/personel?sslmode=verify-full&sslrootcert=/etc/personel/tls/personel-root.crt
```
Eski: `sslmode=disable` — pilot dev kısayolu, **prod'da yasak**.

### Gateway client trust list

Gateway'in agent'lardan gelen mTLS doğrulaması için zaten tenant CA'sını okuyor. **Ek olarak** Vault PKI root sertifikası gateway'in `--trusted-cas` listesine eklenmeli ki backend'lere (NATS, Vault, Postgres) sunucu doğrulaması yapabilsin:
```
docker exec personel-gateway ls -la /etc/personel/tls/personel-root.crt
```

### NATS, ClickHouse, MinIO TLS modları

Her servisin compose env block'unda TLS'i zorla:
```yaml
environment:
  - NATS_TLS_REQUIRED=true
  - CLICKHOUSE_TLS=true
  - MINIO_SERVER_URL=https://minio.personel.internal:9000
```

⚠️ Bu compose değişiklikleri **bu PR'ın kapsamında DEĞİL** — Madde 56 (compose hardening) içinde tek seferde uygulanır. Bu runbook sadece sertifikaları yerleştirir; servisler dev modunda olduğu için sertifikaları hemen kullanmaya başlamayabilirler.

## 6. Doğrulama

```bash
# Her sertifikanın geçerli olduğunu doğrula
for s in vault postgres clickhouse nats minio opensearch keycloak api gateway enricher console portal dlp ml-classifier ocr-service uba-detector livrec-service grafana; do
  if [[ -f /etc/personel/tls/$s.crt ]]; then
    sub=$(openssl x509 -noout -subject -in /etc/personel/tls/$s.crt | sed 's/subject=//')
    iss=$(openssl x509 -noout -issuer  -in /etc/personel/tls/$s.crt | sed 's/issuer=//')
    end=$(openssl x509 -noout -enddate -in /etc/personel/tls/$s.crt | sed 's/notAfter=//')
    printf "%-18s %-50s %s\n" "$s" "$sub" "$end"
  else
    echo "MISSING: $s"
  fi
done
```

Tüm satırların `issuer = O = Personel Platform, CN = Personel Root CA` (veya tenant CA) olduğunu, hiçbir satırın `MISSING` olmadığını doğrula.

## 7. Geri dönüş (rollback)

```bash
sudo systemctl stop personel-compose
sudo rm -rf /etc/personel/tls
sudo cp -a /etc/personel/tls.bak.<timestamp> /etc/personel/tls
sudo systemctl start personel-compose
```

Geri dönüş 60 saniye içinde tamamlanır. Sertifika kaynaklı hata yaşıyorsan **önce rollback yap**, sonra hatayı analiz et — bu hizmet kesintisini en az tutar.

## 8. SOC 2 ve audit kaydı

Bu geçiş bir **change management** olayıdır:
- `change.applied` audit log kaydı (CC8.1)
- Operatör adı + onaylayıcı (DPO veya CTO) + timestamp
- Bu runbook'un version pin'i (git SHA)

DPO sign-off için: `infra/runbooks/soc2-evidence-pack-retrieval.md` içindeki manuel kanıt gönderme prosedürünü kullan ve evidence kind = `KindChangeAuthorization`, control = `CC8.1`.

## 9. Sonraki adımlar

- Madde 54 (sister agent): `rotate-certs.sh` + `cert-inventory.yaml` — otomatik 30 günlük yenileme
- Madde 56: compose `service_started` → `service_healthy` ve TLS'i runtime'da zorla
- Madde 41: Vault auto-unseal sealed file → bu runbook'tan sonra, kalıcı production setup için

---

## Son kontrol — 2026-04-14 (Wave 9 Sprint 5)

- Runbook içeriği Wave 1 deploy kuyruğunda AWAITING operator action
  olarak korunuyor. vm3'te 18 servisten 4'ü (Vault, Gateway, API,
  ClickHouse subset) self-signed cert kullanıyor; diğerleri TLS
  olmadan çalışıyor.
- Bring-up sırasında `infra/scripts/rotate-all-certs.sh` ilk kez full
  cycle koşulmalı. Cert TTL default 90 gün (rol konfigürasyonunda).
- Önkoşul zinciri: **vault-prod-migration tamamlanmış olmalı** + PKI
  engine `pki` altında, `server-cert` ve `agent-cert` rolleri doğrulanmış
  olmalı.
- `cert-inventory.yaml` eksiksiz olmalı; yeni servis ekleyen commit
  (ör. ml-classifier) bu dosyayı da güncellemek zorunda.
