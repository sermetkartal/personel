# Personel Platform — Maliyet İzleme (Faz 13 #145)

> **Hedef**: On-prem Personel kurulumunun operasyon maliyetini izlemek,
> per-tenant allocation yapmak, cloud migration alternatiflerini karşılaştırmak.

---

## 1. On-prem maliyet modeli

Personel on-prem first olduğundan, "cloud bill" yerine dört kategori
maliyet var:

| Kategori | Örnek kalem | Ölçüm |
|---|---|---|
| **Donanım amortisman** | Sunucular, switchler, UPS | 5 yıl doğrusal |
| **Elektrik + soğutma** | Rack kWh × tarife | Aylık kWh ölçümü |
| **Operasyon headcount** | SRE + DPO + destek süresi | İş saati × maaş |
| **Lisans + 3rd party** | Vault Enterprise (optional), EV code signing, MaxMind, SIEM | Yıllık |

### 1.1 Baseline maliyet (500 endpoint pilot, Türkiye)

| Kalem | Aylık TL | Kaynak |
|---|---|---|
| 2× Ubuntu server (16C/64G/2TB) amortisman | 4,200 | 5 yıl × 250K TL |
| Elektrik (2 sunucu + 1 switch, 300W ort) | 1,600 | 0.20 TL/kWh × 720h × 2.4 sunucu-eq |
| Rack space (kolokasyon 1U) | 2,000 | ortalama TR DC |
| SRE headcount (%20 FTE) | 25,000 | 125K brüt × 0.2 |
| DPO danışmanlık (%10 FTE) | 20,000 | 200K brüt × 0.1 |
| EV Code Signing | 1,800 | 22K TL/yıl / 12 |
| MaxMind GeoLite2 | 0 | ücretsiz tier |
| **Toplam** | **~54,600 TL/ay** | |

500 endpoint × 54,600 / 500 = **109 TL/endpoint/ay**

---

## 2. Per-tenant allocation formülü

Multi-tenant Personel kurulumunda her tenant'a pay çıkarmak:

```
tenant_cost = base_allocation + usage_allocation

base_allocation = total_cost × (0.3 / tenant_count)
usage_allocation = total_cost × 0.7 × (tenant_weight / total_weight)

tenant_weight = (active_users × 1.0)
              + (storage_gb × 0.5)
              + (events_per_day / 1_000_000 × 2.0)
              + (screenshots_per_day / 100_000 × 3.0)
```

### 2.1 Ağırlık katsayıları

| Faktör | Katsayı | Gerekçe |
|---|---|---|
| active_user | 1.0 | temel birim |
| storage_gb | 0.5 | depolama görece ucuz |
| events_per_million | 2.0 | CPU + ClickHouse yükü |
| screenshots_per_100k | 3.0 | MinIO + bandwidth + OCR |

### 2.2 Örnek hesap

```
Total cost: 54,600 TL
Tenants: 2
  Tenant A: 200 users, 150 GB, 5M events/day, 200k screenshots/day
  Tenant B: 100 users, 50 GB, 1M events/day, 50k screenshots/day

Base: 54,600 × 0.3 = 16,380; 8,190 per tenant

Weight A: 200 + 75 + 10 + 6 = 291
Weight B: 100 + 25 + 2 + 1.5 = 128.5
Total:    419.5

Usage A: 54,600 × 0.7 × 291/419.5 = 26,505
Usage B: 54,600 × 0.7 × 128.5/419.5 = 11,705

Tenant A total: 8,190 + 26,505 = 34,695 TL
Tenant B total: 8,190 + 11,705 = 19,895 TL
```

Implementation: `infra/scripts/cost-export.sh` (aşağıda).

---

## 3. Prometheus + Grafana ile kaynak takibi

### 3.1 Gerekli metrikler

| Metrik | Kaynak | Kullanım |
|---|---|---|
| `container_cpu_usage_seconds_total` | cAdvisor | Per-service CPU |
| `container_memory_working_set_bytes` | cAdvisor | Per-service RAM |
| `node_disk_io_time_seconds_total` | node_exporter | Disk IO |
| `personel_events_per_tenant_total` | enricher | Per-tenant event hacim |
| `personel_storage_bytes_per_tenant` | api | Per-tenant storage |
| `personel_screenshots_per_tenant_total` | enricher | Per-tenant screenshot |

### 3.2 Cost dashboard

Grafana dashboard JSON'ı `infra/compose/grafana/dashboards/cost-monitoring.json`
altına yerleştirilmeli (scaffold henüz yok).

Görselleştirilecek paneller:

1. Stacked area: per-service CPU/RAM over 30d
2. Gauge: per-tenant cost (aylık)
3. Time series: per-tenant events/storage trend
4. Table: top 10 tenant by weight

---

## 4. Aylık CSV export scripti

`infra/scripts/cost-export.sh`:

