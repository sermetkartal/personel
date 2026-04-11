import { useTranslations, useLocale } from "next-intl";
import { MonitorOff, EyeOff } from "lucide-react";
import type { MyLiveViewEntry, MyLiveViewHistory } from "@/lib/api/types";
import { formatDateTime, formatDurationSeconds } from "@/lib/utils";
import { cn } from "@/lib/utils";

interface SessionHistoryListProps {
  data: MyLiveViewHistory;
}

/**
 * Displays the employee's own past live view sessions.
 *
 * Privacy guarantees per live-view-protocol.md:
 * - requester_role and approver_role shown as labels ONLY (yonetici, ik, etc.)
 * - NO user names, NO user IDs of the admin/HR who ran the session
 * - If restricted: true, empty state with explanation shown instead
 */
export function SessionHistoryList({
  data,
}: SessionHistoryListProps): JSX.Element {
  const t = useTranslations("oturumGecmisi");
  const locale = useLocale() as "tr" | "en";

  // DPO has restricted visibility
  if (data.restricted) {
    return (
      <div className="card text-center py-12">
        <div
          className="w-12 h-12 rounded-xl bg-warm-100 flex items-center justify-center mx-auto mb-4"
          aria-hidden="true"
        >
          <EyeOff className="w-6 h-6 text-warm-400" />
        </div>
        <h2 className="text-base font-medium text-warm-800 mb-2">
          {t("restricted")}
        </h2>
        <p className="text-sm text-warm-500 max-w-sm mx-auto leading-relaxed">
          {t("restrictedDetail")}
        </p>
      </div>
    );
  }

  // No sessions
  if (data.items.length === 0) {
    return (
      <div className="card text-center py-12">
        <div
          className="w-12 h-12 rounded-xl bg-trust-50 flex items-center justify-center mx-auto mb-4"
          aria-hidden="true"
        >
          <MonitorOff className="w-6 h-6 text-trust-500" />
        </div>
        <h2 className="text-base font-medium text-warm-800 mb-2">
          {t("noSessions")}
        </h2>
        <p className="text-sm text-warm-500 max-w-sm mx-auto leading-relaxed">
          {t("noSessionsDetail")}
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Privacy note */}
      <div className="rounded-xl bg-portal-50 border border-portal-100 px-4 py-3 text-xs text-portal-700">
        {t("privacyNote")}
      </div>

      {/* Session list */}
      <ol className="space-y-3" aria-label="Canlı izleme oturum geçmişi">
        {data.items.map((session) => (
          <SessionRow key={session.session_id} session={session} locale={locale} />
        ))}
      </ol>
    </div>
  );
}

function SessionRow({
  session,
  locale,
}: {
  session: MyLiveViewEntry;
  locale: "tr" | "en";
}): JSX.Element {
  const t = useTranslations("oturumGecmisi");

  const stateLabel =
    session.state in
    {
      ENDED: true,
      TERMINATED_BY_HR: true,
      TERMINATED_BY_DPO: true,
      EXPIRED: true,
    }
      ? t(`states.${session.state as "ENDED" | "TERMINATED_BY_HR" | "TERMINATED_BY_DPO" | "EXPIRED"}`)
      : session.state;

  const stateColors: Record<string, string> = {
    ENDED: "bg-trust-100 text-trust-700",
    TERMINATED_BY_HR: "bg-dlp-100 text-dlp-800",
    TERMINATED_BY_DPO: "bg-dlp-100 text-dlp-800",
    EXPIRED: "bg-warm-100 text-warm-600",
  };

  const stateColor = stateColors[session.state] ?? "bg-warm-100 text-warm-600";

  return (
    <li className="card p-4">
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <time
              dateTime={session.started_at}
              className="text-sm font-medium text-warm-900"
            >
              {formatDateTime(session.started_at, locale)}
            </time>
            <span className={cn("badge text-xs", stateColor)}>{stateLabel}</span>
          </div>

          <dl className="grid grid-cols-2 gap-x-8 gap-y-1 text-xs text-warm-600 mt-2">
            <div className="flex gap-1.5">
              <dt className="text-warm-400">{t("requesterRole")}:</dt>
              <dd className="font-medium">
                {t(`roles.${session.requester_role as "yonetici" | "mudur" | "ik"}`)}
              </dd>
            </div>
            <div className="flex gap-1.5">
              <dt className="text-warm-400">{t("approverRole")}:</dt>
              <dd className="font-medium">
                {t(`roles.${session.approver_role as "yonetici" | "mudur" | "ik"}`)}
              </dd>
            </div>
            <div className="flex gap-1.5">
              <dt className="text-warm-400">{t("reasonCategory")}:</dt>
              <dd>
                {t(
                  `reasonCategories.${session.reason_category as "performans_degerlendirme" | "guvenlik_incelemesi" | "is_sureci_denetimi" | "diger"}`
                )}
              </dd>
            </div>
            {session.duration_seconds !== null && (
              <div className="flex gap-1.5">
                <dt className="text-warm-400">{t("duration")}:</dt>
                <dd>{formatDurationSeconds(session.duration_seconds, locale)}</dd>
              </div>
            )}
          </dl>
        </div>

        {/* Session ID for reference */}
        <code
          className="text-xs text-warm-300 font-mono flex-shrink-0 hidden sm:block"
          title={t("sessionId")}
        >
          {session.session_id.slice(0, 8)}…
        </code>
      </div>
    </li>
  );
}
