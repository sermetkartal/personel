/**
 * Plain-Turkish explanations for KVKK legal terms.
 * Used by the LegalTerm component to show hover/click tooltips.
 * Keys are keyed by KVKK article reference or concept identifier.
 */

export type LegalTermKey =
  | "kvkk"
  | "m10"
  | "m11"
  | "m5f"
  | "m5c"
  | "m6"
  | "m7"
  | "m12"
  | "meruMenfaat"
  | "verbis"
  | "dpo"
  | "dpia"
  | "dsr"
  | "dlp"
  | "legalHold"
  | "dataController"
  | "dataProcessor"
  | "anonimHale";

export const LEGAL_TERMS_TR: Record<LegalTermKey, { term: string; plain: string }> = {
  kvkk: {
    term: "KVKK (6698 sayılı Kanun)",
    plain:
      "Kişisel Verilerin Korunması Kanunu — Türkiye'de kişisel verilerin işlenmesini, korunmasını ve veri sahibi haklarını düzenleyen temel kanundur. 2016 yılında yürürlüğe girmiştir.",
  },
  m10: {
    term: "KVKK m.10 — Aydınlatma Yükümlülüğü",
    plain:
      "Kişisel verilerinizi toplayan kuruluşun sizi bilgilendirmesi zorunludur: ne topladığını, neden topladığını, ne kadar saklayacağını ve haklarınızın neler olduğunu açıkça anlatmak zorundadır.",
  },
  m11: {
    term: "KVKK m.11 — Veri Sahibi Hakları",
    plain:
      "Hakkınızda toplanan veriler üzerinde çeşitli haklarınız vardır: bu verilere erişme, yanlışsa düzeltme, silinmesini isteme, itiraz etme gibi. Bu haklardan birini kullanmak için Şeffaflık Portalı'ndan başvurabilirsiniz.",
  },
  m5f: {
    term: "KVKK m.5/2-f — Meşru Menfaat",
    plain:
      "Şirketin, sizin temel haklarınıza zarar vermemek koşuluyla, kendi meşru işletme menfaatleri için verilerinizi işleyebileceği hukuki dayanaktır. Güvenlik ve iş sözleşmesi denetimi bu kapsamda değerlendirilir.",
  },
  m5c: {
    term: "KVKK m.5/2-c — Sözleşmenin İfası",
    plain:
      "İş sözleşmenizin yerine getirilmesi için zorunlu olan veriler bu hukuki dayanakla işlenebilir. Çalışma süresi takibi bu kategoriye girer.",
  },
  m6: {
    term: "KVKK m.6 — Özel Nitelikli Kişisel Veri",
    plain:
      "Sağlık, din, ırk, siyasi görüş, biyometrik veri, sendika üyeliği gibi özellikle hassas kategorilerdeki verilerdir. Bu veriler için kanun çok daha sıkı koruma öngörmüştür.",
  },
  m7: {
    term: "KVKK m.7 — Silme, Yok Etme, Anonimleştirme",
    plain:
      "Saklama süresi dolan veya artık gerekli olmayan verilerin silinmesi, yok edilmesi ya da kimlikten arındırılması zorunludur. Personel platformunda bu işlem otomatik TTL (zaman aşımı) mekanizmalarıyla gerçekleştirilir.",
  },
  m12: {
    term: "KVKK m.12 — Veri Güvenliği",
    plain:
      "Verilerinizi toplayan kuruluş, bu verileri korumak için teknik ve idari önlemler almak zorundadır. Şifreleme, erişim kontrolü ve denetim kaydı bu yükümlülüklerin somut örnekleridir.",
  },
  meruMenfaat: {
    term: "Meşru Menfaat",
    plain:
      "Şirketin, sizin rızanıza gerek kalmaksızın, kendi güvenlik ve operasyonel gereksinimlerini karşılamak için verilerinizi işleyebileceği hukuki zemin. Ancak bu hak sınırsız değildir; temel haklarınıza zarar vermemesi koşuluna bağlıdır.",
  },
  verbis: {
    term: "VERBİS",
    plain:
      "Veri Sorumluları Sicil Bilgi Sistemi. Kurum ve kuruluşların hangi verileri topladığını, neden topladığını ve ne kadar sakladığını Kişisel Verileri Koruma Kurulu'na bildirdiği resmi kayıt sistemidir.",
  },
  dpo: {
    term: "Veri Koruma Görevlisi (DPO)",
    plain:
      "Şirketin KVKK uyumundan sorumlu, gelen veri sahibi başvurularını inceleyen ve yanıtlayan yetkili kişidir. Başvurularınız doğrudan DPO'ya iletilir.",
  },
  dpia: {
    term: "Veri Koruma Etki Değerlendirmesi (DPIA)",
    plain:
      "Yüksek riskli veri işleme faaliyetleri başlatılmadan önce yapılması gereken sistematik risk değerlendirmesidir. DLP özelliğini etkinleştirmek için bu değerlendirmenin yapılıp imzalanmış olması zorunludur.",
  },
  dsr: {
    term: "Veri Sahibi Başvurusu (DSR)",
    plain:
      "KVKK m.11 kapsamındaki haklarınızı kullanmak için yaptığınız resmi başvurudur. Bu portal üzerinden başvurabilirsiniz; yanıt süresi 30 gündür.",
  },
  dlp: {
    term: "Veri Kaybı Önleme (DLP)",
    plain:
      "Hassas verilerin (örneğin kredi kartı numarası, TCKN, ticari sır) yetkisiz kanallarla dışarı sızmasını otomatik olarak tespit eden ve engelleyen sistem. Personel'de bu sistem varsayılan olarak kapalıdır.",
  },
  legalHold: {
    term: "Yasal Saklama (Legal Hold)",
    plain:
      "Aktif bir dava, soruşturma veya Kurul incelemesi süresince ilgili verilerin normal silme döngüsünden çıkarılarak korunduğu durumdur. Bu durum ayrıca denetim kaydına işlenir.",
  },
  dataController: {
    term: "Veri Sorumlusu",
    plain:
      "Kişisel verilerin işlenme amaçlarını ve yöntemlerini belirleyen kurum veya kişidir. On-prem kurulumda Personel platformunu kullanan şirket veri sorumlusudur; Personel yazılım firması değildir.",
  },
  dataProcessor: {
    term: "Veri İşleyen",
    plain:
      "Veri sorumlusunun talimatlarıyla onun adına kişisel verileri işleyen kişi veya kurumdur. Personel firması, müşterinin verilerine erişmediğinden KVKK'da veri işleyen olarak konumlandırılmamaktadır.",
  },
  anonimHale: {
    term: "Anonimleştirme",
    plain:
      "Kişisel verinin, hiçbir yöntemle artık gerçek kişiyle ilişkilendirilemez hale getirilmesidir. Anonimleştirilen veriler KVKK kapsamından çıkar ve süresiz saklanabilir.",
  },
};

