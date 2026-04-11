# Personel Platform — Infrastructure / Altyapı

> **TR:** Bu dizin, Personel platformunun on-prem tek sunucu dağıtımı için gerekli tüm altyapı kodunu içerir: Docker Compose, systemd birimleri, kurulum betikleri, yedekleme/geri yükleme ve gözlemlenebilirlik temeli.
>
> **EN:** This directory contains all infrastructure code for Personel's on-prem single-server deployment: Docker Compose, systemd units, install scripts, backup/restore, and observability baseline.

---

## Hızlı Başlangıç / Quick Start

```bash
# EN: Copy and configure environment
cp compose/.env.example compose/.env
chmod 600 compose/.env
nano compose/.env   # Fill all CHANGEME values

# EN: Run preflight check
sudo scripts/preflight-check.sh

# EN: Install (interactive)
sudo ./install.sh
```

**TR:** Detaylı adımlar için: `runbooks/install.md`
**EN:** For detailed steps: `runbooks/install.md`

---

## Dizin Yapısı / Directory Structure

```
infra/
├── install.sh                  TR: Kurulum betiği / EN: Install script
├── uninstall.sh                TR: Kaldırma betiği / EN: Uninstall script
├── upgrade.sh                  TR: Yükseltme betiği / EN: Upgrade script
├── backup.sh                   TR: Yedekleme betiği / EN: Backup script
├── restore.sh                  TR: Geri yükleme betiği / EN: Restore script
├── compose/
│   ├── docker-compose.yaml     TR: Ana Compose dosyası / EN: Main Compose file
│   ├── .env.example            TR: Ortam değişkenleri şablonu
│   ├── postgres/               TR: PostgreSQL yapılandırması ve şeması
│   ├── clickhouse/             TR: ClickHouse yapılandırması
│   ├── nats/                   TR: NATS JetStream yapılandırması
│   ├── minio/                  TR: MinIO bucket ve lifecycle tanımları
│   ├── opensearch/             TR: OpenSearch ve index şablonları
│   ├── keycloak/               TR: Keycloak realm ve istemci tanımları
│   ├── vault/                  TR: Vault yapılandırması ve politikaları
│   ├── dlp/                    TR: DLP servis hardening (distroless + seccomp)
│   ├── prometheus/             TR: Metrikler ve uyarılar
│   └── grafana/                TR: Dashboard'lar
├── systemd/                    TR: systemd birimi ve zamanlayıcılar
├── scripts/                    TR: Operasyon betikleri
├── runbooks/                   TR: Operasyon kılavuzları
└── tests/                      TR: Kurulum sonrası testler
```

---

## Kaynak Ayak İzi / Resource Footprint

16 CPU / 64 GB RAM sunucu için tasarlanmıştır. %20 başlık alanı korunur.

| Servis | CPU Limiti | RAM Limiti |
|---|---|---|
| ClickHouse | 4 | 16 GB |
| PostgreSQL | 4 | 8 GB |
| NATS | 2 | 4 GB |
| MinIO | 2 | 4 GB |
| OpenSearch | 2 | 4 GB |
| Gateway + API + DLP | 6 | 6 GB |
| Keycloak + UI | 4 | 3.5 GB |
| Vault + Monitoring | 3 | 4 GB |
| **Toplam / Total** | **~27** | **~50 GB** |

---

## Güvenlik Kontrolleri / Security Controls

- **mTLS:** Tüm ajan-sunucu bağlantıları mTLS ile korunur
- **DLP İzolasyonu:** Distroless konteyner, read-only FS, cap-drop all, seccomp, AppArmor
- **Vault:** Shamir 3-of-5, TLS 1.3 only, audit device etkin
- **Ağ:** Sadece Envoy dış ağa açılır; tüm servisler iç ağda
- **Sırlar:** Gerçek sırlar hiçbir zaman dosyalarda saklanmaz
- **Denetim:** Hash-zincirli, append-only, değiştirme korumalı

---

## Runbook'lar / Runbooks

| Konu / Topic | Dosya / File |
|---|---|
| Kurulum / Installation | `runbooks/install.md` |
| Yedekleme / Backup | `runbooks/backup-restore.md` |
| Felaket Kurtarma / DR | `runbooks/disaster-recovery.md` |
| Yükseltme / Upgrade | `runbooks/upgrade.md` |
| Olay Müdahalesi / Incident | `runbooks/incident-response.md` |
| Faz 1 Çıkış Kriterleri | `runbooks/phase-1-exit-criteria.md` |
| Sorun Giderme / Troubleshooting | `runbooks/troubleshooting.md` |

---

## KVKK Uyum Notu / KVKK Compliance Note

Bu platform KVKK m.12 güvenlik yükümlülükleri gözetilerek tasarlanmıştır. Her kurulum, yükseltme ve yedekleme işlemi denetim günlüğüne kaydedilir. Denetim zinciri hash-zincirli ve değiştirme korumalıdır.

This platform is designed with KVKK Article 12 security obligations in mind. Every install, upgrade, and backup action is logged in the audit trail. The audit chain is hash-chained and tamper-evident.
