import { redirect, notFound } from "next/navigation";
import { getTranslations, getLocale } from "next-intl/server";
import Link from "next/link";
import { ChevronLeft } from "lucide-react";
import { getSession } from "@/lib/auth/session";
import { listMyDSRs } from "@/lib/api/dsr";
import { RequestTimeline } from "@/components/dsr/request-timeline";
import { ResponseView } from "@/components/dsr/response-view";
import { formatDate } from "@/lib/utils";

interface DSRDetailPageProps {
  params: Promise<{ id: string; locale: string }>;
}

export async function generateMetadata(): Promise<{ title: string }> {
  const t = await getTranslations("basvuruDetay");
  return { title: t("title") };
}

export default async function DSRDetailPage({
  params,
}: DSRDetailPageProps): Promise<JSX.Element> {
  const { id } = await params;
  const session = await getSession();
  const locale = await getLocale() as "tr" | "en";

  if (!session) redirect(`/${locale}/giris`);

  const t = await getTranslations("basvuruDetay");

  // Fetch the specific DSR by filtering the list
  // (The API only exposes GET /v1/me/dsr — no single-item endpoint for portal)
  let dsr = null;
  try {
    const list = await listMyDSRs(session.accessToken, 1, 100);
    dsr = list.items.find((d) => d.id === id) ?? null;
  } catch {
    // silently fail
  }

  if (!dsr) notFound();

  const TYPE_LABELS: Record<string, string> = {
    access: "Erişim",
    rectify: "Düzeltme",
    erase: "Silme",
    object: "İtiraz",
    restrict: "Kısıtlama",
    portability: "Taşınabilirlik",
  };

  return (
    <div className="space-y-6 animate-fade-in max-w-2xl">
      {/* Back link */}
      <Link
        href={`/${locale}/basvurularim`}
        className="inline-flex items-center gap-1.5 text-sm text-warm-500 hover:text-warm-800 transition-colors"
      >
        <ChevronLeft className="w-4 h-4" aria-hidden="true" />
        Başvurularım
      </Link>

      <header className="page-header">
        <h1>{t("title")}</h1>
        <code className="text-xs text-warm-400 font-mono mt-1 block">
          #{dsr.id}
        </code>
      </header>

      {/* Request details */}
      <section className="card space-y-4">
        <dl className="grid grid-cols-2 gap-4 text-sm">
          <div>
            <dt className="text-xs font-medium text-warm-400 uppercase tracking-wide mb-0.5">
              {t("requestType")}
            </dt>
            <dd className="text-warm-900 font-medium">
              {TYPE_LABELS[dsr.request_type] ?? dsr.request_type}
            </dd>
          </div>
          <div>
            <dt className="text-xs font-medium text-warm-400 uppercase tracking-wide mb-0.5">
              Oluşturulma
            </dt>
            <dd className="text-warm-700">{formatDate(dsr.created_at, locale)}</dd>
          </div>
          {dsr.scope && (
            <div className="col-span-2">
              <dt className="text-xs font-medium text-warm-400 uppercase tracking-wide mb-0.5">
                {t("scope")}
              </dt>
              <dd className="text-warm-700">{dsr.scope}</dd>
            </div>
          )}
          {dsr.justification && (
            <div className="col-span-2">
              <dt className="text-xs font-medium text-warm-400 uppercase tracking-wide mb-0.5">
                {t("justification")}
              </dt>
              <dd className="text-warm-700">{dsr.justification}</dd>
            </div>
          )}
        </dl>
      </section>

      {/* SLA timeline */}
      <section className="card">
        <RequestTimeline
          createdAt={dsr.created_at}
          slaDeadline={dsr.sla_deadline}
          state={dsr.state}
          locale={locale}
        />
      </section>

      {/* Response */}
      <section className="card">
        <ResponseView dsr={dsr} />
      </section>
    </div>
  );
}
