# Personel — Incident Response Playbook

> Dil: Türkçe. Hedef okuyucu: Personel operatör ekibi, DPO, güvenlik ekibi, nöbetçi SRE.
>
> Bu playbook, operasyonel incident'lar için **ilk yanıt rehberidir**. Uzun vadeli post-mortem ve kalıcı düzeltme için ayrı süreçler vardır.
>
> İlgili:
> - `docs/operations/ops-runbook.md` — günlük operasyon (bu değil — o günlük işler için)
> - `docs/operations/troubleshooting.md` — symptom/fix matrisi
> - `docs/compliance/kvkk-framework.md` — KVKK m.12 ihlal bildirimi (72h)
> - `docs/security/runbooks/*` — spesifik konu runbook'ları

## İçindekiler

1. [Severity Sınıflandırması](#1-severity-sınıflandırması)
2. [Escalation Matrisi](#2-escalation-matrisi)
3. [Incident Types](#3-incident-types)
   - [3.1 Agent Mass Disconnect](#31-agent-mass-disconnect)
   - [3.2 Suspected Data Exfiltration](#32-suspected-data-exfiltration)
   - [3.3 Admin Account Compromise](#33-admin-account-compromise)
   - [3.4 Unauthorized Keystroke Decryption Attempt](#34-unauthorized-keystroke-decryption-attempt)
   - [3.5 Vault Seal / Auto-Unseal Failure](#35-vault-seal--auto-unseal-failure)
   - [3.6 Backup Integrity Failure](#36-backup-integrity-failure)
   - [3.7 Audit Chain Break](#37-audit-chain-break)
   - [3.8 Keycloak Service Outage](#38-keycloak-service-outage)
   - [3.9 MinIO Storage Pool Degraded](#39-minio-storage-pool-degraded)
   - [3.10 Gateway mTLS Mass Failure](#310-gateway-mtls-mass-failure)
4. [KVKK m.12 Bildirim Prosedürü](#4-kvkk-m12-bildirim-prosedürü)
5. [Post-Mortem Template](#5-post-mortem-template)

---

## 1. Severity Sınıflandırması

| Severity | Etiket | Tanım | İlk Yanıt | Eskalasyon |
|---|---|---|---|---|
| **P1** | Kritik | Servis tamamen kapalı VEYA KVKK veri ihlali şüphesi | 5 dakika | Anında DPO + CTO |
| **P2** | Yüksek | Kısmi kesinti (bir servis) VEYA ciddi güvenlik olayı | 15 dakika | 30 dk içinde SRE lead |
| **P3** | Orta | Performans düşüklüğü VEYA alert stream'inde pattern | 1 saat | Shift bitiminde |
| **P4** | Düşük | Tek hata, toleranslı failover | Aynı gün | Haftalık review |

### P1 Tetikleyici örnekler

- Audit chain break (KVKK m.12 kanıt bütünlüğü şüphesi)
- Admin hesabı compromise
- Klavye içerik şifresinin yetkisiz çözümü girişimi
- Vault sealed > 5 dk ve API/Gateway down
- > 20% uç nokta offline (olası network attack)
- Veri exfiltration iddiası (ClickHouse / MinIO bulk read anomalisi)
- KVKK m.12 kapsamı şüphesi — 72 saat bildirim gereği

---

## 2. Escalation Matrisi

| Severity | 0-15 dk | 15-60 dk | 1-4 saat | 4 saat+ |
|---|---|---|---|---|
| P1 | Nöbetçi SRE → Senior SRE → DPO → CTO | Ekip toplantısı, war room | Müşteri bildirimi taslak | Kurul (72h kural) |
| P2 | Nöbetçi SRE → SRE lead | DPO (veriyle ilgiliyse) | Takım incident review | — |
| P3 | Nöbetçi SRE | Shift raporu | — | — |
| P4 | Ticket | — | — | — |

### İletişim Kanalları

- **War room**: Slack `#personel-incident-YYYY-MM-DD-NNN`
- **Yetkili bildirim**: DPO e-posta + telefon
- **Müşteri bildirimi**: Kurum iletişim listesi (DPIA'da tanımlı)
- **Kurul (KVKK)**: [ihlal@kvkk.gov.tr](mailto:ihlal@kvkk.gov.tr) + elektronik form

---

## 3. Incident Types

### 3.1 Agent Mass Disconnect

**Severity**: P1 (>20% endpoint), P2 (5-20%), P3 (<5%)

**Detection signals**:
- Prometheus `AgentMassDisconnect` alert (5 dk içinde)
- Grafana "Endpoint Health" dashboard → online count düşüş
- Konsol dashboard → aktif cihaz rakamı düştü

**Containment**:
1. `docker compose ps gateway` → gateway healthy mi?
2. `docker compose logs gateway --tail 500 | grep -i error`
3. Network level: `ss -tnp | grep :9443 | wc -l` — açık bağlantı sayısı
4. Sertifika rotasyonu sırasında mıyız? `journalctl -u personel-cert-rotate.service --since "1 hour ago"`
5. Ağ bölgesi sorunu mu? Farklı bir alt ağdaki uç noktalar farklı davranıyor mu?

**Eradication**:
- Gateway crash-loop ise: config validate + restart
- Cert expired ise: manuel rotation + force re-enroll instructions
- Ağ outage ise: network ekibi eskalasyonu

**Recovery**:
- Ajanlar otomatik yeniden bağlanır (backoff). Manuel restart gerekli değil.
- 30 dk içinde %95+ tekrar online olmalı.
- SQLite offline queue nedeniyle **veri kaybı yok** — sadece gecikme.

**Post-incident review**:
- Kök sebep
- Auto-recovery süresi ölçümü (hedef: < 30 dk)
- Alert threshold tuning gerekiyor mu?

### 3.2 Suspected Data Exfiltration

**Severity**: P1

**Detection signals**:
- UBA detector anomaly score > 0.8
- MinIO audit log: bulk GET from non-DPO user
- ClickHouse query_log: anomalous SELECT volume
- Audit log arama: aynı kullanıcı çok sayıda endpoint detail GET

**Containment**:
1. **Şüpheli kullanıcı hesabını derhal kilitle** (Keycloak):
   ```bash
   docker exec personel-keycloak /opt/keycloak/bin/kcadm.sh update users/<id> \
     -r personel -s enabled=false
   ```
2. Aktif oturumlarını iptal et:
   ```bash
   docker exec personel-keycloak /opt/keycloak/bin/kcadm.sh logout-user \
     -r personel <id>
   ```
3. API key'i revoke et (varsa)
4. `legal_hold` koy (DPO kararı ile, ilgili audit trail'i koru)

**Eradication**:
- Neyi çıkardı? (ClickHouse query log + MinIO audit log ile izleyin)
- Nasıl çıkardı? (API token? VPN? direct DB access?)
- Ne kadar sürede?

**Recovery**:
- KVKK m.12 ihlal bildirimi (§4) — 72h kural
- Etkilenen veri sahiplerine bildirim (KVKK m.10/2)
- Şifre / API key rotasyonu
- Disk forensic (gerekli ise)

**Post-incident**:
- Root cause analizi (insider? dış saldırgan? phishing?)
- RBAC eksiği var mı?
- Monitoring eksikliği var mı?

### 3.3 Admin Account Compromise

**Severity**: P1

**Detection signals**:
- Anormal login location / IP
- Keycloak failed login burst sonrası başarılı
- Admin hareketlerinde olağandışı pattern (gece yarısı DSR ret)

**Containment**:
1. Hesap kilitle (Keycloak disable)
2. Tüm oturumları invalidate et
3. **MFA secret'ı sıfırla**
4. Parolayı sıfırla
5. Audit log'da son 24 saatteki tüm aksiyonları listele
6. Yapılan DEĞİŞİKLİKLER varsa **geri al** (politika push → eski versiyona, endpoint wipe → NO recovery mümkün değil)

**Eradication**:
- Giriş vektörü ne? (phishing? kaybolmuş token? MFA bypass?)
- Aynı saldırgan başka hesaplarda da aktif mi?

**Recovery**:
- KVKK m.12 ihlal: compromise admin verilere erişmişse **evet** bildirim gerekli
- İlgili veri sahiplerine bildirim (etki kapsamı)
- **Tüm admin hesaplarına** zorunlu şifre + MFA rotasyonu

**Post-incident**:
- MFA zorla (eğer opsiyonel idiyse)
- Privileged access management (PAM) değerlendirmesi
- Just-in-time admin rolü (Faz 2)

### 3.4 Unauthorized Keystroke Decryption Attempt

**Severity**: P1 — ADR 0013 ihlal girişimi

**Detection signals**:
- Vault audit log: `transit/decrypt/dlp-edk-*` yetkisiz kullanıcıdan
- DLP service olmayan bir container'dan `pki/sign` call
- Admin API log: `/v1/keystroke` benzeri non-existent endpoint'e istek

**Containment**:
1. Şüpheli kullanıcı / servis credential'ı **derhal iptal et**
2. Vault DLP AppRole secret ID'sini revoke et
3. DLP container'ı **durdur** (ADR 0013 default-off state'e dön)
4. Network isolation: DLP container'ın dış trafik ACL'ini kapat

**Eradication**:
- Vault audit log'da tam call trace (`cat /var/lib/personel/vault/audit/vault_audit.log | jq 'select(.request.path | test("dlp"))' `)
- Hangi keys, ne kadar süre? Başarılı mı?
- Decryption call'ları gerçekten şifre çözdü mü yoksa reject edildi mi?

**Recovery**:
- **KVKK m.12 BİLDİRİMİ GEREKLİ** — özel nitelikli veri risk şüphesi
- Klavye ciphertext blob'larını **şimdi imha et** — yeni DEK rotate, eski blob'lar okunamaz hale gelir
- DLP'yi yeniden etkinleştirmeden önce yeni DPIA amendment + opt-in seremonisi
- Tüm çalışanlara bildirim

**Post-incident**:
- Red team testi (Phase 1 exit #9) yeniden koş
- PE-DEK isolation review
- Cryptographic review

### 3.5 Vault Seal / Auto-Unseal Failure

**Severity**: P1 (if > 5 dk ve API/Gateway down), P2 (if API/Gateway cached token'larla devam)

**Detection signals**:
- `VaultSealed` Prometheus alert
- API log: `vault: permission denied` burst
- Gateway log: cert issue fail

**Containment**:
1. Vault pod'u restart oldu mu? (`docker compose ps vault`)
2. Auto-unseal (HSM/KMS) başarısız mı? (prod only)
3. Manuel Shamir unseal seremonisi:
   ```bash
   # 3 farklı yetkili, 3 farklı parça
   docker exec personel-vault vault operator unseal <share1>
   docker exec personel-vault vault operator unseal <share2>
   docker exec personel-vault vault operator unseal <share3>
   ```

**Eradication**:
- Vault audit log'da son başarılı işlem ne zamandı?
- Log ve telemetry ile crash sebebini tespit et (OOM? disk full? config change?)

**Recovery**:
- Unseal sonrası API + Gateway **restart** (AppRole token'ları yenilenir)
- Cert rotation yap (Vault yeniden geldi — cached cert süresi yaklaşıyor olabilir)

**Post-incident**:
- Auto-unseal migration (HSM / cloud KMS) değerlendirmesi
- Vault HA (Integrated Storage Raft) değerlendirmesi

### 3.6 Backup Integrity Failure

**Severity**: P1

**Detection signals**:
- `BackupFailed` alert
- Manifest signature verify fail
- Backup dump file corrupt (gzip/zstd test fail)

**Containment**:
1. Son başarılı backup ne zamandı?
2. Failure nedeni? (disk full? MinIO down? script bug?)
3. Primary ve off-site backup senkron mu?

**Eradication**:
- Kaynağı tamir et (disk, MinIO, script)
- Manuel full backup çalıştır (`infra/scripts/backup-nightly.sh --on-demand`)
- Manifest integrity test (`infra/scripts/verify-backup.sh`)

**Recovery**:
- RPO değerlendirme: son başarılı backup ile şimdi arasındaki data loss potansiyeli
- ClickHouse / Postgres PITR ile gap doldur (WAL archive varsa)

**Post-incident**:
- Alert sensitivity artırım
- Haftalık restore drill başlat

### 3.7 Audit Chain Break

**Severity**: P1 — KVKK m.12 kanıt bütünlüğü

**Detection signals**:
- `AuditChainBroken` alert
- Daily checkpoint job fail
- `audit.verify_chain()` False döndü

**Containment**:
1. **Dokunma** — kanıtı koru
2. Postgres immediate snapshot:
   ```bash
   docker exec personel-postgres pg_dump -U postgres personel > /var/lib/personel/forensic/audit-snapshot-$(date +%s).sql
   ```
3. WORM mirror'ı doğrula:
   ```bash
   docker exec personel-minio mc ls myminio/audit-worm/
   ```
4. Son sağlam checkpoint'i bul:
   ```sql
   SELECT * FROM audit_checkpoints ORDER BY created_at DESC LIMIT 10;
   ```
5. Drift aralığı:
   - Checkpoint'ler arası saatleri al
   - O aralıkta hangi Postgres user işlem yaptı?
   - DBA superuser olağandışı kullanım var mı?

**Eradication**:
- Drift kaynağı:
  - DBA bypass? → yasaklı erişim politikası
  - Migration bug? → rollback
  - Corruption? → restore + chain rebuild

**Recovery**:
- Chain'i yeniden **hesaplayamazsın** — append-only. Yeni bir checkpoint başlat ve eski chain'i "broken" olarak işaretle.
- Yeni WORM bucket'a yeni chain başlat.
- **KVKK m.12 BİLDİRİMİ GEREKLİ** — kanıt değiştirilmiş şüphesi.

**Post-incident**:
- Entry-level WORM mirror (şu an sadece daily checkpoint — CLAUDE.md tech debt §10)
- DBA privilege azaltma (superuser sadece emergency)

### 3.8 Keycloak Service Outage

**Severity**: P2 (short) → P1 (> 30 dk)

**Detection signals**:
- API 401 burst
- Console login ekranı açılmıyor
- Keycloak health check fail

**Containment**:
1. `docker compose logs keycloak --tail 200`
2. Memory / CPU OOM mu?
3. Infinispan cluster split-brain (HA varsa)?

**Eradication**:
- Restart: `docker compose restart keycloak`
- Persistent state bozulmuşsa: `/var/lib/personel/keycloak/` backup'tan restore
- Realm import fail ise manuel Admin UI üzerinden

**Recovery**:
- Yeni JWT üretimi başladığında tüm kullanıcılar yeniden login olmak zorunda
- API cache'indeki JWKS key'ler fresh olduğu için immediate recovery

**Post-incident**:
- Keycloak HA (Infinispan 2-node) Faz 5 içinde zaten planlı
- Memory limit tuning

### 3.9 MinIO Storage Pool Degraded

**Severity**: P2 (drive fail, data OK) → P1 (erasure set 2+ drive fail)

**Detection signals**:
- `MinIODiskDegraded` alert
- `mc admin info` → offline drive count

**Containment**:
1. Hangi drive fail etti? (`mc admin info myminio`)
2. RAID / file system level error mi?
3. Healing zaten çalışıyor mu?

**Eradication**:
- Drive replace
- `mc admin heal -r myminio` (recursive heal)
- Progress izle: `mc admin heal status myminio`

**Recovery**:
- Healing sonrası durum green olmalı
- Faz 5'te 4-node erasure set (n+2 koruması)

**Post-incident**:
- Disk monitoring iyileştirme
- Spare drive inventory

### 3.10 Gateway mTLS Mass Failure

**Severity**: P1 (>20% endpoint), P2 (5-20%)

**Detection signals**:
- `GatewayMtlsHandshakeFailures` alert
- Agent log burst `tls handshake failure`

**Containment**:
1. Bir ajan mı tüm ajanlar mı?
2. Tüm ajanlar ise: gateway cert'i değişti mi? (cert rotate bug)
3. Trust anchor (tenant_ca.crt) güncel mi?

**Eradication**:
- Gateway cert'i Vault'tan yeniden iste:
  ```bash
  sudo /opt/personel/infra/scripts/cert-rotate-gateway.sh
  ```
- Gateway restart
- Tenant CA chain push (agentlara yeni CA): `/v1/endpoints/{id}/refresh-token` toplu

**Recovery**:
- Ajanlar backoff ile yeniden bağlanır
- SQLite queue nedeniyle veri kaybı yok

**Post-incident**:
- Cert rotation prosedürünün gateway restart gerektirmediği (graceful reload) değerlendirme
- Ajan tarafında trust anchor refresh mekanizması

---

## 4. KVKK m.12 Bildirim Prosedürü

**72 saat kuralı**: KVKK m.12/5 uyarınca, işlediği kişisel verilerin kanuni olmayan yollarla başkaları tarafından elde edilmesi halinde, veri sorumlusu bu durumu **en kısa sürede ve en geç 72 saat içinde** ilgili kişiye ve Kurul'a bildirir.

### Karar Ağacı

1. **Olay, kişisel veriyi etkiliyor mu?**
   - Hayır → m.12 kapsamı dışı, normal incident kapatma
   - Evet → devam
2. **Olay hukuka aykırı erişim, değiştirme veya imha içeriyor mu?**
   - Hayır → dahili işlem, bildirim **opsiyonel**
   - Evet → devam
3. **Etkilenen veri sayısı / tip / hassasiyet?**
   - Küçük + düşük risk → bildirim zorunlu ama dar kapsam
   - Büyük / özel nitelikli → acil bildirim
4. **Çalışan hakları etkilendi mi?**
   - Evet → m.11 çerçevesinde ayrıca bilgilendirme

### Bildirim İçeriği

Kurul formu (`kvkk.gov.tr/ihlal-bildirimi`):

1. **Veri sorumlusu bilgileri**
2. **İhlalin tarihi ve süresi**
3. **İhlalin nerede ve nasıl fark edildiği**
4. **Etkilenen kişi sayısı** (yaklaşık)
5. **Etkilenen kişisel veri kategorileri**
6. **Olası sonuçlar ve etki**
7. **Alınan tedbirler**
8. **İletişim kişisi (DPO)**

### Çalışan Bildirimi

Her etkilenen çalışana ayrı ayrı veya toplu bildirim:
- E-posta (işçi kayıtlı e-posta adresi)
- Şeffaflık Portalı banner (kalıcı, 30 gün)
- İnternet sitesinde duyuru (toplu ise)

---

## 5. Post-Mortem Template

Her P1/P2 incident için **5 iş günü içinde** post-mortem yazılır:

```markdown
# Incident Post-Mortem: <Başlık>

**Tarih**: YYYY-MM-DD HH:MM
**Severity**: P1/P2/P3
**Süre**: X saat Y dakika
**Yazar**: <isim>
**Lead**: <isim>

## Özet

<2-3 cümle — ne oldu, nasıl çözüldü>

## Timeline

| Zaman (UTC) | Olay |
|---|---|
| HH:MM | İlk alert |
| HH:MM | Nöbetçi SRE sayfalandı |
| HH:MM | War room açıldı |
| HH:MM | Containment başladı |
| HH:MM | Root cause tespit |
| HH:MM | Düzeltme uygulandı |
| HH:MM | Doğrulama |
| HH:MM | Incident kapatıldı |

## Etki

- Kaç müşteri/çalışan etkilendi?
- Hangi servisler down/degraded?
- Veri kaybı? (RPO ölçüm)
- Finansal etki?
- KVKK kapsamı?

## Kök Sebep

<teknik analiz — "5 neden" yöntemi>

## Ne İyi Gitti

- <alert doğru zamanlandı>
- <runbook izlendi>
- <containment hızlıydı>

## Ne Kötü Gitti

- <monitoring eksikliği>
- <eskalasyon gecikti>
- <runbook eksik adım içeriyordu>

## Aksiyon Kalemleri

| # | Sorumlu | Son Tarih | Açıklama |
|---|---|---|---|
| 1 | | | |
| 2 | | | |

## Öğrenimler

<takım için genel dersler>
```

Post-mortem'ler `docs/incidents/YYYY/incident-NNNN.md` altında saklanır. KVKK ilgili ise Kurul bildirim formu linkle bağlanır.

---

## Versiyon

| Sürüm | Tarih | Değişiklik |
|---|---|---|
| 1.0 | 2026-04-13 | Faz 15 #165 — İlk sürüm (10 incident type) |