```bash
#!/usr/bin/env bash
# Generates tenant cost allocation CSV for a given month.
# Usage: ./cost-export.sh 2026-04 > cost-2026-04.csv
MONTH="${1:-$(date -u +%Y-%m)}"
TOTAL_COST_TL="${PERSONEL_MONTHLY_COST_TL:-54600}"

# Prometheus queries (approximate; real queries TBD)
PROM="http://localhost:9090/api/v1/query"

query() {
  curl -s "${PROM}" --data-urlencode "query=$1" | jq -r '.data.result[0].value[1] // "0"'
}

echo "tenant_id,tenant_name,active_users,storage_gb,events_per_day,screenshots_per_day,weight,base_tl,usage_tl,total_tl"

# Iterate tenants from postgres
psql "${DATABASE_URL}" -tAc "SELECT id, name FROM core.tenants WHERE status='active'" | \
while IFS=\| read -r tid tname; do
  users=$(query "personel_active_users_per_tenant{tenant_id=\"${tid}\"}")
  storage=$(query "personel_storage_bytes_per_tenant{tenant_id=\"${tid}\"} / 1e9")
  events=$(query "rate(personel_events_per_tenant_total{tenant_id=\"${tid}\"}[30d]) * 86400")
  shots=$(query "rate(personel_screenshots_per_tenant_total{tenant_id=\"${tid}\"}[30d]) * 86400")

  weight=$(awk -v u="${users}" -v s="${storage}" -v e="${events}" -v sh="${shots}" \
    'BEGIN { print u + s*0.5 + (e/1e6)*2 + (sh/1e5)*3 }')

  # Base + usage split applied after loop (needs total weight first).
  # Simplified here: emit raw, post-process with awk.
  echo "${tid},${tname},${users},${storage},${events},${shots},${weight},,,,"
done | awk -F, -v total="${TOTAL_COST_TL}" '
BEGIN { base_pool = total * 0.3; usage_pool = total * 0.7; cnt = 0 }
NR==1 { print; next }
{ lines[NR] = $0; weights[NR] = $7; total_w += $7; cnt++ }
END {
  base_per = base_pool / cnt
  for (i in lines) {
    n = split(lines[i], f, ",")
    usage = usage_pool * (weights[i] / total_w)
    total_line = base_per + usage
    printf "%s,%s,%s,%s,%s,%s,%.2f,%.2f,%.2f,%.2f\n",
      f[1], f[2], f[3], f[4], f[5], f[6], weights[i], base_per, usage, total_line
  }
}'
```

Cron ayı başında:

```cron
0 2 1 * * /opt/personel/infra/scripts/cost-export.sh > /var/log/personel/cost-$(date +\%Y-\%m).csv
```

---

## 5. Cloud migration karşılaştırması

Eğer müşteri bulutta çalıştırmak isterse (şu an Personel roadmap Faz 3+):

| Provider | Tier | Aylık maliyet (500 endpoint) | Notlar |
|---|---|---|---|
| **AWS** | m6i.2xlarge × 3 + RDS PG + S3 | ~$2,800 | EKS eklenince +$500 |
| **GCP** | n2-standard-8 × 3 + Cloud SQL + GCS | ~$2,600 | Network egress pahalı |
| **Azure** | D8s_v5 × 3 + Azure DB + Blob | ~$2,900 | TR bölgesi yok, data residency risk |
| **DigitalOcean** | Droplet 16GB × 3 + Spaces | ~$720 | Compliance riski (KVKK loc) |
| **TR bölgesi cloud** (Turkcell / TT) | 3× 16C/64G + managed PG | ~70,000 TL | KVKK uyumlu, daha pahalı |

### 5.1 Maliyet-fayda özeti

| Kriter | On-prem | AWS | TR cloud |
|---|---|---|---|
| Aylık TL (30 TL/$) | 54,600 | ~84,000 | ~70,000 |
| KVKK data residency | ✓ | risk (müşteri DPO) | ✓ |
| SLA | self | 99.99% | 99.9% |
| Backup/DR | manuel | managed | managed |
| Ops headcount | %20 FTE | %5 FTE | %10 FTE |
| **5 yıl TCO** | 3.3M TL | 5.0M TL | 4.2M TL |

**Karar**: Küçük müşteri (<100 endpoint) için on-prem maliyet avantajı ~%35.
500+ endpoint'te cloud operasyonel basitliği rekabet eder. 1000+ endpoint'te
cloud genelde daha ucuz ama compliance riski gelişir.

---

## 6. Alertler

```yaml
- alert: MonthlyCostOverBudget
  expr: personel_monthly_cost_tl > 60000
  for: 1h
  labels: {severity: warning}
  annotations:
    description: "Aylık maliyet {{ $value }} TL — bütçeyi aştı"

- alert: TenantStorageSpike
  expr: rate(personel_storage_bytes_per_tenant[24h]) > 10737418240
  for: 2h
  labels: {severity: info}
  annotations:
    description: "Tenant {{ $labels.tenant_id }} 24 saatte 10 GB'tan fazla büyüdü"
```

---

## 7. Aksiyonlar

- [ ] `infra/scripts/cost-export.sh` scriptini gerçek Prometheus query'leri
      ile doldur
- [ ] Grafana cost dashboard JSON'u `infra/compose/grafana/dashboards/` altına
      ekle
- [ ] Per-tenant metric'leri `personel_events_per_tenant_total` API +
      enricher tarafında emit et
- [ ] Müşteri DPA template'ine cost transparency maddesi ekle
- [ ] Aylık report generator cron'a bağla
