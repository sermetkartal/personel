# Personel — IT Operator Eğitimi (TR)

Bu doküman, Personel Platform'u **çalıştıran ve bakımını yapan** IT Operator
rolündeki personel için 4 saatlik operasyonel eğitim programını tanımlar.

**Hedef kitle**: Linux SysAdmin, DevOps, SRE, IT Operator
**Ön koşul**: Linux komut satırı + Docker temel bilgisi
**Format**: 1 saat teori + 3 saat hands-on

---

## Program

### 0:00-0:15 — Giriş + Pre-test

- Kurulum / upgrade / yedekleme / olay müdahalesi konularında 10 soruluk pre-test
- Eksik alanları tespit etmek için

---

### 0:15-1:15 — Bölüm 1: Mimari + Operasyon Felsefesi (60 dk)

- Personel stack'inin 18 containerı (`infra/compose/docker-compose.yaml`)
- Her servisin çalıştığı port, volume, bağımlılık
- Systemd vs Docker Compose iki katmanlı orkestrasyon
- Vault unseal lifecycle (şirket politikasına göre manuel veya auto)
- Log konumları (`/var/log/personel/`, journalctl, docker logs)
- Config yönetimi (`/etc/personel/` + environment vars)
- Monitoring stack (Prometheus + Grafana + AlertManager)

---

### 1:15-2:15 — Bölüm 2: Kurulum ve Upgrade (60 dk)

#### 1:15-1:45 — Canlı install lab

**Hedef**: Sıfırdan bir VM'ye Personel kurulumu yapmak.

1. Pre-flight check (`infra/scripts/preflight.sh`)
2. `.env` dosyasını doldurma (CHANGEME'leri değiştirme)
3. `infra/install.sh` çalıştırma
4. Vault unseal ceremony (Shamir 3-of-5)
5. Smoke test

**Alıştırma**: Her katılımcı kendi VM'sinde sıfırdan install.sh çalıştırır.

#### 1:45-2:15 — Upgrade procedure

- Yeni release bildirimi nasıl gelir (release notes)
- Pre-upgrade checklist:
  - [ ] Full backup al
  - [ ] Change window planla
  - [ ] Rollback planı hazırla
  - [ ] DPO onayı al (audit/data etkisi)
- Upgrade akışı:
  - `git pull` (veya tar indirme)
  - `docker compose pull`
  - `docker compose up -d --no-deps <service>`
  - Migration otomatik
  - Smoke test
- Rollback akışı (sorun çıkarsa)

---

**Break 15 dk**

---

### 2:30-3:30 — Bölüm 3: Backup ve Restore (60 dk)

#### 2:30-3:00 — Backup automation

- Nightly cron (`infra/scripts/backup.sh`)
- Output: `/var/lib/personel/backups/YYYY-MM-DD-HH/`
  - `vault-snapshot.snap`
  - `postgres-*.dump`
  - `clickhouse-personel.tar.gz`
  - `minio-mirror/`
  - `config.tar.gz`
  - `MANIFEST.sha256`
- 7 günlük retention policy
- Her başarılı backup → API `/v1/system/backup-runs` evidence kaydı
- Prometheus alert: `BackupFailure` 24h içinde başarılı backup yoksa
- Off-site backup mirror (Faz 5 Wave 1 scaffold)

#### 3:00-3:30 — Canlı restore drill

**Hedef**: Postgres'i bir snapshot'tan geri yükleyerek dakika başına RTO ölçümü.

1. Mevcut Postgres container'ı durdur
2. Son backup'tan `pg_restore`
3. Container'ı yeniden başlat
4. Migration version kontrolü
5. Smoke test

**Alıştırma**: Her katılımcı restore drill'i çalıştırır, RTO'sunu kaydeder.

---

### 3:30-4:30 — Bölüm 4: Incident Response + Troubleshooting (60 dk)

#### 3:30-4:00 — Top 10 incident senaryosu

1. **Vault sealed** → operator unseal ceremony başlatır
2. **API crash loop** → log analiz + config fix
3. **Postgres disk full** → retention reduction + vacuum
4. **ClickHouse OOM** → query kill + memory limit artırma
5. **NATS stream full** → retention policy veya consumer catchup
6. **Keycloak realm corruption** → backup'tan restore
7. **Endpoint agent toplu offline** → gateway TLS cert süresi dolmuş mu
8. **Dashboard yavaş** → ClickHouse index check
9. **Audit log verify fail** → hash chain tamper alarm → security incident
10. **Backup fail 3 gün üst üste** → storage + cron check

Her senaryo için runbook: `docs/operations/troubleshooting.md`

#### 4:00-4:30 — Live incident drill

Eğitmen rastgele bir container'ı durdurur veya disk dolduran fake data üretir.
Katılımcılar:

1. Alert tetiklenir mi?
2. Symptom nasıl anlaşılır?
3. Root cause bulma
4. Fix + verification
5. Post-mortem kısa raporu

---

## Öğrenme Çıktıları

IT Operator eğitim sonunda:

1. ✅ Sıfırdan kurulumu yapabilir
2. ✅ Upgrade'i güvenle gerçekleştirebilir ve rollback planını uygulayabilir
3. ✅ Backup automation'ını doğrulayabilir + restore drill'i çalıştırabilir
4. ✅ En sık 10 incident senaryosuna müdahale edebilir
5. ✅ Prometheus alert'lerini yorumlayabilir
6. ✅ Docker logs + journalctl ile troubleshoot yapabilir
7. ✅ Vault unseal ceremony'yi gerçekleştirebilir (kendi keyholder ise)
8. ✅ Destek'e kritik log + tanı bilgisiyle birlikte ticket açabilir

---

## Eğitim Materyalleri

- `infra/runbooks/install.md`
- `docs/operations/troubleshooting.md`
- `docs/operations/backup-migration.md`
- `infra/scripts/preflight.sh`
- `docs/security/runbooks/vault-setup.md`
- `docs/security/runbooks/incident-response-playbook.md`

---

## Sertifikasyon

Eğitim + başarılı restore drill sonunda **"Personel Certified Operator"**
sertifikası verilir.

---

*Güncelleme: her major release'den sonra revize edilir.*
