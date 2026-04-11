import { useTranslations } from "next-intl";
import { CheckCircle2, Clock, AlertTriangle, XCircle } from "lucide-react";
import { cn } from "@/lib/utils";
import { getDaysRemaining, getSLAProgress, formatDate } from "@/lib/utils";
import type { DSRState } from "@/lib/api/types";

interface RequestTimelineProps {
  createdAt: string;
  slaDeadline: string;
  state: DSRState;
  locale?: "tr" | "en";
}

/**
 * SLA visualization for a DSR request.
 * Shows a progress bar from T+0 (submission) to T+30 (KVKK legal deadline),
 * with the at-risk threshold at T+20.
 */
export function RequestTimeline({
  createdAt,
  slaDeadline,
  state,
  locale = "tr",
}: RequestTimelineProps): JSX.Element {
  const t = useTranslations("basvurularim");
  const daysRemaining = getDaysRemaining(slaDeadline);
  const progressPercent = getSLAProgress(createdAt, slaDeadline);

  const isClosed = state === "closed" || state === "rejected";
  const isOverdue = state === "overdue" || (daysRemaining < 0 && !isClosed);
  const isAtRisk = state === "at_risk" || (daysRemaining <= 10 && daysRemaining >= 0 && !isClosed);

  const fillColor = isClosed
    ? "bg-trust-500"
    : isOverdue
    ? "bg-red-500"
    : isAtRisk
    ? "bg-dlp-500"
    : "bg-portal-500";

  return (
    <div className="space-y-4">
      <h4 className="text-sm font-medium text-warm-700">
        {t("slaTimeline.title")}
      </h4>

      {/* Progress bar */}
      {!isClosed && (
        <div>
          <div className="sla-bar" role="progressbar" aria-valuenow={Math.round(progressPercent)} aria-valuemin={0} aria-valuemax={100}>
            <div
              className={cn("sla-bar-fill", fillColor)}
              style={{ width: `${progressPercent}%` }}
            />
          </div>
          {/* Milestone markers */}
          <div className="relative mt-1">
            <div
              className="absolute h-2 w-px bg-warm-300"
              style={{ left: "66.7%" }}
              aria-hidden="true"
              title="Gün 20 — Risk eşiği"
            />
          </div>
          <div className="flex justify-between text-xs text-warm-400 mt-2">
            <span>{t("slaTimeline.submitted")}</span>
            <span className="text-center" style={{ marginLeft: "58%" }}>
              Gün 20
            </span>
            <span>{t("slaTimeline.deadline")}</span>
          </div>
        </div>
      )}

      {/* Status items */}
      <ol className="space-y-2" aria-label="Başvuru süreci">
        <TimelineItem
          icon={<CheckCircle2 className="w-4 h-4 text-trust-500" />}
          label={t("slaTimeline.submitted")}
          date={formatDate(createdAt, locale)}
          done
        />
        {isClosed && (
          <TimelineItem
            icon={<CheckCircle2 className="w-4 h-4 text-trust-500" />}
            label={t("slaTimeline.closed")}
            done
          />
        )}
        {!isClosed && isOverdue && (
          <TimelineItem
            icon={<XCircle className="w-4 h-4 text-red-500" />}
            label="Yasal süre geçti"
            highlight="danger"
          />
        )}
        {!isClosed && !isOverdue && (
          <TimelineItem
            icon={
              isAtRisk ? (
                <AlertTriangle className="w-4 h-4 text-dlp-500" />
              ) : (
                <Clock className="w-4 h-4 text-warm-400" />
              )
            }
            label={t("slaTimeline.deadline")}
            date={formatDate(slaDeadline, locale)}
            highlight={isAtRisk ? "warning" : undefined}
            daysRemaining={daysRemaining}
          />
        )}
      </ol>
    </div>
  );
}

function TimelineItem({
  icon,
  label,
  date,
  done = false,
  highlight,
  daysRemaining,
}: {
  icon: React.ReactNode;
  label: string;
  date?: string;
  done?: boolean;
  highlight?: "warning" | "danger";
  daysRemaining?: number;
}): JSX.Element {
  const t = useTranslations("basvurularim");

  return (
    <li
      className={cn(
        "flex items-center gap-3 text-sm",
        done ? "text-warm-700" : "text-warm-500",
        highlight === "warning" && "text-dlp-700",
        highlight === "danger" && "text-red-700"
      )}
    >
      <span className="flex-shrink-0" aria-hidden="true">
        {icon}
      </span>
      <span className="flex-1">{label}</span>
      {date && <span className="text-xs tabular-nums">{date}</span>}
      {daysRemaining !== undefined && daysRemaining >= 0 && (
        <span
          className={cn(
            "text-xs font-medium tabular-nums",
            highlight === "warning" ? "text-dlp-600" : "text-portal-600"
          )}
        >
          {t("daysRemaining", { n: daysRemaining })}
        </span>
      )}
    </li>
  );
}
