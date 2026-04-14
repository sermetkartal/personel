import { redirect } from "next/navigation";
import { getTranslations, getLocale } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { LegalTerm } from "@/components/common/legal-term";
import { AcknowledgePanel } from "@/components/aydinlatma/acknowledge-panel";

const AYDINLATMA_VERSION = "1.0.0";
const AYDINLATMA_LAST_UPDATED = "2026-04-13";

export async function generateMetadata(): Promise<{ title: string }> {
  const t = await getTranslations("aydinlatma");
  return { title: t("title") };
}

/**
 * Aydınlatma Metni page — full KVKK m.10 disclosure.
 *
 * Layout: two-column — legal text on the left, plain-Turkish explanations
 * in a sticky sidebar on the right.
 *
 * The content mirrors aydinlatma-metni-template.md with company placeholders
 * showing as configurable environment values.
 */
export default async function AydinlatmaPage(): Promise<JSX.Element> {
  const session = await getSession();
  const locale = await getLocale();

  if (!session) redirect(`/${locale}/giris`);

  const t = await getTranslations("aydinlatma");
  const companyName = process.env["NEXT_PUBLIC_COMPANY_NAME"] ?? "[Şirket Adı]";
  const dpoEmail = process.env["NEXT_PUBLIC_DPO_EMAIL"] ?? "kvkk@musteri.com.tr";
  const baseUrl = process.env["NEXT_PUBLIC_BASE_URL"] ?? "https://personel-portal.musteri.com.tr";

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Page header */}
      <header className="page-header">
        <h1>{t("title")}</h1>
        <p className="mt-2 text-warm-600">{t("subtitle")}</p>
        <div className="mt-3 flex items-center gap-3">
          <span className="badge-neutral">{t("lastVersion")}</span>
          <a
            href="#ack-heading"
            className="text-xs text-portal-600 hover:underline"
          >
            {t("jumpToAck")}
          </a>
        </div>
      </header>

      {/* Two-column layout */}
      <div className="legal-layout">
        {/* LEFT — Legal text */}
        <article className="card prose prose-sm max-w-none prose-headings:font-semibold prose-headings:text-warm-900 prose-p:text-warm-700">
          <section aria-labelledby="veri-sorumlusu">
            <h2 id="veri-sorumlusu">1. Veri Sorumlusunun Kimliği</h2>
            <p>
              KVKK m.3/1-ı uyarınca, işlenen kişisel verilerinizin veri sorumlusu{" "}
              <strong>{companyName}</strong>'dir. Personel Platformu yazılımını sağlayan
              firma, yalnızca{" "}
              <LegalTerm termKey="dataProcessor">yazılım sağlayıcı</LegalTerm>{" "}
              konumunda olup <LegalTerm termKey="kvkk">KVKK</LegalTerm> anlamında{" "}
              <LegalTerm termKey="dataController">veri sorumlusu</LegalTerm> veya veri
              işleyen değildir; kişisel verilerinize erişimi bulunmamaktadır.
            </p>
          </section>

          <hr className="my-6 border-warm-200" />

          <section aria-labelledby="veri-kategorileri">
            <h2 id="veri-kategorileri">2. İşlenen Kişisel Veri Kategorileri</h2>
            <p>
              Personel Platformu, iş bilgisayarınızda aşağıdaki kategorilerde kişisel veriyi
              toplar ve işler:
            </p>
            <table className="w-full text-sm border-collapse">
              <thead>
                <tr className="border-b border-warm-200">
                  <th className="text-left py-2 pr-4 font-semibold text-warm-700">Kategori</th>
                  <th className="text-left py-2 font-semibold text-warm-700">İçerik</th>
                </tr>
              </thead>
              <tbody className="text-warm-600">
                {[
                  ["Kimlik ve oturum verileri", "Kullanıcı adı, Windows oturum tanımlayıcısı, cihaz tanımlayıcısı, oturum açma/kapama kayıtları"],
                  ["Uygulama ve süreç kullanım verileri", "Çalıştırılan uygulamalar, süreçler, ön plan pencereleri, pencere başlıkları, kullanım süreleri"],
                  ["Ekran verileri", "Periyodik ve olay tetiklemeli ekran görüntüleri, kısa ekran video klipleri"],
                  ["Dosya sistemi olayları", "Oluşturma, okuma, yazma, silme, adlandırma, kopyalama olayları; dosya yolu ve boyut bilgisi"],
                  ["Pano verileri", "Pano kullanım meta verisi; şifrelenmiş içerik (yalnızca DLP kural motoruna açık)"],
                  ["Klavye verileri", "İstatistiksel sayaçlar (tuş sayısı, geri silme sayısı); şifrelenmiş içerik (Şirket yöneticilerince teknik olarak okunamayan biçimde, yalnızca veri sızıntısı önleme kural motoruna açık)"],
                  ["Yazıcı verileri", "Yazıcı işi metadata'sı (belge adı, sayfa sayısı, yazıcı kimliği)"],
                  ["USB ve harici aygıt verileri", "Takma/çıkarma olayları, aygıt tanımlayıcıları"],
                  ["Ağ verileri", "Ağ akış özetleri, DNS sorguları, TLS SNI bilgisi"],
                  ["Canlı izleme verileri", "İnceleme gerekçesi ve İK onayı olduğunda, zaman sınırlı canlı ekran izleme"],
                  ["Politika ve denetim verileri", "Engellenen uygulama/URL olayları, ajan sağlık durumu, denetim kayıtları"],
                ].map(([cat, content]) => (
                  <tr key={cat} className="border-b border-warm-100 last:border-0">
                    <td className="py-2 pr-4 font-medium text-warm-800 align-top">{cat}</td>
                    <td className="py-2 align-top">{content}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </section>

          <hr className="my-6 border-warm-200" />

          <section aria-labelledby="amaclar">
            <h2 id="amaclar">3. Kişisel Verilerin İşlenme Amaçları</h2>
            <p>Kişisel verileriniz aşağıdaki amaçlarla işlenmektedir:</p>
            <ol className="space-y-2 text-warm-600">
              <li><strong>Bilgi güvenliğinin sağlanması:</strong> İç tehdit tespiti, zararlı yazılım ve veri sızıntısının önlenmesi.</li>
              <li><strong>Şirket varlıklarının korunması:</strong> Donanım, yazılım lisansları, ticari sır ve müşteri verisinin korunması.</li>
              <li><strong>İş sözleşmesinin ifasının denetlenmesi:</strong> İş sözleşmesinden doğan yükümlülüklerinizin yerine getirilmesinin izlenmesi.</li>
              <li><strong>Politika uyumunun denetlenmesi:</strong> Şirket iç politikalarına uyumun kontrolü.</li>
              <li><strong>Hukuki uyuşmazlıklarda delil temini:</strong> İş mahkemesi veya resmi mercilerin talebi hâlinde gerekli kanıt sağlanması.</li>
              <li><strong>KVKK m.12 kapsamında veri güvenliği yükümlülüklerinin yerine getirilmesi.</strong></li>
            </ol>
            <p className="mt-4 text-sm font-medium text-warm-800 bg-trust-50 rounded-xl px-4 py-3 border border-trust-200">
              Özellikle belirtmek isteriz ki, Personel Platformu performans değerlendirmesi,
              otomatik karar verme veya profil oluşturma amacıyla kullanılmaz.
            </p>
          </section>

          <hr className="my-6 border-warm-200" />

          <section aria-labelledby="hukuki-dayanak">
            <h2 id="hukuki-dayanak">4. Kişisel Verilerin Toplanma Yöntemi ve Hukuki Sebebi</h2>
            <p>
              Kişisel verileriniz, iş bilgisayarınıza kurulan Personel uç nokta ajanı
              aracılığıyla otomatik yollarla toplanır. Veriler Şirket'in altyapısı dışına{" "}
              <strong>çıkarılmaz</strong>.
            </p>
            <p>Hukuki sebepler:</p>
            <ul>
              <li>
                <LegalTerm termKey="m5c">m.5/2-c</LegalTerm> — İş sözleşmesinin ifasıyla
                doğrudan ilgili veri işleme.
              </li>
              <li>
                <strong>m.5/2-ç</strong> — Hukuki yükümlülük (KVKK m.12, 5651 sayılı Kanun).
              </li>
              <li>
                <LegalTerm termKey="m5f">m.5/2-f</LegalTerm> —{" "}
                <LegalTerm termKey="meruMenfaat">Meşru menfaat</LegalTerm> (bilgi güvenliği,
                şirket varlıklarının korunması).
              </li>
            </ul>
          </section>

          <hr className="my-6 border-warm-200" />

          <section aria-labelledby="aktarim">
            <h2 id="aktarim">5. Kişisel Verilerin Aktarımı</h2>
            <p>
              Personel Platformu on-prem kurulu olduğundan, kişisel verileriniz{" "}
              <strong>hiçbir üçüncü kişiye ve hiçbir yurt dışına aktarılmamaktadır</strong>.
              Yasal yükümlülük doğması hâlinde ilgili mercilere{" "}
              <LegalTerm termKey="kvkk">KVKK</LegalTerm> m.8/2-ç uyarınca aktarılabilir.
            </p>
          </section>

          <hr className="my-6 border-warm-200" />

          <section aria-labelledby="saklama">
            <h2 id="saklama">6. Saklama Süreleri</h2>
            <ul className="text-warm-600 space-y-1 text-sm">
              {[
                ["Ekran görüntüleri", "30 gün"],
                ["Ekran video klipleri", "14 gün"],
                ["Klavye şifreli içerik", "14 gün"],
                ["Klavye istatistikleri", "90 gün"],
                ["Süreç/pencere olayları", "90 gün"],
                ["Dosya sistemi olayları", "180 gün"],
                ["USB olayları", "365 gün"],
                ["Canlı izleme oturum denetim kayıtları", "5 yıl (hesap verebilirlik amacıyla)"],
              ].map(([cat, period]) => (
                <li key={cat} className="flex gap-2">
                  <span className="font-medium text-warm-800 min-w-[220px]">{cat}:</span>
                  <span>{period}</span>
                </li>
              ))}
            </ul>
          </section>

          <hr className="my-6 border-warm-200" />

          <section aria-labelledby="canli-izleme">
            <h2 id="canli-izleme">7. Canlı Ekran İzleme Özelliği</h2>
            <p>
              Personel Platformu canlı ekran izleme özelliği içerir. Bu özellik şirketin
              ikili onay mekanizmasına tabidir: bir yönetici talebi <strong>ve</strong> İnsan
              Kaynakları rolündeki ayrı bir çalışanın onayı olmadan başlatılamaz. Her oturum
              en fazla 15 dakika sürebilir ve değiştirilemez denetim defterinde saklanır.
            </p>
            <p>
              Canlı izleme esnasında ekranınızda gösterge bulunmayabilir; ancak bu özelliğin
              varlığından işbu aydınlatma metni ile peşin bilgilendirilmektesiniz. Şeffaflık
              Portalı üzerinden kendinize ait geçmiş izleme oturumlarını görebilirsiniz.
            </p>
          </section>

          <hr className="my-6 border-warm-200" />

          <section aria-labelledby="klavye-gizlilik">
            <h2 id="klavye-gizlilik">8. Klavye İçeriğinin Kriptografik Olarak Korunması</h2>
            <p>
              Klavye içeriğiniz iş bilgisayarınızda şifrelenerek saklanır. Bu içeriğe
              yöneticiler teknik olarak erişememektedir; şifre çözme işlemi yalnızca izole
              bir DLP motoru tarafından, önceden belirlenmiş kurallarla eşleşme aramak için
              yapılır. Ham klavye içeriğiniz insan tarafından okunmamaktadır.
            </p>
          </section>

          <hr className="my-6 border-warm-200" />

          <section aria-labelledby="haklar">
            <h2 id="haklar">9. <LegalTerm termKey="m11">KVKK m.11</LegalTerm> Kapsamındaki Haklarınız</h2>
            <p>KVKK m.11 uyarınca aşağıdaki haklara sahipsiniz:</p>
            <ol className="list-[lower-alpha] pl-5 space-y-1 text-warm-600 text-sm">
              <li>Kişisel verinizin işlenip işlenmediğini öğrenme</li>
              <li>İşlenmişse buna ilişkin bilgi talep etme</li>
              <li>İşlenme amacını ve amacına uygun kullanılıp kullanılmadığını öğrenme</li>
              <li>Yurt içinde veya yurt dışında aktarıldığı üçüncü kişileri bilme</li>
              <li>Eksik veya yanlış işlenmişse düzeltilmesini isteme</li>
              <li>Şartlar çerçevesinde silinmesini veya yok edilmesini isteme</li>
              <li>Yukarıdaki işlemlerin aktarıldığı üçüncü kişilere bildirilmesini isteme</li>
              <li>Münhasıran otomatik sistemler vasıtasıyla analiz edilerek aleyhinize sonuç ortaya çıkmasına itiraz etme</li>
              <li>KVKK'ya aykırı işleme sebebiyle zarara uğramanız hâlinde giderilmesini talep etme</li>
            </ol>
            <div className="mt-4 rounded-xl bg-portal-50 border border-portal-100 p-4 text-sm text-portal-700">
              <strong>Başvuru:</strong> Şeffaflık Portalı ({baseUrl}) ·{" "}
              E-posta: {dpoEmail} · Yanıt süresi: 30 gün (KVKK m.13)
            </div>
          </section>
        </article>

        {/* RIGHT — Plain-Turkish sidebar */}
        <aside
          aria-label="Sade Türkçe Açıklamalar"
          className="space-y-4 lg:sticky lg:top-24 lg:self-start"
        >
          <div className="card bg-portal-50 border-portal-100">
            <h2 className="text-sm font-semibold text-portal-700 mb-3">
              {t("plainSidebarTitle")}
            </h2>
            <p className="text-xs text-portal-600 leading-relaxed">
              {t("plainSidebarHint")}
            </p>
          </div>

          {[
            { heading: "Veri Sorumlusu", body: t("dataControllerPlain") },
            { heading: "Veri Kategorileri", body: t("dataCategoriesPlain") },
            { heading: "Amaçlar", body: t("purposesPlain") },
            { heading: "Hukuki Dayanak", body: t("legalBasisPlain") },
            { heading: "Aktarım", body: t("dataTransferPlain") },
            { heading: "Saklama", body: t("retentionPeriodsPlain") },
            { heading: "Canlı İzleme", body: t("liveViewPlain") },
            { heading: "Klavye Gizliliği", body: t("keystrokePrivacyPlain") },
            { heading: "Haklarınız", body: t("yourRightsPlain") },
          ].map((item) => (
            <div key={item.heading} className="card border-l-4 border-l-portal-300">
              <h3 className="text-xs font-semibold text-portal-700 uppercase tracking-wide mb-1.5">
                {item.heading}
              </h3>
              <p className="text-xs text-warm-600 leading-relaxed">{item.body}</p>
            </div>
          ))}
        </aside>
      </div>

      {/* Acknowledge panel — PDF download + "Kabul Ediyorum" checkbox */}
      <AcknowledgePanel
        accessToken={session.accessToken}
        version={AYDINLATMA_VERSION}
        lastUpdated={AYDINLATMA_LAST_UPDATED}
        alreadyAcknowledged={session.firstLoginAcknowledged}
      />
    </div>
  );
}
