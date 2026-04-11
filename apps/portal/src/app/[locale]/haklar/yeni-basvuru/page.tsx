import { redirect } from "next/navigation";
import { getTranslations, getLocale } from "next-intl/server";
import Link from "next/link";
import { ChevronLeft } from "lucide-react";
import { getSession } from "@/lib/auth/session";
import { NewRequestForm } from "@/components/dsr/new-request-form";

export async function generateMetadata(): Promise<{ title: string }> {
  const t = await getTranslations("yeniBasvuru");
  return { title: t("title") };
}

export default async function YeniBasvuruPage(): Promise<JSX.Element> {
  const session = await getSession();
  const locale = await getLocale();

  if (!session) redirect(`/${locale}/giris`);

  const t = await getTranslations("yeniBasvuru");

  return (
    <div className="space-y-6 animate-fade-in max-w-2xl">
      {/* Back link */}
      <Link
        href={`/${locale}/haklar`}
        className="inline-flex items-center gap-1.5 text-sm text-warm-500 hover:text-warm-800 transition-colors"
      >
        <ChevronLeft className="w-4 h-4" aria-hidden="true" />
        KVKK Haklarım
      </Link>

      <header className="page-header">
        <h1>{t("title")}</h1>
        <p className="text-warm-600">{t("subtitle")}</p>
      </header>

      <NewRequestForm accessToken={session.accessToken} />
    </div>
  );
}
