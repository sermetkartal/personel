# Vault Üretim Migrasyonu — Dev Shamir'den 3-of-5 Üretim Anahtar Yapısına

> **Hedef kitle**: Personel platformunun on-prem kurulumunu üretime taşıyacak
> SRE / DevOps mühendisi ve müşteri DPO'su. Bu doküman, Faz 1 bring-up
> sırasında kullanılan dev kısayollarını (1-of-1 Shamir, `disable_mlock=true`,
> tek envelope custodian) gerçek üretim güvencelerine — 3-of-5 Shamir,
> mlock aktif, 5 farklı custodian, otomatik unseal sealed-file ara çözümü ve
> opsiyonel HSM cutover — taşır.
>
> Bu prosedür **PKI durumunu, AppRole'leri ve transit anahtarlarını YOK
> ETMEDEN** gerçekleştirilir. Yanlış sırada yapılırsa tüm sertifika
> envanteri ve şifreleme anahtarları kaybedilir. Adımları **bire bir** takip
> et. Şüpheye düşersen DUR.

---

## İçindekiler

1. Amaç ve kapsam
2. Ön koşullar
3. Sertifika ve veri yedekleme (snapshot)
4. Üretim konfigürasyon dosyasını devreye al
5. Shamir 3-of-5 ceremony — `bootstrap-prod.sh`
6. Post-init re-import: PKI, AppRole, policy, transit
7. Auto-unseal sealed-file kurulumu (operasyonel orta yol)
8. (İleride) HSM auto-unseal cutover
9. Doğrulama matrisi
10. Geri dönüş prosedürü (rollback)

---

## 1. Amaç ve kapsam

Faz 1 sırasında Vault aşağıdaki dev kısayollarıyla ayağa kaldırıldı:

- `disable_mlock = true` — bellek swap'a yazılabilir
- `key-shares=1`, `key-threshold=1` — tek anahtarla unseal
- Root token sunucuda `/tmp/vault-init.json` içinde plaintext
- Sertifikalar self-signed, `tenant_ca.crt` aynı zamanda root CA
- AppRole secret_id'leri commit edilmiş `.env` dosyalarında

Bu migrasyon sonrası:

