import { redirect } from "next/navigation";
import { getTranslations, getLocale } from "next-intl/server";
import Link from "next/link";
import { User, Globe, LogOut } from "lucide-react";
import { getSession } from "@/lib/auth/session";

export async function generateMetadata(): Promise<{ title: string }> {
  const t = await getTranslations("profil");
  return { title: t("title") };
}

export default async function ProfilPage(): Promise<JSX.Element> {
  const session = await getSession();
  const locale = await getLocale();

  if (!session) redirect(`/${locale}/giris`);

  const t = await getTranslations("profil");

  return (
    <div className="space-y-6 animate-fade-in max-w-md">
      <header className="page-header">
        <div className="flex items-center gap-3">
          <div className="w-12 h-12 rounded-full bg-portal-100 flex items-center justify-center">
            <User className="w-6 h-6 text-portal-600" aria-hidden="true" />
          </div>
          <div>
            <h1 className="text-lg font-semibold text-warm-900">{session.name}</h1>
            <p className="text-sm text-warm-500">{session.email}</p>
          </div>
        </div>
      </header>

      <section className="card space-y-5">
        {/* Email */}
        <dl>
          <div>
            <dt className="text-xs font-medium text-warm-400 uppercase tracking-wide mb-1">
              {t("email")}
            </dt>
            <dd className="text-sm text-warm-800">{session.email}</dd>
          </div>
        </dl>

        <hr className="border-warm-100" />

        {/* Language */}
        <div>
          <p className="text-xs font-medium text-warm-400 uppercase tracking-wide mb-2">
            {t("language")}
          </p>
          <div className="flex gap-2">
            <Link
              href="/tr"
              className={`inline-flex items-center gap-1.5 px-3 py-2 rounded-xl text-sm border transition-colors ${
                locale === "tr"
                  ? "bg-portal-600 text-white border-portal-600"
                  : "bg-white text-warm-600 border-warm-200 hover:border-portal-300"
              }`}
            >
              <Globe className="w-3.5 h-3.5" aria-hidden="true" />
              {t("languageTr")}
            </Link>
            <Link
              href="/en"
              className={`inline-flex items-center gap-1.5 px-3 py-2 rounded-xl text-sm border transition-colors ${
                locale === "en"
                  ? "bg-portal-600 text-white border-portal-600"
                  : "bg-white text-warm-600 border-warm-200 hover:border-portal-300"
              }`}
            >
              <Globe className="w-3.5 h-3.5" aria-hidden="true" />
              {t("languageEn")}
            </Link>
          </div>
        </div>
      </section>

      {/* Logout */}
      <form action="/api/auth/logout" method="POST">
        <button
          type="submit"
          className="inline-flex items-center gap-2 text-sm text-warm-500 hover:text-red-600 transition-colors"
        >
          <LogOut className="w-4 h-4" aria-hidden="true" />
          {t("logout")}
        </button>
      </form>
    </div>
  );
}
