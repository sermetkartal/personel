import { redirect } from "next/navigation";
import { getTranslations, getLocale } from "next-intl/server";
import {
  Smartphone,
  Home,
  Globe,
  Coffee,
  Mail,
  Clock,
  Lock,
  Heart,
  BarChart2,
  Fingerprint,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { getSession } from "@/lib/auth/session";
import { WhatNotMonitoredCard } from "@/components/data-cards/what-not-monitored-card";

interface NotMonitoredItem {
  key: string;
  icon: LucideIcon;
}

const ITEMS: NotMonitoredItem[] = [
  { key: "personalPhone", icon: Smartphone },
  { key: "homeComputer", icon: Home },
  { key: "personalBrowserSessions", icon: Globe },
  { key: "breakTimes", icon: Coffee },
  { key: "personalAccounts", icon: Mail },
  { key: "outsideWorkHours", icon: Clock },
  { key: "keyboardContentDefault", icon: Lock },
  { key: "privateHealthInfo", icon: Heart },
  { key: "performanceScore", icon: BarChart2 },
  { key: "biometrics", icon: Fingerprint },
];

export async function generateMetadata(): Promise<{ title: string }> {
  const t = await getTranslations("nelerIzlenmiyor");
  return { title: t("title") };
}

/**
 * "Neler İzlenmiyor?" — Trust-building page.
 *
 * This is one of the most important pages in the portal from a trust perspective.
 * It explicitly tells employees what the system cannot and does not see.
 * It uses the trust-green palette (vs the portal-blue of most other pages).
 *
 * Copy is carefully worded in plain Turkish with specific, concrete examples.
 */
export default async function NelerIzlenmiyorPage(): Promise<JSX.Element> {
  const session = await getSession();
  const locale = await getLocale();

  if (!session) redirect(`/${locale}/giris`);

  const t = await getTranslations("nelerIzlenmiyor");
  const tItems = await getTranslations("nelerIzlenmiyor.items");

  return (
    <div className="space-y-8 animate-fade-in">
      <header className="page-header">
        <div className="flex items-center gap-3 mb-2">
          <div
            className="w-10 h-10 rounded-xl bg-trust-100 flex items-center justify-center"
            aria-hidden="true"
          >
            <span className="text-trust-600 font-bold text-lg" aria-hidden="true">✓</span>
          </div>
          <h1>{t("title")}</h1>
        </div>
        <p className="text-warm-600 font-medium">{t("subtitle")}</p>
        <p className="mt-2 text-warm-500 text-sm leading-relaxed max-w-2xl">
          {t("intro")}
        </p>
      </header>

      {/* Items grid */}
      <section aria-label="İzlenmeyen konular">
        <div className="grid gap-4 sm:grid-cols-2">
          {ITEMS.map((item) => (
            <WhatNotMonitoredCard
              key={item.key}
              icon={item.icon}
              title={tItems(`${item.key}.title`)}
              description={tItems(`${item.key}.description`)}
            />
          ))}
        </div>
      </section>

      {/* Footer note */}
      <footer className="rounded-xl bg-warm-100 border border-warm-200 px-5 py-4 text-sm text-warm-600 leading-relaxed">
        <p>{t("footer")}</p>
      </footer>
    </div>
  );
}
