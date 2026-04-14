# Personel — Satış Sık Sorulan Sorular (30 Soru TR)

Bu doküman, ön satış görüşmelerinde karşılaşılan en yaygın 30 soruyu ve kısa +
teknik dayanaklı cevaplarını içerir. Her cevap, Personel reposundaki ilgili
dokümana referans verir.

---

## A. KVKK ve Hukuki Uyum

### 1. KVKK uyumluluğunu nasıl sağlıyorsunuz?

KVKK uyumu Personel'in mimari tasarımına **ilk günden içerilmiştir**, sonradan
yamalı değildir. Somut kanıtlar: VERBİS envanter export, aydınlatma metni
template'i + zorunlu ilk-giriş onayı, DSR (m.11) 30 günlük SLA, saklama
matrisi, hash-zincirli audit, Şeffaflık Portalı. Detaylar:
`docs/compliance/kvkk-framework.md`.

### 2. Klavye içeriğini (keystroke) toplayabilir miyim? Bu KVKK'ya aykırı mı?

ADR 0013 ile klavye içeriği **varsayılan KAPALI**'dır. Etkinleştirmek için:
(a) DPIA amendment, (b) DPO + IT Security + Hukuk imzalı opt-in formu, (c)
Vault Secret ID issuance töreni, (d) Şeffaflık Portalı banner'ı. Etkinleştirildikten
sonra bile admin rolü içeriği **kriptografik olarak** okuyamaz — sadece izole
DLP motoru, önceden imzalanmış kurallarla eşleşme arayabilir. Bu KVKK m.5/m.6
orantılılık prensibine tam uyumlu bir tasarımdır.
Referans: `docs/adr/0013-dlp-disabled-by-default.md`

### 3. VERBİS kaydını siz mi yapıyorsunuz?

VERBİS kaydını **veri sorumlusu olan müşteri kurum** yapar. Biz (Personel)
veri işleyen sıfatıyla DPA (Veri İşleyen Sözleşmesi) imzalarız. Ancak size
VERBİS için hazır envanter export'u sağlarız: tek tıkla JSON + PDF.
Referans: `docs/compliance/verbis-kayit-rehberi.md`

### 4. Veri yurt dışına çıkıyor mu?

**Hayır**. Personel on-prem bir üründür. Tüm veri müşterinin kendi
sunucusunda (tek VM veya HA cluster) tutulur. Faz 1'de cloud seçeneği yoktur.
Faz 3'te opsiyonel AB/TR bölgesinde yönetilen SaaS planlanmaktadır.

### 5. KVKK Kurulu denetimine gelirse ne yapacağım?

`infra/runbooks/inspection-ready.md` runbook'u, denetim başladığında 30
dakika içinde sunulması gereken kanıt dosyalarını sıralar: audit log export,
saklama matrisi enforcement raporu, DSR işlem kayıtları, politika imza zinciri,
çalışan bilgilendirme kanıtları. DPO'nuz bu runbook'u takip ederek hazır olur.

### 6. DPIA yapmamız gerekiyor mu?

Evet. KVKK Kurulu 2019/144 sayılı kararla UAM sistemleri için DPIA zorunlu
tutmuştur. Personel size doldurulmuş bir DPIA şablonu verir:
`docs/compliance/dpia-sablonu.md`. Ortalama 4-8 saatlik DPO çalışmasıyla
müşteri-özgü hale getirilir.

### 7. Çalışanın rızası lazım mı?

Hayır — meşru menfaat ve iş sözleşmesi kapsamında işlem temelidir. Ancak
**aydınlatma zorunludur** (KVKK m.10). Personel, ilk giriş modalı ile bu
yükümlülüğü otomatikleştirir (`apps/portal/src/components/onboarding/first-login-modal.tsx`).
**Sadece DLP keystroke içerik analizi** açık rıza gerektirir (ADR 0013).

---

## B. Teknik Mimari

### 8. Hangi işletim sistemlerinde çalışıyor?

**Endpoint**: Windows 10/11 (x64) — Faz 1 production-ready. macOS ve Linux
scaffolds var (Faz 2 — ilk release sonrası çalışabilir).

**Sunucu**: Ubuntu 22.04 LTS + Docker 25+. Diğer Linux dağıtımları test edilmedi.

