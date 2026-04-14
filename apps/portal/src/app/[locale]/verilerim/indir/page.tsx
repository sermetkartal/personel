import { redirect } from "next/navigation";
import { getTranslations, getLocale } from "next-intl/server";
import Link from "next/link";
import { ChevronLeft } from "lucide-react";
import { getSession } from "@/lib/auth/session";
import { DataDownloadFlow } from "@/components/verilerim/data-download-flow";

export async function generateMetadata(): Promise<{ title: string }> {
  const t = await getTranslations("dataDownload");
  return { title: t("title") };
}

/**
 * Self-service data download flow — KVKK m.11/b ("işlenen kişisel verileri
 * talep etme"). Creates an `access` DSR on behalf of the employee, then
 * polls the DSR status until fulfilled.
 *
 * Under KVKK this does NOT bypass DPO review — it just pre-fills the
 * request and gives the employee a one-click shortcut. The DPO still has
 * 30 days to review + respond; the polling UI is honest about that.
 */
export default async function DataDownloadPage(): Promise<JSX.Element> {
  const session = await getSession();
  const locale = await getLocale();

  if (!session) redirect(`/${locale}/giris`);

  const t = await getTranslations("dataDownload");

  return (
    <div className="space-y-6 animate-fade-in max-w-2xl">
      <Link
        href={`/${locale}/verilerim`}
        className="inline-flex items-center gap-1.5 text-sm text-warm-500 hover:text-warm-800 transition-colors"
      >
        <ChevronLeft className="w-4 h-4" aria-hidden="true" />
        {t("back")}
      </Link>

      <header className="page-header">
        <h1>{t("title")}</h1>
        <p className="text-warm-600">{t("subtitle")}</p>
      </header>

      <DataDownloadFlow accessToken={session.accessToken} />
    </div>
  );
}
