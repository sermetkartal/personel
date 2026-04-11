import Link from "next/link";
import { useLocale, useTranslations } from "next-intl";
import { User, Globe } from "lucide-react";
import type { SessionPayload } from "@/lib/auth/session";

interface HeaderProps {
  session: SessionPayload;
}

export function Header({ session }: HeaderProps): JSX.Element {
  const t = useTranslations("nav");
  const locale = useLocale();
  const altLocale = locale === "tr" ? "en" : "tr";
  const companyName = process.env["NEXT_PUBLIC_COMPANY_NAME"] ?? "Şirketiniz";

  return (
    <header className="sticky top-0 z-40 bg-white border-b border-warm-200 shadow-banner">
      <div className="flex items-center justify-between px-6 h-14">
        {/* Logo + company name */}
        <div className="flex items-center gap-3">
          <Link
            href={`/${locale}`}
            className="flex items-center gap-2.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-portal-600 rounded-lg"
            aria-label="Ana sayfaya dön"
          >
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src="/logo.svg"
              alt="Personel"
              width={28}
              height={28}
              className="flex-shrink-0"
            />
            <span className="font-semibold text-portal-700 text-sm hidden sm:block">
              Şeffaflık Portalı
            </span>
          </Link>
          <span className="text-warm-300 text-sm hidden md:block" aria-hidden="true">
            |
          </span>
          <span className="text-warm-500 text-sm hidden md:block">{companyName}</span>
        </div>

        {/* Right side: employee name, language switcher */}
        <div className="flex items-center gap-4">
          {/* Language switch */}
          <Link
            href={`/${altLocale}`}
            className="flex items-center gap-1.5 text-sm text-warm-500 hover:text-warm-800 transition-colors"
            aria-label={altLocale === "en" ? "Switch to English" : "Türkçe'ye geç"}
          >
            <Globe className="w-4 h-4" aria-hidden="true" />
            <span className="hidden sm:block">
              {altLocale === "en" ? "EN" : "TR"}
            </span>
          </Link>

          {/* Employee identity */}
          <Link
            href={`/${locale}/profil`}
            className="flex items-center gap-2 text-sm text-warm-600 hover:text-warm-900 transition-colors rounded-lg px-2 py-1"
            aria-label="Profilim"
          >
            <span
              className="w-7 h-7 rounded-full bg-portal-100 text-portal-700 flex items-center justify-center font-medium text-xs flex-shrink-0"
              aria-hidden="true"
            >
              {session.name.charAt(0).toUpperCase()}
            </span>
            <span className="hidden sm:block max-w-[180px] truncate">
              {session.name}
            </span>
            <User className="w-4 h-4 sm:hidden text-warm-400" aria-hidden="true" />
          </Link>
        </div>
      </div>
    </header>
  );
}
