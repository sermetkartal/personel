import Link from "next/link";
import { useTranslations, useLocale } from "next-intl";
import type { LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

interface WhatMonitoredCardProps {
  icon: LucideIcon;
  categoryKey: string;
  name: string;
  description: string;
  legalBasis: string;
  retentionPeriod: string;
  isSensitive?: boolean;
  /** Event count in the last 30 days; undefined means the summary is unavailable */
  recentCount?: number | undefined;
}

/**
 * One card per event category on the "Verilerim" page.
 * Shows icon, name, plain-Turkish description, legal basis, retention period,
 * and a quick-access link to submit a DSR for this specific category.
 */
export function WhatMonitoredCard({
  icon: Icon,
  categoryKey: _categoryKey,
  name,
  description,
  legalBasis,
  retentionPeriod,
  isSensitive = false,
  recentCount,
}: WhatMonitoredCardProps): JSX.Element {
  const t = useTranslations("verilerim");
  const tCommon = useTranslations("common");
  const locale = useLocale();

  return (
    <article
      className={cn(
        "card-hover relative overflow-hidden",
        isSensitive && "border-l-4 border-l-dlp-400"
      )}
      aria-label={name}
    >
      {/* Sensitive indicator */}
      {isSensitive && (
        <span className="badge-warning absolute top-4 right-4 text-xs">
          Hassas
        </span>
      )}

      <div className="flex items-start gap-4">
        <div
          className={cn(
            "w-10 h-10 rounded-xl flex items-center justify-center flex-shrink-0",
            isSensitive ? "bg-dlp-100" : "bg-portal-100"
          )}
          aria-hidden="true"
        >
          <Icon
            className={cn(
              "w-5 h-5",
              isSensitive ? "text-dlp-600" : "text-portal-600"
            )}
          />
        </div>

        <div className="flex-1 min-w-0">
          <h3 className="font-medium text-warm-900">{name}</h3>
          <p className="mt-1.5 text-sm text-warm-600 leading-relaxed">
            {description}
          </p>
        </div>
      </div>

      <dl className="mt-4 pt-4 border-t border-warm-100 grid grid-cols-2 gap-3">
        <div>
          <dt className="text-xs font-medium text-warm-400 uppercase tracking-wide">
            {tCommon("legalBasis")}
          </dt>
          <dd className="mt-0.5 text-xs text-warm-600">{legalBasis}</dd>
        </div>
        <div>
          <dt className="text-xs font-medium text-warm-400 uppercase tracking-wide">
            {t("retentionHeader")}
          </dt>
          <dd className="mt-0.5 text-xs text-warm-600">{retentionPeriod}</dd>
        </div>
      </dl>

      {recentCount !== undefined && (
        <p className="mt-3 text-xs text-portal-700 bg-portal-50 rounded-lg px-3 py-2">
          {recentCount === 0
            ? t("noDataYet")
            : t("recentCount", { count: recentCount.toLocaleString("tr-TR") })}
        </p>
      )}

      <div className="mt-4 flex gap-3">
        <Link
          href={`/${locale}/haklar/yeni-basvuru?type=access`}
          className="text-xs text-portal-600 hover:text-portal-800 underline-offset-2 hover:underline"
        >
          {t("requestThisData")}
        </Link>
        <span className="text-warm-300 text-xs" aria-hidden="true">·</span>
        <Link
          href={`/${locale}/haklar/yeni-basvuru?type=erase`}
          className="text-xs text-warm-500 hover:text-warm-800 underline-offset-2 hover:underline"
        >
          {t("requestDataDeletion")}
        </Link>
      </div>
    </article>
  );
}