- `disable_mlock = false` (gerçek mlock; container'da `cap_add: [IPC_LOCK]`)
- 3-of-5 Shamir, 5 farklı custodian'a fiziksel zarflarla dağıtım
- Root token ceremony sonunda revoke edilir, dosya `shred` ile silinir
- AppRole secret_id'leri her servis için Vault'tan bir kez okunur ve
  `/etc/personel/...env` altında 600 modunda saklanır
- Otomatik restart için **age-encrypted shares file** + systemd drop-in ile
  master key (HSM gelene kadar)
- Sertifika rotasyonu için `personel-cert-rotation.timer` günlük çalışır

**Önemli**: Bu migrasyon Vault'un PKI engine'ini, transit anahtarlarını,
AppRole tanımlarını ve policy'leri **yeniden oluşturmaz**. Onlar zaten
Vault'ın storage'ında (raft data dizininde) duruyor. Sadece **seal yapısı**
değişiyor — mevcut state korunur.

---

## 2. Ön koşullar

| Madde | Doğrulama komutu |
|---|---|
| Vault çalışıyor ve erişilebilir | `vault status` |
| Mevcut PKI engine listelenebiliyor | `vault secrets list \| grep pki` |
| AppRole engine aktif | `vault auth list \| grep approle` |
| Transit anahtarları okunabiliyor | `vault list transit/keys` |
| `age` kuruldu (apt: `age`) | `age --version` |
| `jq`, `python3`, `openssl` kuruldu | `jq --version && python3 --version` |
| 3-5 custodian fiziksel olarak hazır (ceremony için) | — |
| Müşteri DPO'su odada | — |
| Backup hedefi (en az 2 fiziksel medium) hazır | — |
| Customer password vault erişimi (master key saklamak için) | — |

**Kritik**: 5 custodian için 5 ayrı tamper-evident zarf hazırlayın. Her
zarfın üzerinde sıra numarası, custodian adı, ve "Açma bu zarfı SADECE
ceremony sırasında" notu olsun.

---

## 3. Sertifika ve veri yedekleme (snapshot)

Migrasyondan önce **mutlaka** Raft snapshot al. Bir şey yanlış giderse bu
snapshot tek dönüş yolundur.

```bash
# 1. Snapshot oluştur
ssh kartal@192.168.5.44 'sudo vault operator raft snapshot save /tmp/vault-pre-prod.snap'

# 2. Snapshot'ı kontrol et
ssh kartal@192.168.5.44 'sudo ls -lh /tmp/vault-pre-prod.snap'

# 3. İki ayrı medyaya kopyala (örnek: müşteri NAS + harici disk)
ssh kartal@192.168.5.44 'sudo cp /tmp/vault-pre-prod.snap /mnt/backup-1/'
ssh kartal@192.168.5.44 'sudo cp /tmp/vault-pre-prod.snap /mnt/backup-2/'

# 4. Snapshot'ın bütünlüğünü test et (boş bir Vault'a restore deneyebilirsin
#    staging ortamında; production'da test etme)
```

**Hash kayıt al**: Snapshot dosyasının SHA256'sını `audit.log` ile birlikte
Customer DPO'ya teslim et.

---

## 4. Üretim konfigürasyon dosyasını devreye al

```bash
# Repo'dan üretim config'i hedef hosta kopyala
scp infra/compose/vault/config.prod.hcl \
    kartal@192.168.5.44:/tmp/vault-config-prod.hcl

# Hedefte sudo ile yerine yerleştir
ssh kartal@192.168.5.44 'sudo install -m 644 -o root -g root \
  /tmp/vault-config-prod.hcl /etc/personel/vault/config.hcl'
```

**Önemli**: `config.prod.hcl` `disable_mlock=false` içeriyor. Vault'un mlock
yapabilmesi için container `cap_add: [IPC_LOCK]` ile çalışmalı.
`docker-compose.yaml` içindeki vault servisini düzenleyin:

```yaml
vault:
  cap_add:
    - IPC_LOCK
```

ve hostta swap'ı kapatmak istiyorsanız (önerilir):

```bash
ssh kartal@192.168.5.44 'sudo swapoff -a'
ssh kartal@192.168.5.44 "sudo sed -i.bak '/swap/s/^/#/' /etc/fstab"
```

Vault container'ı henüz **yeniden başlatma**. 5. adımda yapacağız.

---

## 5. Shamir 3-of-5 ceremony — `bootstrap-prod.sh`

Bu ceremony **kesinlikle yeni bir Vault instance'a** uygulanır. Mevcut Vault
zaten initialize olmuş durumda (`Initialized: true`), bu yüzden
`bootstrap-prod.sh` direkt initialize'ı reddeder. Doğru sıra:

### 5.1 — Yeni boş Vault klasörü oluştur

```bash
ssh kartal@192.168.5.44 'sudo mkdir -p /vault-prod/data && sudo chown -R 100:1000 /vault-prod'
```

### 5.2 — Geçici prod container'ı başlat (yeni storage path)

`docker-compose.prod-migration.yaml` adında geçici bir override yaz veya
manuel:

```bash
ssh kartal@192.168.5.44 'sudo docker run -d --name personel-vault-prod-init \
  --cap-add IPC_LOCK \
  -v /etc/personel/vault/config.hcl:/vault/config/config.hcl:ro \
  -v /etc/personel/tls:/etc/personel/tls:ro \
  -v /vault-prod/data:/vault/data \
  -p 8210:8200 \
  hashicorp/vault:1.15.6 server -config=/vault/config/config.hcl'
```

Bu yeni instance ayrı portta (8210) çalışır, mevcut prod Vault'a
dokunmaz.

### 5.3 — Bootstrap ceremony'sini çalıştır

```bash
# Repo'daki bootstrap-prod.sh'i hedef hosta kopyala
scp infra/compose/vault/bootstrap-prod.sh kartal@192.168.5.44:/tmp/

# Custodian'ları odaya çağır.
# Sonra:
ssh kartal@192.168.5.44 'sudo VAULT_ADDR=https://127.0.0.1:8210 \
  VAULT_CACERT=/etc/personel/tls/tenant_ca.crt \
  bash /tmp/bootstrap-prod.sh'
```

Script:

1. Vault'un initialize olmadığını doğrular
2. "I UNDERSTAND" onayı ister
3. `vault operator init -key-shares=5 -key-threshold=3` çalıştırır
4. 5 share'i ve root token'ı **tek seferlik** ekrana yazar
5. JSON'u `/root/vault-init-prod.json` (mode 600) içine kaydeder

**Custodian'lar burada**: her custodian kendi share'ini fiziksel zarfa
yazıp imzalar. Ceremony tutanağı (kim hangi share'i aldı, tarih, saat)
DPO tarafından imzalanır.

### 5.4 — İlk unseal (manuel)

3 custodian fiziksel olarak orada:

```bash
ssh kartal@192.168.5.44 'sudo VAULT_ADDR=https://127.0.0.1:8210 vault operator unseal'
# her custodian sırayla kendi share'ini girer
```

3 share sonrası `Sealed: false` olmalı.

---

## 6. Post-init re-import: PKI, AppRole, policy, transit

Yeni prod Vault şu an boş. Mevcut prod'un PKI/AppRole/transit/policy
state'ini re-import et.

**Yöntem A — Snapshot restore (önerilen, atomik)**:

```bash
# Mevcut prod Vault'tan snapshot al (madde 3'tekinden farklı; bu güncel)
ssh kartal@192.168.5.44 'sudo vault operator raft snapshot save /tmp/vault-current.snap'

# Yeni prod Vault'a restore et
ssh kartal@192.168.5.44 'sudo VAULT_ADDR=https://127.0.0.1:8210 \
  vault operator raft snapshot restore -force /tmp/vault-current.snap'
```

Restore sonrası yeni Vault, mevcut prod ile aynı PKI/AppRole/transit'e
sahip olur **ama** yeni Shamir anahtarlarıyla mühürlenir (yeni 3-of-5).
Bu bizim istediğimiz sonuç.

**Yöntem B — Manuel re-import** (snapshot yöntemi başarısız olursa):
`bootstrap.sh configure` adımını yeni Vault'a karşı yeniden çalıştır
(`/tmp/vault-root-token` dosyasına yeni root token'ı yazarak), sonra
`ca-bootstrap.sh` ile PKI'yı yeniden ayarla. **Bu yöntem AppRole
secret_id'lerini sıfırlar — tüm servis env dosyalarını günceller.**

---

## 7. Auto-unseal sealed-file kurulumu (operasyonel orta yol)

HSM gelene kadar her restart'ta 3 custodian'ı odada toplamak
operasyonel olarak imkânsız. Çözüm: 5 share'i tek bir age-encrypted dosyaya
koy, decryption master key'i systemd drop-in ile sağla.

### 7.1 — age key oluştur ve master'ı sakla

```bash
# Yerel makinede (mac veya kartal'ın laptop'u — production hostunda DEĞİL)
age-keygen -o personel-vault-master.age

# Public key'i not al (age1...)
# Secret key'i (AGE-SECRET-KEY-1...) müşteri password vault'una koy
# Backup: tamper-evident envelope #6 (mevcut 5'in yanına) — DPO için
```

### 7.2 — Shares dosyasını şifrele

```bash
# 5 share'i tek dosyaya yaz (her satır bir share, yorum satırları '#' ile)
cat > /tmp/shares-plaintext.txt <<'EOF'
# Personel Vault prod shares — encrypted; do not store plaintext
<share-1>
<share-2>
<share-3>
<share-4>
<share-5>
EOF

# Şifrele
age -r age1<public-key> -o /tmp/vault-shares.enc /tmp/shares-plaintext.txt

# Plaintext'i hemen sil
shred -uz /tmp/shares-plaintext.txt
```

### 7.3 — Şifreli dosyayı hosta yerleştir

```bash
scp /tmp/vault-shares.enc kartal@192.168.5.44:/tmp/
ssh kartal@192.168.5.44 'sudo install -m 600 -o root -g root \
  /tmp/vault-shares.enc /etc/personel/vault-shares.enc'
ssh kartal@192.168.5.44 'rm /tmp/vault-shares.enc'
```

### 7.4 — systemd unit ve master key drop-in

```bash
# Repo'dan unit dosyalarını kopyala
scp infra/systemd/personel-vault-autounseal.service kartal@192.168.5.44:/tmp/
scp infra/compose/vault/auto-unseal-sealed.sh kartal@192.168.5.44:/tmp/

ssh kartal@192.168.5.44 'sudo install -m 644 \
  /tmp/personel-vault-autounseal.service /etc/systemd/system/'
ssh kartal@192.168.5.44 'sudo install -m 750 \
  /tmp/auto-unseal-sealed.sh /opt/personel/infra/compose/vault/'

# Master key drop-in'i fiziksel olarak host konsolundan oluştur (asla SSH ile parolayı transfer etme)
# Hostta:
sudo mkdir -p /etc/systemd/system/personel-vault-autounseal.service.d
sudo nano /etc/systemd/system/personel-vault-autounseal.service.d/master.conf
# İçerik:
#   [Service]
#   Environment=VAULT_AUTOSEAL_KEY=AGE-SECRET-KEY-1XXX...
sudo chmod 600 /etc/systemd/system/personel-vault-autounseal.service.d/master.conf
sudo chown root:root /etc/systemd/system/personel-vault-autounseal.service.d/master.conf

sudo systemctl daemon-reload
sudo systemctl enable personel-vault-autounseal.service

# Eski dev unseal unit'ini devre dışı bırak
sudo systemctl disable personel-vault-unseal.service || true
```

### 7.5 — Test

```bash
# Vault'u manuel mühürle
ssh kartal@192.168.5.44 'sudo vault operator seal'

# Auto-unseal'ı tetikle
ssh kartal@192.168.5.44 'sudo systemctl start personel-vault-autounseal.service'

# Durum
ssh kartal@192.168.5.44 'sudo systemctl status personel-vault-autounseal.service'
ssh kartal@192.168.5.44 'sudo vault status | grep Sealed'
```

`Sealed: false` görmelisin.

---

## 8. (İleride) HSM auto-unseal cutover

HSM cihazı geldiğinde:

1. `config.prod.hcl` içindeki `seal "pkcs11" {...}` bloğunu uncomment et
2. HSM PIN dosyasını `/etc/personel/hsm.pin` (mode 600) olarak yerleştir
3. Vault'u durdur
4. `vault operator migrate -config=config.prod.hcl` çalıştır (Shamir → HSM)
5. Vault'u başlat — artık HSM ile auto-unseal
6. `auto-unseal-sealed.sh` ve `vault-shares.enc` dosyalarını shred et
7. `personel-vault-autounseal.service`'i devre dışı bırak

Bu sayfa Phase 2'de bir HSM-spesifik runbook'la genişletilecek.

---

## 9. Doğrulama matrisi

| Test | Komut | Beklenen sonuç |
|---|---|---|
| Initialize true | `vault status` | `Initialized: true` |
| Sealed false | `vault status` | `Sealed: false` |
| Threshold 3 | `vault status` | `Threshold: 3` |
| Total shares 5 | `vault status` | `Total Shares: 5` |
| PKI mevcut | `vault secrets list` | `pki/` görünüyor |
| Transit anahtarları korundu | `vault list transit/keys` | Beklenen anahtarlar |
| AppRole policy'leri intact | `vault policy list` | `gateway-service`, `admin-api` vb. |
| Cert rotator dry-run | `sudo /opt/personel/infra/scripts/rotate-certs.sh --check` | Tüm 10 servis için "OK" veya "needs rotation" |
| Restart drill — auto-unseal | `sudo systemctl restart personel-vault-autounseal.service` | Service active, `Sealed: false` |
| Audit log akıyor | `sudo tail -f /vault/data/audit.log` | Yeni log satırları |
| Mlock aktif | `sudo grep -i mlock /var/log/vault/*.log` | `mlocked: true` veya error yok |

Tüm testler PASS ise migrasyon **DONE**.

---

## 10. Geri dönüş prosedürü (rollback)

Migrasyon sırasında bir şey ters giderse:

1. **Auto-unseal disable**: `sudo systemctl disable personel-vault-autounseal.service`
2. **Yeni prod container'ı durdur**: `sudo docker stop personel-vault-prod-init`
3. **Eski Vault'u doğrula**: `sudo vault status` — eski instance hala çalışıyor olmalı
4. **Snapshot restore (gerekirse)**: Madde 3'teki `vault-pre-prod.snap` dosyasını
   `vault operator raft snapshot restore` ile mevcut Vault'a uygula
5. **Eski unseal yöntemine dön**: `sudo systemctl enable personel-vault-unseal.service`
6. **Olay raporu**: Customer DPO'ya 1 saat içinde rollback raporu yaz
7. **Post-mortem**: 24 saat içinde root cause analizi, runbook güncellemesi

---

## Ekler

- A. `bootstrap-prod.sh` script kaynak: `infra/compose/vault/bootstrap-prod.sh`
- B. `auto-unseal-sealed.sh` script kaynak: `infra/compose/vault/auto-unseal-sealed.sh`
- C. systemd unit: `infra/systemd/personel-vault-autounseal.service`
- D. Üretim Vault config: `infra/compose/vault/config.prod.hcl`
- E. Sertifika rotasyon servisi: `infra/systemd/personel-cert-rotation.{service,timer}`
- F. Cert envanteri: `infra/scripts/cert-inventory.yaml`
- G. Cert rotator script: `infra/scripts/rotate-certs.sh`

---

*Versiyon 1.0 — Faz 5 Wave 1 madde #41 + #54 — 2026-04-13*
