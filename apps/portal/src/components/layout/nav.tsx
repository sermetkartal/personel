"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useLocale, useTranslations } from "next-intl";
import {
  Home,
  FileText,
  Database,
  EyeOff,
  Monitor,
  Scale,
  InboxIcon,
  KeyRound,
  Phone,
} from "lucide-react";
import { cn } from "@/lib/utils";

type NavLabelKey =
  | "home"
  | "aydinlatma"
  | "verilerim"
  | "nelerIzlenmiyor"
  | "canliIzleme"
  | "oturumGecmisi"
  | "haklar"
  | "yeniBasvuru"
  | "basvurularim"
  | "dlpDurumu"
  | "iletisim"
  | "profil"
  | "cikis";

interface NavItem {
  href: string;
  labelKey: NavLabelKey;
  icon: React.ComponentType<{ className?: string }>;
}

export function Nav(): JSX.Element {
  const t = useTranslations<"nav">("nav");
  const locale = useLocale();
  const pathname = usePathname();

  const navItems: NavItem[] = [
    { href: `/${locale}`, labelKey: "home", icon: Home },
    { href: `/${locale}/aydinlatma`, labelKey: "aydinlatma", icon: FileText },
    { href: `/${locale}/verilerim`, labelKey: "verilerim", icon: Database },
    { href: `/${locale}/neler-izlenmiyor`, labelKey: "nelerIzlenmiyor", icon: EyeOff },
    { href: `/${locale}/canli-izleme`, labelKey: "canliIzleme", icon: Monitor },
    { href: `/${locale}/haklar`, labelKey: "haklar", icon: Scale },
    { href: `/${locale}/basvurularim`, labelKey: "basvurularim", icon: InboxIcon },
    { href: `/${locale}/dlp-durumu`, labelKey: "dlpDurumu", icon: KeyRound },
    { href: `/${locale}/iletisim`, labelKey: "iletisim", icon: Phone },
  ];

  return (
    <nav aria-label="Ana navigasyon">
      <ul className="space-y-0.5" role="list">
        {navItems.map((item) => {
          const isActive =
            item.href === `/${locale}`
              ? pathname === `/${locale}` || pathname === `/${locale}/`
              : pathname.startsWith(item.href);

          const Icon = item.icon;

          return (
            <li key={item.href}>
              <Link
                href={item.href}
                aria-current={isActive ? "page" : undefined}
                className={cn(
                  "flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors duration-100",
                  isActive
                    ? "bg-portal-100 text-portal-700 font-medium"
                    : "text-warm-600 hover:bg-warm-100 hover:text-warm-900"
                )}
              >
                <Icon
                  className={cn(
                    "w-4 h-4 flex-shrink-0",
                    isActive ? "text-portal-600" : "text-warm-400"
                  )}
                  aria-hidden="true"
                />
                <span>{t(item.labelKey)}</span>
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
