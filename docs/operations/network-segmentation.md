# Personel Platform — Ağ Segmentasyonu (Faz 13 #140)

> **Hedef**: Personel stack'indeki 7 docker network'ünün açık dokümantasyonu,
> servis-network matrisi ve egress/ingress kuralları.
>
> **Referans kod**: `infra/compose/docker-compose.yaml` §25-52 (networks: bloğu).

---

## 1. Network envanteri

| Network | Docker driver | `internal` | Amaç |
|---|---|---|---|
| `public` | bridge | hayır | Sadece nginx reverse proxy (443/80 host expose) |
| `app` | bridge | hayır | API, gateway, enricher, DLP, console, portal — servis-servis |
| `data` | bridge | **evet** | Postgres, ClickHouse, MinIO, OpenSearch — veri tier |
| `vault-net` | bridge | **evet** | Vault ve onu çağıran servisler |
| `monitoring` | bridge | **evet** | Prometheus, Grafana, AlertManager, Loki, Tempo, exporters |
| `dlp-isolated` | bridge | **evet** + icc=false | DLP motoru izole, yan-konteyner ile konuşmaz |
| `net_ml` | bridge | **evet** | ML classifier + Llama server (ADR 0017) |

`internal: true` olan network'ler docker host'un default gateway'ine
erişemez — dış internete çıkamazlar.

---

## 2. Servis-network matrisi

| Servis | public | app | data | vault-net | monitoring | dlp-isolated | net_ml |
|---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| nginx | ✓ | ✓ | | | | | |
| envoy (edge) | ✓ | ✓ | | | | | |
| console | | ✓ | | | | | |
| portal | | ✓ | | | | | |
| api | | ✓ | ✓ | ✓ | ✓ | | |
| gateway | | ✓ | ✓ | ✓ | ✓ | | |
| enricher | | ✓ | ✓ | ✓ | ✓ | | |
| dlp | | | ✓ | ✓ | ✓ | ✓ | |
| postgres | | | ✓ | | ✓ | | |
| clickhouse | | | ✓ | | ✓ | | |
| nats | | ✓ | ✓ | | ✓ | | |
| minio | | | ✓ | | ✓ | | |
| opensearch | | | ✓ | | ✓ | | |
| vault | | | | ✓ | ✓ | | |
| keycloak | | ✓ | ✓ | ✓ | ✓ | | |
| livekit | | ✓ | | | ✓ | | |
| prometheus | | | | | ✓ | | |
| grafana | | ✓ | | ✓ | ✓ | | |
| alertmanager | | | | | ✓ | | |
| loki | | | | | ✓ | | |
| promtail | | | | | ✓ | | |
| tempo | | ✓ | | | ✓ | | |
| ml-classifier | | ✓ | | | ✓ | | ✓ |
| llama-server | | | | | | | ✓ |

**Okuma**: Yatay satır = servis, dikey sütun = network. ✓ işareti servisin o
network'e bağlı olduğunu gösterir. Bir servis birden fazla network'te olabilir.

---

## 3. Ingress kuralları

### 3.1 Dış ingress (public network)

Sadece iki sınıf ingress dış trafiği kabul eder:

1. **nginx reverse proxy** (443/80)
   - Upstream: `console:3000`, `portal:3001`, `api:8000`
   - WAF + rate limit + DDoS koruması (bkz. `infra/compose/nginx/nginx.conf`
     + `includes/ddos-protection.conf`)
2. **gateway mTLS listener** (9443)
   - Upstream: agent traffic only
   - Cert validation: tenant CA chain
   - Firewall level: sadece endpoint subnet'ten kabul (bkz. #141)

Başka hiçbir port host expose edilmez.

### 3.2 İç ingress (app ↔ data)

- API → Postgres / ClickHouse / MinIO / OpenSearch (`data` network)
- Gateway → NATS (`app` network)
- Enricher → NATS + ClickHouse + MinIO (`app` + `data`)
- Tüm app servisleri → Vault (`vault-net`)

---

## 4. Egress kuralları

### 4.1 `data` network (internal)

Data tier'da çalışan containerlar dış internete çıkamaz. İstisnalar:

- **Hiçbiri** — data network'e bağlı hiçbir servis dış bağlantı başlatmamalı.
  Internal: true bu koruma için yeterli.

Eğer DB exporter veya backup agent için egress gerekirse, ayrı bir
`egress-proxy` container'ı `data` + `public` networkü ile `internal: false`
alternatif çözümdür (mevcut değil).

### 4.2 `app` network

Egress'e izin verilen dış destinasyonlar (nginx/envoy üzerinden değil
doğrudan):

