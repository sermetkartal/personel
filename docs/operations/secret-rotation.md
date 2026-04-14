# Secret Rotation Runbook

> **Faz 5 — Madde 55**
> **Hedef**: Personel platform'undaki statik kimlik bilgilerinin (Postgres rol şifreleri, Vault AppRole secret-id'leri, MinIO root, Keycloak admin) periyodik ve denetlenebilir biçimde döndürülmesi.
> **Sahibi**: SRE — DPO sign-off cadence değişikliklerinde
> **Zamanlama**: `personel-secret-rotation.timer` (haftalık, Pazar 04:00) — script her seferinde envanterdeki **vadesi gelen** secret'ları döndürür.

---

## 1. Kapsam ve Cadence Gerekçesi

`infra/scripts/secret-inventory.yaml` döndürülecek secret'ları, türlerini ve döngülerini tanımlar.

### Cadence kararı (DPO onaylı)

| Tür | Cadence | Gerekçe |
|---|---|---|
| Vault AppRole secret-id (api-service, gateway-service, enricher-service) | **30 gün** | Bu kimlik bilgileri sızarsa saldırgan tüm tenant verilerine erişebilir. SOC 2 CC6.1 alt-üç-aylık döngü bekler. KVKK yüksek-değerli ayrıcalık sınırlandırması. |
| Keycloak admin password | **30 gün** | Admin user üzerinden tüm IdP politikası, realm konfigürasyonu, kullanıcı CRUD mümkün. Tek faktörlü olduğu için yüksek risk. |
| Postgres rol şifreleri (app_admin_api, personel_enricher, personel_gw) | **90 gün** | Etki alanı tek schema, blast radius dar. Compose restart cascade'i pahalı (60-90 sn kesinti). |
| MinIO root access key | **90 gün** | Tüm bucket'lara root erişim ama backup audit-worm bucket Object Lock Compliance modunda — silinemez. Restart cascade'i pahalı. |

**Bu cadence'ı değiştirmek için**:
1. DPO + CTO sign-off (e-posta veya ticket)
2. `audit_log` üzerinden `secret_rotation.cadence_changed` aksiyonu
3. `secret-inventory.yaml` üzerinde commit + PR + code review
4. Bu runbook'taki tablonun güncellenmesi

Cadence'ı **genişletmek** (ör. 30→60 gün) compliance düşüşüdür ve DPO onayı zorunludur. **Daraltmak** (30→14 gün) izin gerektirmez ama operasyonel maliyeti hesaba kat.

## 2. Çalışma Modeli

### Nasıl tetiklenir

- **Otomatik**: `personel-secret-rotation.timer` haftalık koşar. Script vadesi gelmemiş secret'ları atlar — yani 90 günlük postgres şifresi 13 Pazar boyunca skip edilir, 13. Pazarın round'unda rotate edilir.
- **Manuel**: SRE her zaman `sudo /opt/personel/infra/scripts/rotate-secrets.sh` ile tetikleyebilir.
- **Acil**: Tek bir secret için: `sudo /opt/personel/infra/scripts/rotate-secrets.sh --secret postgres-app-admin-api --force`

### Akış (her secret için)

