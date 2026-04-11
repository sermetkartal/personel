"use client";

import Link from "next/link";
import { useLocale, useTranslations } from "next-intl";
import { Lock, ShieldAlert } from "lucide-react";
import { cn } from "@/lib/utils";
import { formatDate } from "@/lib/utils";
import type { DLPStateResponse } from "@/lib/api/types";

interface DlpBannerProps {
  /**
   * DLP state — passed as a prop from the server layout.
   * Falls back to "disabled" if not provided, which is the correct default
   * per ADR 0013 (DLP disabled by default in Phase 1).
   */
  state?: DLPStateResponse;
}

/**
 * DLP state banner shown at the top of every authenticated page.
 *
 * Design decisions:
 * OFF state (default): warm-gray neutral, very calm. Informational only.
 *   → "Klavye içeriği kaydediliyor ancak hiçbir sistem ya da kişi tarafından okunmuyor."
 * ON state: amber tone, factual date. Still calm — no alarming red.
 *   → "DLP aktif: [tarih]'den itibaren otomatik tarama yapılıyor."
 *
 * ADR 0013: the vast majority of deployments will show the OFF state.
 */
export function DlpBanner({ state }: DlpBannerProps): JSX.Element {
  const t = useTranslations("dlpBanner");
  const locale = useLocale() as "tr" | "en";

  // Default to disabled — ADR 0013 default
  const isEnabled = state?.status === "enabled";
  const dlpPage = `/${locale}/dlp-durumu`;

  if (!isEnabled) {
    return (
      <div
        role="status"
        aria-label="Klavye gizlilik durumu"
        className={cn(
          "w-full px-4 py-2.5 shadow-banner",
          "bg-warm-100 border-b border-warm-200"
        )}
      >
        <div className="max-w-7xl mx-auto flex items-center gap-2.5 text-sm text-warm-600">
          <Lock
            className="w-4 h-4 text-warm-400 flex-shrink-0"
            aria-hidden="true"
          />
          <span>{t("offMessage")}</span>
          <Link
            href={dlpPage}
            className="ml-auto flex-shrink-0 text-portal-600 hover:text-portal-800 underline-offset-2 hover:underline text-xs font-medium"
          >
            {t("offLearnMore")}
          </Link>
        </div>
      </div>
    );
  }

  const enabledAt = state?.enabled_at
    ? formatDate(state.enabled_at, locale)
    : "";

  return (
    <div
      role="status"
      aria-label="DLP aktif durumu"
      className={cn(
        "w-full px-4 py-2.5 shadow-banner",
        "bg-dlp-100 border-b border-dlp-200"
      )}
    >
      <div className="max-w-7xl mx-auto flex items-center gap-2.5 text-sm text-dlp-800">
        <ShieldAlert
          className="w-4 h-4 text-dlp-600 flex-shrink-0"
          aria-hidden="true"
        />
        <span>{t("onMessage", { date: enabledAt })}</span>
        <Link
          href={dlpPage}
          className="ml-auto flex-shrink-0 text-dlp-700 hover:text-dlp-900 underline-offset-2 hover:underline text-xs font-medium"
        >
          {t("onLearnMore")}
        </Link>
      </div>
    </div>
  );
}
