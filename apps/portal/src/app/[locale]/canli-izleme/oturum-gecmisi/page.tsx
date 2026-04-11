import { redirect } from "next/navigation";
import { getTranslations, getLocale } from "next-intl/server";
import Link from "next/link";
import { ChevronLeft } from "lucide-react";
import { getSession } from "@/lib/auth/session";
import { getMyLiveViewHistory } from "@/lib/api/live-view";
import { SessionHistoryList } from "@/components/live-view/session-history-list";

export async function generateMetadata(): Promise<{ title: string }> {
  const t = await getTranslations("oturumGecmisi");
  return { title: t("title") };
}

/**
 * My live view session history page.
 * Default visibility: ON — employee always sees their own monitoring history.
 * (Changed per architect's revision round; can only be restricted by DPO with
 * written justification and audit record.)
 */
export default async function OturumGecmisiPage(): Promise<JSX.Element> {
  const session = await getSession();
  const locale = await getLocale();

  if (!session) redirect(`/${locale}/giris`);

  const t = await getTranslations("oturumGecmisi");

  let historyData = null;
  let loadError = false;

  try {
    historyData = await getMyLiveViewHistory(session.accessToken);
  } catch {
    loadError = true;
  }

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Back link */}
      <Link
        href={`/${locale}/canli-izleme`}
        className="inline-flex items-center gap-1.5 text-sm text-warm-500 hover:text-warm-800 transition-colors"
      >
        <ChevronLeft className="w-4 h-4" aria-hidden="true" />
        Canlı İzleme
      </Link>

      <header className="page-header">
        <h1>{t("title")}</h1>
        <p className="text-warm-600">{t("subtitle")}</p>
      </header>

      {loadError ? (
        <div className="card text-center py-8">
          <p className="text-sm text-warm-500">
            Oturum geçmişi şu an yüklenemiyor. Lütfen daha sonra tekrar deneyin.
          </p>
        </div>
      ) : historyData ? (
        <SessionHistoryList data={historyData} />
      ) : null}
    </div>
  );
}