export const LEGAL_TERMS_EN: Record<LegalTermKey, { term: string; plain: string }> = {
  kvkk: {
    term: "KVKK (Law No. 6698)",
    plain:
      "Personal Data Protection Law — the primary law governing the processing and protection of personal data in Turkey, and data subject rights. It came into force in 2016.",
  },
  m10: {
    term: "KVKK Art. 10 — Disclosure Obligation",
    plain:
      "The organisation collecting your personal data must inform you: what it collects, why, how long it will keep it, and what your rights are.",
  },
  m11: {
    term: "KVKK Art. 11 — Data Subject Rights",
    plain:
      "You have various rights over data collected about you: access, correction if incorrect, deletion, objection, etc. You can submit a request through the Transparency Portal.",
  },
  m5f: {
    term: "KVKK Art. 5/2-f — Legitimate Interest",
    plain:
      "The legal basis allowing the company to process your data for its own legitimate business interests, provided it does not harm your fundamental rights. Security and work contract monitoring fall under this.",
  },
  m5c: {
    term: "KVKK Art. 5/2-c — Performance of Contract",
    plain:
      "Data necessary for the performance of your employment contract can be processed under this legal basis. Working hours tracking falls in this category.",
  },
  m6: {
    term: "KVKK Art. 6 — Special Categories of Personal Data",
    plain:
      "Particularly sensitive categories including health, religion, race, political opinion, biometric data, and union membership. The law provides much stricter protection for this data.",
  },
  m7: {
    term: "KVKK Art. 7 — Erasure, Destruction, Anonymisation",
    plain:
      "Data whose retention period has expired or is no longer needed must be deleted, destroyed, or anonymised. In the Personel platform, this is done automatically via TTL (time-to-live) mechanisms.",
  },
  m12: {
    term: "KVKK Art. 12 — Data Security",
    plain:
      "The organisation collecting your data must take technical and administrative measures to protect it. Encryption, access control, and audit logging are concrete examples of these obligations.",
  },
  meruMenfaat: {
    term: "Legitimate Interest",
    plain:
      "The legal ground allowing the company to process your data without your consent, to meet its own security and operational needs. This right is not unlimited; it is subject to not harming your fundamental rights.",
  },
  verbis: {
    term: "VERBİS",
    plain:
      "Data Controllers' Registry Information System. The official registry where organisations report to the Personal Data Protection Authority what data they collect, why, and how long they retain it.",
  },
  dpo: {
    term: "Data Protection Officer (DPO)",
    plain:
      "The person responsible for the company's KVKK compliance who reviews and responds to incoming data subject requests. Your requests are forwarded directly to the DPO.",
  },
  dpia: {
    term: "Data Protection Impact Assessment (DPIA)",
    plain:
      "A systematic risk assessment required before starting high-risk data processing activities. Enabling the DLP feature requires this assessment to be completed and signed.",
  },
  dsr: {
    term: "Data Subject Request (DSR)",
    plain:
      "A formal request you make to exercise your rights under KVKK Article 11. You can submit requests through this portal; the response time is 30 days.",
  },
  dlp: {
    term: "Data Loss Prevention (DLP)",
    plain:
      "A system that automatically detects and prevents sensitive data (e.g. credit card numbers, national ID numbers, trade secrets) from leaking through unauthorised channels. In Personel, this system is off by default.",
  },
  legalHold: {
    term: "Legal Hold",
    plain:
      "When relevant data is protected from normal deletion cycles during an active lawsuit, investigation, or Authority review. This is also recorded in the audit log.",
  },
  dataController: {
    term: "Data Controller",
    plain:
      "The organisation or person that determines the purposes and means of processing personal data. In on-premise installations, the company using the Personel platform is the data controller; the Personel software vendor is not.",
  },
  dataProcessor: {
    term: "Data Processor",
    plain:
      "A person or organisation that processes personal data on behalf of the data controller. Since Personel does not access customer data, it is not classified as a data processor under KVKK.",
  },
  anonimHale: {
    term: "Anonymisation",
    plain:
      "Making personal data unable to be associated with a real person by any means. Anonymised data falls outside the scope of KVKK and can be retained indefinitely.",
  },
};
