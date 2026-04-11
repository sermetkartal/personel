import { redirect } from "next/navigation";
import { getTranslations, getLocale } from "next-intl/server";
import { Shield } from "lucide-react";
import { getSession } from "@/lib/auth/session";

export async function generateMetadata(): Promise<{ title: string }> {
  const t = await getTranslations("giris");
  return { title: t("title") };
}

export default async function GirisPage(): Promise<JSX.Element> {
  const session = await getSession();
  const locale = await getLocale();

  // Already logged in — redirect to home
  if (session) redirect(`/${locale}`);

  const t = await getTranslations("giris");
  const companyName = process.env["NEXT_PUBLIC_COMPANY_NAME"] ?? "Şirketiniz";

  return (
    <div className="min-h-screen bg-warm-50 flex items-center justify-center px-4">
      <div className="w-full max-w-sm animate-slide-up">
        <div className="card space-y-6 text-center">
          {/* Logo */}
          <div className="flex flex-col items-center gap-3 pb-2">
            <div className="w-14 h-14 rounded-2xl bg-portal-100 flex items-center justify-center">
              <Shield className="w-7 h-7 text-portal-600" aria-hidden="true" />
            </div>
            <div>
              <h1 className="text-lg font-semibold text-warm-900">
                Personel Şeffaflık Portalı
              </h1>
              <p className="text-sm text-warm-500 mt-0.5">{companyName}</p>
            </div>
          </div>

          {/* Subtitle */}
          <p className="text-sm text-warm-600 leading-relaxed">{t("subtitle")}</p>

          {/* Login CTA */}
          <a
            href="/api/auth/login"
            className="w-full inline-flex flex-col items-center justify-center gap-1 bg-portal-600 hover:bg-portal-700 text-white font-medium py-3.5 px-6 rounded-xl transition-colors duration-150 no-underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-portal-600 focus-visible:ring-offset-2"
          >
            <span>{t("loginButton")}</span>
            <span className="text-xs text-portal-200">{t("loginButtonSub")}</span>
          </a>

          {/* Info */}
          <p className="text-xs text-warm-400 leading-relaxed">{t("info")}</p>
        </div>
      </div>
    </div>
  );
}
