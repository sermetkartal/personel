"use client";

/**
 * DLP State Indicator — always-visible header badge.
 * Shows "DLP: Kapalı" (red) or "DLP: Aktif" (green).
 *
 * Per ADR 0013: DLP is OFF by default. This badge must be visible at all times
 * so that operators are always aware of the DLP state.
 */

import { useTranslations } from "next-intl";
import { useDLPState } from "@/lib/hooks/use-dlp-state";
import { cn } from "@/lib/utils";
import Link from "next/link";
import { useLocale } from "next-intl";
import { Shield, ShieldOff } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

interface DLPStateIndicatorProps {
  className?: string;
}

export function DLPStateIndicator({ className }: DLPStateIndicatorProps): JSX.Element {
  const t = useTranslations("dlp.statusBadge");
  const { data, isLoading } = useDLPState();
  const locale = useLocale();

  const isActive = data?.state === "active";

  if (isLoading) {
    return (
      <div
        className={cn(
          "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium",
          "border-border bg-muted text-muted-foreground",
          className,
        )}
        aria-label="DLP durumu yükleniyor"
      >
        <span className="h-2 w-2 rounded-full bg-muted-foreground/50 animate-pulse" />
        DLP: ...
      </div>
    );
  }

  return (
    <TooltipProvider delayDuration={300}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Link
            href={`/${locale}/settings/dlp`}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-semibold transition-colors",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              isActive
                ? "border-green-200 bg-green-50 text-green-700 hover:bg-green-100 dark:border-green-800/50 dark:bg-green-900/20 dark:text-green-400"
                : "border-red-200 bg-red-50 text-red-700 hover:bg-red-100 dark:border-red-800/50 dark:bg-red-900/20 dark:text-red-400",
              className,
            )}
            aria-label={`DLP durumu: ${isActive ? "aktif" : "kapalı"}. DLP ayarlarına git.`}
          >
            {isActive ? (
              <Shield className="h-3 w-3" aria-hidden="true" />
            ) : (
              <ShieldOff className="h-3 w-3" aria-hidden="true" />
            )}
            {isActive ? t("active") : t("inactive")}
          </Link>
        </TooltipTrigger>
        <TooltipContent side="bottom">
          {isActive
            ? "DLP hizmeti aktif. Klavye içeriği işleniyor."
            : "DLP hizmeti kapalı (varsayılan). Tıklayın: detaylar için."}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
