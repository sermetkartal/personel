# Personel — Mimari Genel Bakış

> Dil: Türkçe (yönetici özeti). Hedef okuyucu: ürün sahibi, CTO, hukuk/KVKK danışmanı, satış öncesi mühendislik.

## 1. Ürün Tanımı

**Personel**, Türkiye pazarına yönelik, kurumsal çalışan aktivite izleme ve performans analitiği platformudur. Teramind, ActivTrak, Veriato, Insightful ve Safetica gibi küresel UAM (User Activity Monitoring) çözümlerinin yerli, KVKK uyumlu ve on-prem öncelikli alternatifidir.

Platform üç ana bileşenden oluşur:

1. **Uç Nokta Ajanı (Endpoint Agent)** — Windows PC'lere kurulan, Rust ile yazılmış düşük ayak izli bir servistir. Kullanıcı modunda çalışır; süreç/uygulama kullanımı, aktif pencere, ekran görüntüsü, dosya olayları, klavye/pano, yazıcı, USB, ağ akışları ve boşta/aktif zamanı toplar.
2. **Sunucu Tarafı (On-Prem Sunucu Kümesi)** — Müşterinin kendi veri merkezinde Docker Compose + systemd ile çalışan olay alımı, depolama, analitik, politika dağıtımı, canlı izleme ve uzaktan yönetim bileşenleri.
3. **Yönetici Konsolu + Şeffaflık Portalı + Mobil Admin** — Next.js tabanlı web uygulamaları ve sınırlı özellikli bir mobil yönetici uygulaması.

## 2. Hedef Metrikler (Faz 1 MVP)

| Metrik | Hedef |
|---|---|
| Pilot uç nokta sayısı | 500 |
| Ölçeklenme hedefi | 10.000 uç nokta, günlük ~1 milyar olay |
| Ajan CPU tüketimi (ortalama) | < %2 |
| Ajan RAM tüketimi | < 150 MB |
| Yönetici panelinde p95 sorgu süresi | < 1 saniye |
| Sunucu tarafı uptime | %99,5 |
| Olay uçtan uca gecikme (p95) | < 5 saniye |

## 3. KVKK Uyumu ve Hukuki Çerçeve

Personel; 6698 sayılı **Kişisel Verilerin Korunması Kanunu** çerçevesinde tasarlanmıştır. Faz 0/1 kapsamı **yalnızca Türkiye**'dir; AB/GDPR genişlemesi Faz 3+'a bırakılmış, ancak veri modeli bu genişlemeyi bloke etmeyecek şekilde tasarlanmıştır.

Temel uyum ilkeleri:

- **Hukuki sebep (m.5-6)**: İşveren meşru menfaati ve iş sözleşmesinin ifası; açık rıza gerektiren özel nitelikli veriler (klavye içeriği gibi) için ayrıca çalışan bilgilendirmesi ve şeffaflık portalı.
- **Amaçla sınırlılık (m.4)**: Her veri kategorisi için toplama amacı politika motorunda tanımlıdır; amaç dışı kullanım teknik olarak engellenir.
- **Saklama süresi (m.7)**: Her veri sınıfı için açık saklama matrisine bağlanmıştır (bkz. `data-retention-matrix.md`).
- **Güvenlik (m.12)**: Uçtan uca mTLS, sertifika sabitleme, disk şifreleme, anahtar hiyerarşisi, denetim günlüğü.
- **VERBİS kaydı**: Her kurulum için müşteri VERBİS kayıt şablonu sağlanır.
- **Veri sorumlusu / veri işleyen ayrımı**: On-prem kurulumda müşteri veri sorumlusudur; Personel sağlayıcı teknik hizmet sağlayıcıdır.

## 4. Kritik Tasarım Kararı: Klavye İçeriği Şifrelemesi (ve Varsayılan Kapalı DLP)

En hassas tasarım karar, klavye (keystroke) içeriğinin **yöneticilerin asla ham metni göremeyeceği** şekilde şifrelenmesidir. Bu bir politika kuralı değil, **kriptografik olarak zorlanan bir mimari garantidir**. **ADR 0013 (2026-04-11) ile birlikte** bu garanti iki katmanlı bir hukuki iddia haline gelmiştir (bkz. `docs/compliance/kvkk-framework.md` §10.2):

