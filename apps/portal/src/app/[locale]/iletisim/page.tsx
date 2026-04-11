import { redirect } from "next/navigation";
import { getTranslations, getLocale } from "next-intl/server";
import Link from "next/link";
import { Mail, MapPin, ExternalLink, Monitor, Phone } from "lucide-react";
import { getSession } from "@/lib/auth/session";

export async function generateMetadata(): Promise<{ title: string }> {
  const t = await getTranslations("iletisim");
  return { title: t("title") };
}

export default async function IletisimPage(): Promise<JSX.Element> {
  const session = await getSession();
  const locale = await getLocale();

  if (!session) redirect(`/${locale}/giris`);

  const t = await getTranslations("iletisim");
  const dpoEmail = process.env["NEXT_PUBLIC_DPO_EMAIL"] ?? "kvkk@musteri.com.tr";
  const companyName = process.env["NEXT_PUBLIC_COMPANY_NAME"] ?? "[Şirket Adı]";

  return (
    <div className="space-y-8 animate-fade-in max-w-2xl">
      <header className="page-header">
        <h1>{t("title")}</h1>
        <p className="text-warm-600">{t("subtitle")}</p>
      </header>

      {/* DPO contact */}
      <section className="card" aria-labelledby="dpo-contact">
        <h2 id="dpo-contact" className="text-base font-semibold text-warm-800 mb-4">
          {t("dpoContact")}
        </h2>
        <dl className="space-y-3">
          <div className="flex items-start gap-3">
            <Mail className="w-4 h-4 text-portal-400 mt-0.5 flex-shrink-0" aria-hidden="true" />
            <div>
              <dt className="text-xs font-medium text-warm-400 uppercase tracking-wide mb-0.5">
                {t("dpoEmail")}
              </dt>
              <dd>
                <a
                  href={`mailto:${dpoEmail}`}
                  className="text-sm text-portal-600 hover:text-portal-800 underline-offset-2 hover:underline"
                >
                  {dpoEmail}
                </a>
              </dd>
            </div>
          </div>
          <div className="flex items-start gap-3">
            <MapPin className="w-4 h-4 text-portal-400 mt-0.5 flex-shrink-0" aria-hidden="true" />
            <div>
              <dt className="text-xs font-medium text-warm-400 uppercase tracking-wide mb-0.5">
                {t("dpoAddress")}
              </dt>
              <dd className="text-sm text-warm-700">{companyName}</dd>
            </div>
          </div>
        </dl>
      </section>

      {/* Portal application */}
      <section className="card bg-portal-50 border-portal-100" aria-labelledby="portal-apply">
        <h2 id="portal-apply" className="text-sm font-semibold text-portal-700 mb-2">
          {t("portalApplication")}
        </h2>
        <p className="text-sm text-portal-600 leading-relaxed mb-3">
          {t("portalApplicationText")}
        </p>
        <Link
          href={`/${locale}/haklar/yeni-basvuru`}
          className="inline-flex items-center gap-2 text-sm text-portal-600 hover:text-portal-800 font-medium"
        >
          <Monitor className="w-4 h-4" aria-hidden="true" />
          Başvuru Formuna Git
        </Link>
      </section>

      {/* KVKK Board */}
      <section className="card" aria-labelledby="kvkk-board">
        <h2 id="kvkk-board" className="text-base font-semibold text-warm-800 mb-2">
          {t("kvkkBoard")}
        </h2>
        <p className="text-sm text-warm-600 leading-relaxed mb-3">
          {t("kvkkBoardText")}
        </p>
        <dl className="space-y-2 text-sm">
          <div className="flex items-center gap-2">
            <ExternalLink className="w-4 h-4 text-warm-400" aria-hidden="true" />
            <a
              href="https://kvkk.gov.tr"
              target="_blank"
              rel="noopener noreferrer"
              className="text-portal-600 hover:underline underline-offset-2"
            >
              {t("kvkkBoardUrl")}
            </a>
          </div>
          <div className="flex items-start gap-2">
            <MapPin className="w-4 h-4 text-warm-400 mt-0.5" aria-hidden="true" />
            <span className="text-warm-600">{t("kvkkBoardAddress")}</span>
          </div>
        </dl>
      </section>

      {/* IT support */}
      <section className="card" aria-labelledby="it-support">
        <h2 id="it-support" className="text-sm font-semibold text-warm-700 mb-2">
          {t("itSupport")}
        </h2>
        <p className="text-sm text-warm-500 leading-relaxed">{t("itSupportText")}</p>
      </section>

      {/* Response time note */}
      <div className="rounded-xl bg-trust-50 border border-trust-200 px-4 py-3 text-sm text-trust-700">
        {t("responseTime")}
      </div>
    </div>
  );
}