### 9. Kubernetes'e ihtiyacımız var mı?

**Hayır**. Docker Compose + systemd ile 500 endpoint'e kadar tek VM yeterli.
Kubernetes Faz 3'te multi-tenant SaaS için planlıyoruz.

### 10. Ekran görüntüsü kim saklıyor? Şifreli mi?

Ekran görüntüleri MinIO bucket'ında saklanır, AES-GCM envelope ile şifrelenir.
Anahtar hiyerarşisi: PE-DEK (per-endpoint) → TMK (tenant master key) → Vault
root. Yönetici ekran görüntüsünü sadece Investigator/DPO rolüyle +
presigned URL ile görebilir, doğrudan dosya sistemi erişimi yoktur.
Referans: `docs/architecture/key-hierarchy.md`

### 11. Endpoint agent ne kadar kaynak kullanıyor?

Hedef: **<%2 CPU, <150 MB RAM**. Rust + tek binary. Gerçek ölçüm:
`qa/footprint-bench` Windows CI runner'ında çalışır.

### 12. Agent kapatılabilir mi? Kullanıcı process'i öldürürse?

Anti-tamper ile korunur: (a) Windows service "restart on failure", (b) sibling
watchdog process mutual monitoring, (c) config DPAPI-sealed (manuel değiştirme
tespit edilir), (d) PE self-hash + registry ACL, (e) tampering audit event
üretir ve yöneticiye bildirim gider.
Referans: `docs/security/anti-tamper.md`

### 13. Offline çalışır mı? İnternet kopuşu olursa veri kaybolur mu?

Evet, offline çalışır. Agent yerel SQLCipher queue'da olayları şifreli biriktirir
(AES-256 page encryption). Bağlantı geri geldiğinde NATS'e drain eder. 24 saate
kadar offline olay birikebilir (policy ile ayarlanabilir).

### 14. NATS/ClickHouse/Vault nedir? Bu kadar çok şey lazım mı?

Her biri spesifik bir amaç için:
- **NATS JetStream**: Event bus, at-rest encryption, Kafka'dan daha basit ops
- **ClickHouse**: Time-series olaylar, %95+ sıkıştırma, 1B+/gün throughput
- **Vault**: PKI + gizli anahtar yönetimi, FIPS-aligned
- **PostgreSQL**: Metadata (kullanıcılar, politika, audit), RLS ile tenant izolasyonu
- **MinIO**: Ekran görüntüsü + log + blob, Object Lock WORM audit için
- **OpenSearch**: Full-text audit arama
- **Keycloak**: OIDC/SAML auth, SCIM provisioning

Hepsi `install.sh` ile otomatik ayağa kalkar, DevOps uzmanlığı gerekmez.

### 15. Yedekleme nasıl yapılıyor?

Nightly otomatik: Postgres pg_dump → MinIO, ClickHouse snapshot → MinIO,
MinIO cross-bucket mirror. Her başarılı yedek **SOC 2 A1.2 evidence item**
olarak kaydedilir. PITR (Point-in-Time Recovery) Postgres WAL archiving ile.
Restore drill runbook'u: `docs/operations/backup-migration.md`.

---

## C. Güvenlik

### 16. Audit log'u biri siliyor mu? Tamper-proof mu?

Postgres audit_log tablosunda INSERT + SELECT only (UPDATE/DELETE revoke).
Her entry hash-zincirli (önceki entry hash'i yeni entry'de imzalanır). Günlük
checkpoint Vault Ed25519 key ile imzalanır ve MinIO Object Lock Compliance
mode WORM bucket'a yazılır. Superuser DBA bile `DISABLE TRIGGER` yapsa bile
WORM checkpoint ile tespit edilir. Referans: `docs/security/audit-log.md`.

### 17. DBA root yetkisiyle kayıtları değiştirebilir mi?

Teorik olarak superuser `ALTER TABLE ... DISABLE TRIGGER` yapabilir. Ancak
WORM checkpoint mimarisi bir sonraki günlük checkpoint'te bunu tespit eder.
Entry-level anlık WORM mirror roadmap'te var (Faz 3).

### 18. Pentest yaptırdınız mı?

