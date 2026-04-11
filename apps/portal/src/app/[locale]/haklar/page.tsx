import { redirect } from "next/navigation";
import { getTranslations, getLocale } from "next-intl/server";
import Link from "next/link";
import { Scale, ExternalLink, ArrowRight } from "lucide-react";
import { getSession } from "@/lib/auth/session";
import { LegalTerm } from "@/components/common/legal-term";
import type { DSRRequestType } from "@/lib/api/types";

interface RightItem {
  key: string;
  dsrType: DSRRequestType;
}

const RIGHTS: RightItem[] = [
  { key: "access", dsrType: "access" },
  { key: "purpose", dsrType: "access" },
  { key: "transfer", dsrType: "access" },
  { key: "rectify", dsrType: "rectify" },
  { key: "erase", dsrType: "erase" },
  { key: "notify", dsrType: "erase" },
  { key: "object", dsrType: "object" },
  { key: "compensation", dsrType: "object" },
];

export async function generateMetadata(): Promise<{ title: string }> {
  const t = await getTranslations("haklar");
  return { title: t("title") };
}

export default async function HaklarPage(): Promise<JSX.Element> {
  const session = await getSession();
  const locale = await getLocale();

  if (!session) redirect(`/${locale}/giris`);

  const t = await getTranslations("haklar");
  const tRights = await getTranslations("haklar.rights");

  return (
    <div className="space-y-8 animate-fade-in">
      <header className="page-header">
        <div className="flex items-center gap-3 mb-2">
          <div
            className="w-10 h-10 rounded-xl bg-portal-100 flex items-center justify-center"
            aria-hidden="true"
          >
            <Scale className="w-5 h-5 text-portal-600" />
          </div>
          <h1>{t("title")}</h1>
        </div>
        <p className="text-warm-600">{t("subtitle")}</p>
        <p className="mt-2 text-sm text-warm-500 leading-relaxed max-w-2xl">
          {t("intro")}
        </p>
      </header>

      {/* Rights list */}
      <section aria-label="KVKK m.11 hakları">
        <ol className="space-y-4">
          {RIGHTS.map((right) => (
            <li key={right.key}>
              <article className="card-hover">
                <div className="flex items-start justify-between gap-4">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1.5">
                      <h2 className="font-semibold text-warm-900 text-base">
                        {tRights(`${right.key}.title`)}
                      </h2>
                      <span className="badge-neutral text-xs font-mono">
                        {tRights(`${right.key}.legalRef`)}
                      </span>
                    </div>

                    {/* Legal description — formal */}
                    <p className="text-sm text-warm-600 leading-relaxed legal-text mb-3">
                      {tRights(`${right.key}.description`)}
                    </p>

                    {/* Plain explanation */}
                    <div className="rounded-lg bg-portal-50 border border-portal-100 px-3 py-2.5 text-sm text-portal-700 leading-relaxed">
                      <span className="text-xs font-semibold text-portal-500 uppercase tracking-wide block mb-0.5">
                        Sade Türkçe Açıklama
                      </span>
                      {tRights(`${right.key}.plain`)}
                    </div>
                  </div>

                  {/* Use this right CTA */}
                  <Link
                    href={`/${locale}/haklar/yeni-basvuru?type=${right.dsrType}`}
                    className="flex-shrink-0 inline-flex items-center gap-1.5 text-xs font-medium text-portal-600 hover:text-portal-800 bg-portal-50 hover:bg-portal-100 border border-portal-200 px-3 py-2 rounded-xl transition-colors"
                    aria-label={`${tRights(`${right.key}.title`)} hakkını kullan`}
                  >
                    {t("useThisRight")}
                    <ArrowRight className="w-3 h-3" aria-hidden="true" />
                  </Link>
                </div>
              </article>
            </li>
          ))}
        </ol>
      </section>

      {/* Footer info */}
      <footer className="space-y-4">
        <div className="rounded-xl bg-trust-50 border border-trust-200 px-4 py-3 flex items-center gap-3 text-sm text-trust-700">
          <span className="text-trust-500 font-bold text-lg" aria-hidden="true">⏱</span>
          <span>
            <strong>{t("responseTime")}</strong> — KVKK m.13 uyarınca ücretsizdir.
          </span>
        </div>

        <div className="rounded-xl bg-warm-100 border border-warm-200 px-4 py-3 text-sm text-warm-600 leading-relaxed flex items-start gap-3">
          <ExternalLink className="w-4 h-4 text-warm-400 flex-shrink-0 mt-0.5" aria-hidden="true" />
          <span>
            {t("kvkkBoard")}{" "}
            <a
              href="https://kvkk.gov.tr"
              target="_blank"
              rel="noopener noreferrer"
              className="text-portal-600 font-medium hover:underline"
            >
              kvkk.gov.tr
            </a>
          </span>
        </div>
      </footer>
    </div>
  );
}