- **Varsayılan durum (DLP kapalı)**: Her yeni Faz 1 kurulumunda DLP servisi **varsayılan olarak kapalıdır**. Vault'ta `dlp-service` AppRole ve politika oluşturulur (denetlenebilir olması için) ama **Secret ID düzenlenmez**. Sonuç: hiçbir süreç TMK `derive` edemez; Vault audit device, kurulumun ömrü boyunca sıfır `derive` çağrısı gösterir; Postgres'teki `keystroke_keys` tablosu boştur; ajan klavye içeriği toplamaz (`KeystrokeSettings.content_enabled = false` varsayılan politika bundle'ında). Hukuki iddia: "Bu kurulumda klavye içeriği şifre çözme anahtarı hiçbir zaman var olmamıştır." Bu, varlığıyla ispatlanan matematiksel bir olgudur.
- **Opt-in sonrası durum (DLP açık)**: Müşteri DPO, belgelenmiş ve denetlenebilir bir opt-in seremonisini (DPIA güncellemesi → imzalı form → `infra/scripts/dlp-enable.sh` → şeffaflık banner'ı → audit chain + checkpoint) tamamladıktan sonra DLP aktif hale gelir. Aktif durumda, uç noktada toplanan klavye içeriği yalnızca **DLP servisinin** türetebileceği anahtarla AES-256-GCM altında şifrelenir; yönetici konsolu anahtar hiyerarşisinde hiçbir materyale sahip değildir; DLP servisi izole süreçte çalışır (Profil 1 veya Profil 2) ve dışa yalnızca redakte edilmiş `dlp.match` meta verisi üretir — ham metin asla DLP sınırını terk etmez. Opt-out aynı seviyede belgelenir (`infra/scripts/dlp-disable.sh`).
- **Pazarlama ifadesi**: "Varsayılan olarak keystroke-blind olan tek KVKK-uyumlu UAM." Bu ifadeyi hiçbir rakip eşleyememektedir.
- Detaylar: `docs/architecture/key-hierarchy.md` §Default vs Opted-In Runtime Guarantees, `docs/architecture/dlp-deployment-profiles.md` §Default Operational State, `docs/adr/0013-dlp-disabled-by-default.md`, `docs/adr/0009-keystroke-content-encryption.md`, `docs/compliance/kvkk-framework.md` §10.5.

## 5. Canlı İzleme (Live View) Yönetişimi

Canlı ekran izleme, uzun süredir tartışmalı olan UAM özelliğidir. Personel üç güvencelik uygular:

1. **Tek seferlik kurulum bildirimi**: Çalışan, ajan kurulumu sırasında canlı izlemenin mümkün olduğu konusunda **bir defa** bilgilendirilir (şeffaflık portalında kalıcı olarak görünür).
2. **İkinci onay (HR Approval Gate)**: Her canlı oturum, yönetici isteği + İK rolündeki ikinci bir kişinin onayı olmadan **kriptografik olarak başlatılamaz**. Bu bir durum makinesiyle zorlanır.
3. **Değiştirilemez denetim izi**: Her oturum, hash-zincirli append-only log'a yazılır. Log geriye dönük değiştirilemez; VERBİS denetimlerinde kanıt olarak sunulabilir.

Detaylar: `docs/architecture/live-view-protocol.md`.

## 6. Dağıtım Modeli

- **Faz 0/1**: Sadece **on-prem**. Docker Compose (uygulama katmanı) + systemd (host servisleri, yedekleme, log rotasyonu). Kubernetes ve Helm **yok**.
- **Tek kiracılı (single-tenant)** kurulum MVP için kabul edilir; kod yolları çok kiracılıyı (multi-tenant) destekler ama pilotta kullanılmaz.
- **Faz 3+**: Yönetilen SaaS. Bu faza kadar, tenant izolasyonu ve veri modeli SaaS'a geçişi bloke etmeyecek şekilde tasarlanmıştır.

## 7. Yüksek Seviye Mimari Bileşenler

| Bileşen | Teknoloji | Sorumluluk |
|---|---|---|
| Endpoint Agent | Rust (tokio) | Veri toplama, yerel şifreli kuyruk, politika yürütme |
| Ingest Gateway | Go, gRPC | mTLS sonlandırma, olay batch doğrulama, NATS'a yazma |
| Event Bus | NATS JetStream | Olay stream, yeniden oynatma, tüketici grupları |
| Time-Series Store | ClickHouse | Olay, metrik, rapor tablosu |
| Metadata Store | PostgreSQL | Tenant, kullanıcı, uç nokta, politika, denetim |
| Object Store | MinIO (S3 uyumlu) | Ekran görüntüleri, video klipler |
| DLP Service | Go (izole süreç) | Klavye içeriği şifre çözme ve desen eşleme — **ADR 0013 uyarınca varsayılan olarak kapalıdır; opt-in seremonisi ile aktive edilir** |
| Secrets / KMS | HashiCorp Vault | Anahtar hiyerarşisi, sertifika yönetimi |
| Live View | LiveKit (WebRTC SFU) | Ekran paylaşım oturumları |
| Admin Console | Next.js 14 (App Router) | Yönetici arayüzü |
| Transparency Portal | Next.js 14 | Çalışan şeffaflık arayüzü |
| Search/Logs | OpenSearch | Tam metin arama, denetim log arama |

## 8. Faz 1 Kapsamı (Özet)

**İÇERİDE**: Windows ajanı, temel olay toplama (≥25 olay türü), ekran görüntüsü, canlı izleme, politika dağıtımı, USB/app/web engelleme, **DLP pattern matching (ADR 0013 — varsayılan kapalı, opt-in ile aktif)**, denetim logu, yönetici konsolu, şeffaflık portalı.

**DIŞARIDA**: macOS/Linux ajan, minifilter driver, OCR, davranışsal anomali ML, mobil admin, SSO/SAML (temel LDAP var), multi-tenant aktif kullanım, bulut SaaS.

Detaylar: `docs/architecture/mvp-scope.md`.

## 9. Risk Özeti

| Risk | Etki | Azaltma |
|---|---|---|
| Kullanıcı modu anti-tamper limitleri | Yüksek | Faz 3 minifilter driver yol haritasında; Faz 1'de watchdog + registry korumaları |
| Klavye şifrelemesinin DLP servisine bağımlılığı | Orta (Düşük, ADR 0013 sonrası) | ADR 0013: DLP varsayılan kapalı; aktifken Profil 1/2 izolasyonu ve yüksek erişilebilirlik replikası |
| Canlı izlemenin hukuki zemini | Yüksek | Çift onaylı kapı + şeffaflık portalı + VERBİS şablonu |
| ClickHouse operasyonel yük | Orta | MVP'de tek node; ölçeklenme için ADR 0004'te yol haritası |
| Ajan otomatik güncelleme başarısızlığı | Yüksek | Canary + otomatik rollback + imzalı artefakt zinciri |
