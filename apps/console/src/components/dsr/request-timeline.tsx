"use client";

/**
 * DSR SLA Timeline Visualization.
 * Displays the 30-day SLA as a horizontal progress bar with color state:
 * - T+0 to T+19: green (safe)
 * - T+20 to T+27: amber (warning)
 * - T+28 to T+29: orange (critical)
 * - T+30+: red (breach)
 */

import { useTranslations } from "next-intl";
import { CheckCircle2, AlertCircle, XCircle, Clock } from "lucide-react";
import { cn, slaStatusFromDays } from "@/lib/utils";
import type { DSRRequest } from "@/lib/api/types";
import { dsrDaysElapsed, dsrDaysRemaining } from "@/lib/api/dsr";

interface RequestTimelineProps {
  request: DSRRequest;
  className?: string;
}

interface Milestone {
  dayLabel: string;
  day: number;
  labelKey: string;
  icon: React.ElementType;
  activeColor: string;
}

const MILESTONES: Milestone[] = [
  {
    dayLabel: "0",
    day: 0,
    labelKey: "submitted",
    icon: CheckCircle2,
    activeColor: "text-green-500",
  },
  {
    dayLabel: "20",
    day: 20,
    labelKey: "atRisk",
    icon: AlertCircle,
    activeColor: "text-amber-500",
  },
  {
    dayLabel: "28",
    day: 28,
    labelKey: "critical",
    icon: AlertCircle,
    activeColor: "text-orange-500",
  },
  {
    dayLabel: "30",
    day: 30,
    labelKey: "deadline",
    icon: XCircle,
    activeColor: "text-red-500",
  },
];

export function RequestTimeline({ request, className }: RequestTimelineProps): JSX.Element {
  const t = useTranslations("dsr.slaTimeline");

  const daysElapsed = dsrDaysElapsed(request);
  const daysRemaining = dsrDaysRemaining(request);
  const slaStatus = slaStatusFromDays(daysElapsed);

  // Progress capped at 100%
  const progressPercent = Math.min((daysElapsed / 30) * 100, 100);

  const progressColorClass = {
    safe: "bg-green-500",
    warning: "bg-amber-500",
    critical: "bg-orange-500",
    breach: "bg-red-500",
  }[slaStatus];

  const trackColorClass = {
    safe: "bg-green-100 dark:bg-green-900/20",
    warning: "bg-amber-100 dark:bg-amber-900/20",
    critical: "bg-orange-100 dark:bg-orange-900/20",
    breach: "bg-red-100 dark:bg-red-900/20",
  }[slaStatus];

  return (
    <div className={cn("space-y-3", className)} role="group" aria-label="SLA zaman çizelgesi">
      {/* Progress bar */}
      <div className="relative">
        <div
          className={cn("h-2.5 w-full overflow-hidden rounded-full", trackColorClass)}
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={30}
          aria-valuenow={daysElapsed}
          aria-label={`${daysElapsed} / 30 gün geçti`}
        >
          <div
            className={cn(
              "h-full rounded-full transition-all duration-500",
              progressColorClass,
            )}
            style={{ width: `${progressPercent}%` }}
          />
        </div>

        {/* Milestone markers */}
        <div className="absolute top-0 flex w-full justify-between" aria-hidden="true">
          {[20, 28, 30].map((day) => (
            <div
              key={day}
              className="absolute top-0 h-2.5 w-0.5 bg-background/60"
              style={{ left: `${(day / 30) * 100}%` }}
            />
          ))}
        </div>
      </div>

      {/* Milestone labels */}
      <div className="flex justify-between text-xs">
        {MILESTONES.map((milestone) => {
          const isReached = daysElapsed >= milestone.day;
          const isCurrent =
            milestone.day === 0
              ? daysElapsed < 20
              : milestone.day === 20
              ? daysElapsed >= 20 && daysElapsed < 28
              : milestone.day === 28
              ? daysElapsed >= 28 && daysElapsed < 30
              : daysElapsed >= 30;

          return (
            <div
              key={milestone.day}
              className={cn(
                "flex flex-col items-center gap-1",
                isReached ? milestone.activeColor : "text-muted-foreground/50",
                isCurrent && "font-semibold",
              )}
            >
              <milestone.icon className="h-3 w-3" aria-hidden="true" />
              <span>{t(milestone.labelKey as Parameters<typeof t>[0])}</span>
              <span className="text-muted-foreground">Gün {milestone.dayLabel}</span>
            </div>
          );
        })}
      </div>

      {/* Status message */}
      <div
        className={cn(
          "flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium",
          {
            "bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-400":
              slaStatus === "safe",
            "bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-400":
              slaStatus === "warning",
            "bg-orange-50 text-orange-700 dark:bg-orange-900/20 dark:text-orange-400":
              slaStatus === "critical",
            "bg-red-50 text-red-600 dark:bg-red-900/20 dark:text-red-400":
              slaStatus === "breach",
          },
        )}
        role="status"
        aria-live="polite"
      >
        <Clock className="h-4 w-4 shrink-0" aria-hidden="true" />
        {slaStatus === "breach" ? (
          <span>
            {t("breach", { count: Math.abs(daysRemaining) })}
          </span>
        ) : daysRemaining >= 0 ? (
          <span>
            {t("daysElapsed", { count: daysElapsed })} —{" "}
            {t("daysRemaining", { count: daysRemaining })}
          </span>
        ) : (
          <span>{t("breach", { count: Math.abs(daysRemaining) })}</span>
        )}
      </div>
    </div>
  );
}