Faz 1 exit kriterleri arasında 3. parti pentest var (`CLAUDE.md` §10). Müşteri
talep ederse pilot sonrası koordineli pentest yaptırıyoruz. Kendi pentest
sonucunuzu da paylaşırsanız bulguları değerlendiririz.

### 19. Keystroke verilerini siz mi saklıyorsunuz? Okuyabiliyor musunuz?

**Hayır**. Keystroke ciphertext olarak saklanır. Personel şirketi olarak biz bu
verilere erişim sağlayamayız. Anahtar hiyerarşisi müşterinin Vault instance'ında
yaşar. Faz 1 exit #9 red team testi bu garantiyi sürekli doğrular.

### 20. Zero-day'de ne olur?

(a) Anti-tamper + güvenli config ile sınırlı etki, (b) SBOM (cargo cyclonedx
+ go cyclonedx) ile bağımlılık envanteri hızlı tarama, (c) güvenlik patch'leri
OTA update ile dağıtılır (dual-signed), (d) customer notification 24 saat
içinde. Faz 12 security roadmap detaylar (CLAUDE.md §0 Faz 12).

---

## D. Operasyon ve Kurulum

### 21. Kurulum ne kadar sürüyor?

**2 saat** hedef (Faz 1 exit kriteri). `sudo infra/install.sh` idempotent
çalışır, Vault unseal ceremony (Shamir 3-of-5), migration'lar, healthcheck,
smoke test hepsi içinde.

### 22. Upgrade zero-downtime mi?

Blue-green deployment scaffold mevcut (Faz 13 #136). Mevcut release için
rolling restart — yaklaşık 2-5 dakika kısa kesinti. Faz 13 sonrası tam
zero-downtime.

### 23. Monitoring nasıl?

Prometheus + Grafana + AlertManager dahil. Kritik alarmlar:
- Agent heartbeat kaybı (Flow 7 employee-disable classification)
- DSR SLA aşımı (KVKK m.11 30 gün)
- Vault audit hatası
- Backup failure
- SOC 2 evidence coverage gap

### 24. Hangi rolleri destekliyorsunuz?

7 çekirdek rol: **Admin**, **DPO**, **HR Manager**, **Department Manager**,
**IT Manager**, **IT Operator**, **Investigator**. Her rol için özel UI +
RBAC matrisi. Referans: `apps/api/internal/auth/rbac.go`.

### 25. Keycloak üzerinden AD/LDAP bağlayabilir miyiz?

Evet. Keycloak 24 AD/LDAP federasyonu, SAML 2.0, OIDC, SCIM 2.0 destekler.
Kurulum sonrası realm config'te 15 dakikalık iş.

---

## E. Ticari

### 26. Fiyatlandırma modeli nedir?

Endpoint başına yıllık. 3 tier: Starter (50-100), Business (100-300),
Enterprise (300+). Eklenti modüller: UBA, OCR, HRIS, Mobile app, SIEM export.
Gerçek rakamlar müşteriye özel teklifte. POC 30 gün ücretsiz.

### 27. Sözleşme süresi ne?

1 yıl minimum, 3 yıl tercih edilir (indirim). 30 günlük POC sonrası karar.

### 28. Destek SLA'sı nasıl?

3 tier: **Standard** (8×5, 24h first response, 3 gün çözüm), **Priority**
(8×5, 4h first response, 1 gün çözüm), **Enterprise** (24×7, 1h first
response, 8h P1 çözüm). Detay: `docs/customer-success/support-sla.md`.

### 29. Tüm kaynak kod bizim mi?

Hayır, Personel ticari (closed-source) bir üründür. Ancak müşteri escrow
hizmeti sunulabilir (şirket kapanırsa kaynak kod devrolur, kurumsal sözleşmeye
özel madde).

### 30. Ne zaman production-ready olacak?

Faz 1 build clean (tüm kod derlenir). Faz 1 exit kriterleri 18 madde olup
bazıları doğrulandı, bazıları hala aktif. Pilot müşterilerle 2026 Q2-Q3
süresince canlı pilot, 2026 Q4 genel kullanıma açılış (GA) hedefi.
Mevcut durum her zaman güncel: `CLAUDE.md` §0.

---

*Bu FAQ canlı bir dokümandır — her yeni müşteri görüşmesinden sonra eklenen
soruları içerir. Güncelleme sorumluluğu: Satış ekibi + Product Manager.*
