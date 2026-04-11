import { redirect } from "next/navigation";
import { getTranslations, getLocale } from "next-intl/server";
import Link from "next/link";
import { InboxIcon, Plus, ArrowRight } from "lucide-react";
import { getSession } from "@/lib/auth/session";
import { listMyDSRs } from "@/lib/api/dsr";
import { RequestTimeline } from "@/components/dsr/request-timeline";
import { formatDate } from "@/lib/utils";
import { cn } from "@/lib/utils";
import type { DSRState, DSRRequestType } from "@/lib/api/types";

export async function generateMetadata(): Promise<{ title: string }> {
  const t = await getTranslations("basvurularim");
  return { title: t("title") };
}

const STATE_BADGE: Record<DSRState, string> = {
  open: "badge-info",
  at_risk: "badge-warning",
  overdue: "badge-danger",
  closed: "badge-success",
  rejected: "badge-neutral",
};

const TYPE_LABELS: Record<DSRRequestType, string> = {
  access: "Erişim",
  rectify: "Düzeltme",
  erase: "Silme",
  object: "İtiraz",
  restrict: "Kısıtlama",
  portability: "Taşınabilirlik",
};

export default async function BasvurularimPage(): Promise<JSX.Element> {
  const session = await getSession();
  const locale = await getLocale() as "tr" | "en";

  if (!session) redirect(`/${locale}/giris`);

  const t = await getTranslations("basvurularim");

  let dsrList = null;
  let loadError = false;

  try {
    dsrList = await listMyDSRs(session.accessToken);
  } catch {
    loadError = true;
  }

  return (
    <div className="space-y-6 animate-fade-in">
      <header className="page-header flex items-start justify-between">
        <div>
          <div className="flex items-center gap-3 mb-2">
            <div
              className="w-10 h-10 rounded-xl bg-portal-100 flex items-center justify-center"
              aria-hidden="true"
            >
              <InboxIcon className="w-5 h-5 text-portal-600" />
            </div>
            <h1>{t("title")}</h1>
          </div>
          <p className="text-warm-600">{t("subtitle")}</p>
        </div>
        <Link
          href={`/${locale}/haklar/yeni-basvuru`}
          className="inline-flex items-center gap-2 bg-portal-600 hover:bg-portal-700 text-white font-medium py-2.5 px-4 rounded-xl text-sm transition-colors flex-shrink-0"
        >
          <Plus className="w-4 h-4" aria-hidden="true" />
          <span className="hidden sm:block">{t("newApplication")}</span>
        </Link>
      </header>

      {loadError ? (
        <div className="card text-center py-8">
          <p className="text-sm text-warm-500">
            Başvurular şu an yüklenemiyor. Lütfen daha sonra tekrar deneyin.
          </p>
        </div>
      ) : !dsrList || dsrList.items.length === 0 ? (
        <div className="card text-center py-14">
          <div
            className="w-12 h-12 rounded-xl bg-warm-100 flex items-center justify-center mx-auto mb-4"
            aria-hidden="true"
          >
            <InboxIcon className="w-6 h-6 text-warm-400" />
          </div>
          <h2 className="text-base font-medium text-warm-800 mb-2">
            {t("noApplications")}
          </h2>
          <p className="text-sm text-warm-500 mb-6">{t("noApplicationsDetail")}</p>
          <Link
            href={`/${locale}/haklar/yeni-basvuru`}
            className="inline-flex items-center gap-2 text-sm text-portal-600 font-medium hover:underline underline-offset-2"
          >
            {t("newApplication")}
            <ArrowRight className="w-4 h-4" aria-hidden="true" />
          </Link>
        </div>
      ) : (
        <ol className="space-y-4" aria-label="Başvuru listesi">
          {dsrList.items.map((dsr) => (
            <li key={dsr.id}>
              <article className="card-hover">
                <div className="flex items-start gap-4">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-2 flex-wrap">
                      <span className="font-medium text-warm-900 text-sm">
                        {TYPE_LABELS[dsr.request_type] ?? dsr.request_type}
                      </span>
                      <span className={cn("badge text-xs", STATE_BADGE[dsr.state])}>
                        {t(`states.${dsr.state}`)}
                      </span>
                      <code className="text-xs text-warm-300 font-mono">
                        #{dsr.id.slice(0, 8)}
                      </code>
                    </div>

                    <div className="text-xs text-warm-500 mb-4">
                      {t("created")}: {formatDate(dsr.created_at, locale)}
                    </div>

                    {/* SLA timeline */}
                    <RequestTimeline
                      createdAt={dsr.created_at}
                      slaDeadline={dsr.sla_deadline}
                      state={dsr.state}
                      locale={locale}
                    />
                  </div>

                  <Link
                    href={`/${locale}/basvurularim/${dsr.id}`}
                    className="flex-shrink-0 inline-flex items-center gap-1.5 text-xs text-portal-600 hover:text-portal-800 font-medium"
                    aria-label={`Başvuru #${dsr.id.slice(0, 8)} detaylarını gör`}
                  >
                    {t("viewDetail")}
                    <ArrowRight className="w-3 h-3" aria-hidden="true" />
                  </Link>
                </div>
              </article>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