| Hedef | Servis | Amaç |
|---|---|---|
| Keycloak OIDC discovery (harici IdP federation) | api, console | opsiyonel SSO |
| Customer SIEM HEC/DCR | api/siem exporter | SIEM event push (ADR 0018) |
| OCSP/CRL responders | agent (gateway üzerinden değil) | cert revocation |
| Maxmind GeoIP download | enricher (cron, opsiyonel) | mmdb refresh |

Dışarıya giden tüm trafik firewall'da **explicit allowlist** (bkz. #141).

### 4.3 `net_ml` network

- **Internal: true** — llama-server ve ml-classifier sadece kendi aralarında
  konuşur.
- Model dosyaları `volumes: [ml-models]` ile sağlanır; runtime'da indirme
  yasak.
- iptables egress-block preflight.sh tarafından doğrulanır (ADR 0017).

---

## 5. Firewall tier diagram

```
                           ┌──────────────┐
  Internet traffic ──────> │   BASTION    │  (22 in only, TOTP + SSH cert)
                           │  + VPN (WG)  │
                           └──────┬───────┘
                                  │ internal mgmt
                                  ▼
  ┌──────────────────────────────────────────────────────────────────┐
  │                      HOST FIREWALL (nft)                          │
  │  accept: 443(nginx), 9443(gateway), 22(bastion), 9090(mon)        │
  │  drop: everything else                                             │
  └──────┬───────────────────────────┬───────────────────────────────┘
         │                           │
         ▼                           ▼
  ┌───────────────┐         ┌─────────────────┐
  │ public net    │         │ monitoring net  │  (internal)
  │ - nginx       │         │ - prometheus    │
  │ - envoy       │         │ - grafana       │
  └───────┬───────┘         │ - loki + tempo  │
          │                 │ - alertmanager  │
          ▼                 └────────┬────────┘
  ┌───────────────┐                  │
  │ app net       │◄─────────────────┘
  │ - api         │
  │ - gateway     │
  │ - enricher    │
  │ - console     │
  │ - portal      │
  └───────┬───────┘
          │
          ▼
  ┌───────────────┐         ┌─────────────────┐
  │ data net      │         │ vault-net       │  (both internal)
  │ - postgres    │         │ - vault         │
  │ - clickhouse  │         └─────────────────┘
  │ - minio       │
  │ - opensearch  │         ┌─────────────────┐
  │ - nats        │         │ dlp-isolated    │  (internal + icc=false)
  └───────────────┘         │ - dlp           │
                            └─────────────────┘
                            ┌─────────────────┐
                            │ net_ml          │  (internal)
                            │ - ml-classifier │
                            │ - llama-server  │
                            └─────────────────┘
```

---

## 6. Operasyonel kontroller

### 6.1 Egress leak testi

```bash
# Data network'ten internet çıkışı YASAK olmalı
docker run --rm --network compose_data --entrypoint sh alpine \
  -c 'wget -T5 -qO- https://1.1.1.1 || echo BLOCKED_OK'
```

Çıktı `BLOCKED_OK` olmalı. Aksi halde `internal: true` yanlış uygulanmış.

### 6.2 DLP izolasyon testi

```bash
# DLP container'ı app network'e ping atamamalı (icc=false + isolated)
docker exec personel-dlp ping -c1 -W1 personel-api 2>&1 | grep -q 'unreachable' \
  && echo OK_ISOLATED \
  || echo FAIL_DLP_CAN_REACH_APP
```

### 6.3 Network stat dump

```bash
docker network ls | grep personel
for n in $(docker network ls --format '{{.Name}}' | grep compose); do
  echo "=== ${n} ==="
  docker network inspect "${n}" --format '{{range .Containers}}{{.Name}} {{end}}'
done
```

---

## 7. Değişiklik yönetimi

Yeni bir servis eklerken:

1. Hangi network'lere bağlanacağına karar ver (minimum-privilege)
2. Bu doküman matrisine satır ekle
3. `docker-compose.yaml` `networks:` listesinde declare et
4. Egress gerekli mi? Firewall allowlist güncelle (#141)
5. PR'de bu dokümandaki matrisi güncellemeyi zorunlu kıl

---

## 8. Bilinen riskler

- **Single docker host**: tüm network'ler aynı Linux kernel üzerinde. Kernel
  kaçış saldırısı network izolasyonunu delebilir. Mitigation: host hardening,
  seccomp, AppArmor, Minimum CAP (ADR 0013).
- **No ingress from monitoring net**: Prometheus → scrape hedefleri
  (`app`/`data`/`vault-net` üzerinde). Monitoring network'ü cross-connect
  olmak zorunda — `compose_monitoring` network `internal: true` ama Prometheus
  bu network'e bağlı containerlar da `monitoring` network'lerinde olduğu için
  çalışır. Düz internet egress hala yok.
- **DNS leaks**: Docker embedded DNS tenant_id label'ını hostname'e sızdırabilir.
  Mitigation: container hostname'lerinde tenant_id kullanma.