1. **Vade kontrolü**: `/var/lib/personel/secret-rotation/<name>.last_rotated` marker'ı + cadence
2. **Yeni değer üretimi**: `openssl`/`urandom` tabanlı, alfanümerik, talep edilen uzunlukta. (AppRole secret-id için Vault'un kendisi üretir.)
3. **Kaynağı güncelle**: postgres `ALTER ROLE`, vault `auth/approle/role/<r>/secret-id`, MinIO env update, Keycloak `kcadm.sh set-password`
4. **Yeni dosyayı stage et**: `/etc/personel/secrets/<name>.new` (chmod 600)
5. **Reload**: `docker compose restart <service>` — ilgili tüketici container'ı
6. **Verify**: yeni secret kullanılarak known-good operation (psql SELECT 1, healthz, mc admin info, kc token request)
7. **Atomic swap**: `<name>.new` → `<name>`, eskiyi `shred`
8. **Audit emit**: API'ye `secret.rotated` aksiyonu — SOC 2 CC6.1 evidence trail
9. **Marker güncelleme**

### Hata davranışı

- Verify başarısız olursa **eski secret yerinde kalır**, `.new` dosyası shred edilir, log'a `CRITICAL` yazılır, audit'e `outcome=verify_failed` ile kayıt düşer
- Reload başarısız olursa aynı şekilde
- Bir secret'ın hatası diğer secret'ları bloklamaz — script tüm envanteri yürütür, sonda toplam başarısız sayısı ile exit code = 2 dönderir

## 3. Operasyon Komutları

### Vade durumunu görüntüle (rotate etmeden)

```bash
sudo /opt/personel/infra/scripts/rotate-secrets.sh --check-only
```

### Tek bir secret rotate et

```bash
sudo /opt/personel/infra/scripts/rotate-secrets.sh --secret vault-approle-api-service
```

### Tümünü zorla rotate et (acil compromise senaryosu)

```bash
sudo /opt/personel/infra/scripts/rotate-secrets.sh --force
```

### Log'lar

- Detaylı log: `/var/log/personel/secret-rotation.log` (append-only)
- Systemd journal: `journalctl -u personel-secret-rotation.service -n 200`
- SOC 2 audit chain: API üzerinden `GET /v1/audit?action=secret.rotated`

## 4. Yeni bir secret eklemek

1. `infra/scripts/secret-inventory.yaml` içine yeni entry ekle
2. Reload + verify komutlarını mutlaka test et (öncelikle `--check-only` ve sonra `--secret <name>` ile manuel)
3. Yeni secret tipi ise (`postgres-role`, `vault-approle-secret-id`, `minio-root`, `keycloak-admin` dışında), `rotate-secrets.sh` içinde `update_<type>` fonksiyonunu yaz
4. Bu runbook'taki cadence tablosunu güncelle

## 5. SOC 2 Audit Trail Beklentisi

Her başarılı rotasyon `audit_log` içinde şunları içermeli:

```json
{
  "action": "secret.rotated",
  "resource": "vault-approle-api-service",
  "resource_type": "vault-approle-secret-id",
  "outcome": "success",
  "actor": "system:secret-rotation",
  "occurred_at": "2026-04-13T04:15:22Z"
}
```

Bu kayıt `audit.WORMSink` tarafından MinIO `audit-worm` bucket'ına da mirror edilir (5-yıl Object Lock Compliance retention).

`AUDIT_TOKEN` env var **set edilmemişse** audit emit atlanır ve uyarı yazılır. Bu durumda operatör manuel olarak `infra/runbooks/soc2-manual-evidence-submission.md` prosedürünü uygulamalıdır.

## 6. Geri Dönüş (Rollback)

Rotation script atomic swap'tan **önce** verify yaptığı için "yarım rotation" durumu olamaz: ya başarılı ve dosya değişti, ya başarısız ve eski dosya yerinde. Yine de patolojik bir senaryoda (verify yanlış pozitif, sonradan tüketici servis crash):

1. `journalctl -u personel-secret-rotation.service` ile son rotasyon zamanını bul
2. Eğer postgres rolü ise: önceki şifreyi yedeklemiş olmalısın — script `<name>.old` shred eder, dolayısıyla DBA olarak elle yeni şifre belirle:
   ```sql
   ALTER ROLE app_admin_api WITH PASSWORD '<elle-belirlenen>';
   ```
   ve `/etc/personel/secrets/postgres-app-admin-api` içine yaz, sonra `docker compose restart api`
3. Vault AppRole için: aynı role yeni bir secret-id üret ve elle replace et:
   ```bash
   vault write -f auth/approle/role/api-service/secret-id
   ```
4. **Compose .env'in `.bak.<timestamp>` yedekleri** MinIO + Keycloak rotasyonlarında oluşturulur — buradan eski değeri geri al

## 7. Bilinen Eksiklikler / Kalan Tech Debt

- AppRole secret-id rotation **eski secret-id'yi revoke etmiyor** — yeni issue ediliyor, eski TTL ile kendiliğinden expire ediyor. Bu Vault best practice ile uyumlu (zero-downtime) ama "compromise sonrası anında revoke" gerekirse manuel: `vault write auth/approle/role/<r>/secret-id-accessor/destroy accessor=<acc>`
- MinIO root rotation compose `.env` üzerinden çalışıyor — bu yüzden script root yazma yetkisi gerektirir ve `.env` git'e commit edilmemeli (zaten edilmiyor). Alternatif: docker secrets'a geçiş (Madde 56 kapsamında düşünülecek)
- Keycloak admin password rotation `kcadm.sh` ile yapılıyor; eski password compose `.env` içinde tutuluyor olmalı yoksa script eski password'ü bilemez. Bu chicken-and-egg pilot ortam dışında üretimde "first-rotation bootstrap" prosedürü gerektirir.
- Pre-rotation snapshot otomasyonu yok (postgres dump). Üretimde Madde 58 (backup automation) bu eksikliği kapatır — rotation öncesi nightly backup garanti olmalı.

## 8. DPO İçin Özet (3 cümle)

Personel platformu statik kimlik bilgilerini düzenli olarak (yüksek-risk = 30 gün, orta-risk = 90 gün) otomatik döndürür; her döndürme SOC 2 CC6.1 audit zincirine yazılır ve doğrulama başarısız olursa eski kimlik bilgisi yerinde kalır. Cadence'ı genişletmek (ör. 30 → 60 gün) DPO onayı gerektirir; daraltmak gerektirmez. Tüm rotasyon olayları `audit_log` tablosunda `action=secret.rotated` ile izlenebilir ve aylık SOC 2 evidence pack içinde yer alır.

---

## Son kontrol — 2026-04-14 (Wave 9 Sprint 5)

- Runbook içeriği Wave 1 deploy kuyruğunda AWAITING operator action
  olarak korunuyor. vm3'te systemd timer (`personel-secret-rotate.timer`)
  henüz etkinleştirilmedi; dev ortamda default parolalar sabit.
- Önkoşul: `vault-prod-migration.md` tamamlanmış + Vault transit key
  `kv/personel/rotation/*` yolları oluşturulmuş olmalı.
- Rotasyon öncesi `backup-restore.md` nightly backup son 24 saat içinde
  başarılı olmalı (script precondition).
- Prosedür 2026-04-13 sürümünden değişmedi. Yeni statik secret eklendiyse
  (Wave 8'de eklenmedi) `rotate.sh` hedef listesi güncellenmeli.
