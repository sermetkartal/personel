import { redirect } from "next/navigation";
import { getTranslations, getLocale } from "next-intl/server";
import {
  User,
  AppWindow,
  Camera,
  FolderOpen,
  Clipboard,
  Keyboard,
  Printer,
  Usb,
  Network,
  Monitor,
  ShieldCheck,
  Calendar,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { getSession } from "@/lib/auth/session";
import { getMyData } from "@/lib/api/transparency";
import { WhatMonitoredCard } from "@/components/data-cards/what-monitored-card";
import { RetentionCard } from "@/components/data-cards/retention-card";
import { formatDate } from "@/lib/utils";

interface CategoryConfig {
  key: string;
  icon: LucideIcon;
  legalBasis: string;
  retentionPeriod: string;
  isSensitive?: boolean;
}

const CATEGORY_CONFIGS: CategoryConfig[] = [
  {
    key: "identity",
    icon: User,
    legalBasis: "m.5/2-c, m.5/2-f",
    retentionPeriod: "1 yıl",
  },
  {
    key: "process",
    icon: AppWindow,
    legalBasis: "m.5/2-c, m.5/2-f",
    retentionPeriod: "90 gün",
  },
  {
    key: "screenshot",
    icon: Camera,
    legalBasis: "m.5/2-f",
    retentionPeriod: "30 gün",
    isSensitive: true,
  },
  {
    key: "file",
    icon: FolderOpen,
    legalBasis: "m.5/2-f",
    retentionPeriod: "180 gün",
  },
  {
    key: "clipboard",
    icon: Clipboard,
    legalBasis: "m.5/2-f",
    retentionPeriod: "30 gün (içerik), 90 gün (meta veri)",
    isSensitive: true,
  },
  {
    key: "keystroke",
    icon: Keyboard,
    legalBasis: "m.5/2-c, m.5/2-f",
    retentionPeriod: "14 gün (içerik), 90 gün (istatistik)",
    isSensitive: true,
  },
  {
    key: "print",
    icon: Printer,
    legalBasis: "m.5/2-f",
    retentionPeriod: "180 gün",
  },
  {
    key: "usb",
    icon: Usb,
    legalBasis: "m.5/2-f",
    retentionPeriod: "365 gün",
  },
  {
    key: "network",
    icon: Network,
    legalBasis: "m.5/2-f, m.5/2-ç",
    retentionPeriod: "30–60 gün",
  },
  {
    key: "liveView",
    icon: Monitor,
    legalBasis: "m.5/2-f, m.12",
    retentionPeriod: "5 yıl (denetim kaydı)",
  },
  {
    key: "policy",
    icon: ShieldCheck,
    legalBasis: "m.5/2-f",
    retentionPeriod: "365 gün – 2 yıl",
  },
];

export async function generateMetadata(): Promise<{ title: string }> {
  const t = await getTranslations("verilerim");
  return { title: t("title") };
}

export default async function VerilerimPage(): Promise<JSX.Element> {
  const session = await getSession();
  const locale = await getLocale() as "tr" | "en";

  if (!session) redirect(`/${locale}/giris`);

  const t = await getTranslations("verilerim");
  const tCats = await getTranslations("verilerim.categories");

  // Load API data — non-critical, show static categories if it fails
  let collectedSince: string | null = null;
  try {
    const myData = await getMyData(session.accessToken);
    collectedSince = myData.collected_since;
  } catch {
    // Show static layout even without API data
  }

  return (
    <div className="space-y-8 animate-fade-in">
      <header className="page-header">
        <h1>{t("title")}</h1>
        <p className="text-warm-600">{t("subtitle")}</p>
        {collectedSince && (
          <div className="mt-3 flex items-center gap-2 text-sm text-warm-500">
            <Calendar className="w-4 h-4" aria-hidden="true" />
            <span>
              {t("collectedSince")}: {formatDate(collectedSince, locale)}
            </span>
          </div>
        )}
      </header>

      {/* Retention note */}
      <div className="rounded-xl bg-trust-50 border border-trust-200 px-4 py-3 text-sm text-trust-700">
        {t("retentionNote")}
      </div>

      {/* Categories grid */}
      <section aria-labelledby="categories-heading">
        <h2
          id="categories-heading"
          className="text-base font-semibold text-warm-700 mb-4"
        >
          {t("categoriesTitle")}
        </h2>
        <p className="text-sm text-warm-500 mb-5">{t("categoriesSubtitle")}</p>

        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {CATEGORY_CONFIGS.map((config) => (
            <WhatMonitoredCard
              key={config.key}
              icon={config.icon}
              categoryKey={config.key}
              name={tCats(`${config.key}.name`)}
              description={tCats(`${config.key}.description`)}
              legalBasis={config.legalBasis}
              retentionPeriod={config.retentionPeriod}
              isSensitive={config.isSensitive ?? false}
            />
          ))}
        </div>
      </section>

      {/* Retention summary sidebar */}
      <section
        aria-labelledby="retention-summary-heading"
        className="card max-w-xl"
      >
        <h2
          id="retention-summary-heading"
          className="text-sm font-semibold text-warm-700 mb-4"
        >
          Saklama Süresi Özeti
        </h2>
        <RetentionCard category="Ekran görüntüleri" period="30 gün" note="Hassas uygulamalar hariç" />
        <RetentionCard category="Ekran video klipleri" period="14 gün" />
        <RetentionCard category="Klavye içeriği (şifreli)" period="14 gün" note="Yönetici erişimi yok" />
        <RetentionCard category="Klavye istatistikleri" period="90 gün" />
        <RetentionCard category="Süreç/pencere olayları" period="90 gün" />
        <RetentionCard category="Dosya sistemi olayları" period="180 gün" />
        <RetentionCard category="USB olayları" period="365 gün" />
        <RetentionCard category="Canlı izleme denetim kaydı" period="5 yıl" note="Değiştirilemez" />
      </section>
    </div>
  );
}
