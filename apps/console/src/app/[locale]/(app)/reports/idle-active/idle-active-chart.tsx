"use client";

import { useTranslations } from "next-intl";
import type { IdleActivePreviewRow } from "@/lib/api/reports";

interface Props {
  items: IdleActivePreviewRow[];
  from: string;
  to: string;
}

function formatHours(sec: number): string {
  const m = Math.round(sec / 60);
  if (m < 60) return `${m} dk`;
  const h = Math.floor(m / 60);
  const mm = m % 60;
  return mm === 0 ? `${h} sa` : `${h} sa ${mm} dk`;
}

export function IdleActiveChart({ items, from, to }: Props): JSX.Element {
  const t = useTranslations("reports.idleActive");

  const totalActive = items.reduce((s, r) => s + r.active_seconds, 0);
  const totalIdle = items.reduce((s, r) => s + r.idle_seconds, 0);
  const totalSeconds = totalActive + totalIdle;
  const globalActiveRatio = totalSeconds > 0 ? totalActive / totalSeconds : 0;

  const maxTotal = Math.max(
    1,
    ...items.map((r) => r.active_seconds + r.idle_seconds),
  );

  return (
    <div className="space-y-6">
      <div className="rounded-xl border bg-card p-6">
        <div className="flex items-center justify-between flex-wrap gap-3">
          <div>
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              {t("rangeLabel")}
            </div>
            <div className="text-sm font-medium tabular-nums">
              {from.slice(0, 10)} → {to.slice(0, 10)}
            </div>
          </div>
          <div className="flex items-center gap-6">
            <div>
              <div className="text-xs text-muted-foreground">
                {t("activeRatio")}
              </div>
              <div className="text-2xl font-bold tabular-nums text-green-600">
                {(globalActiveRatio * 100).toFixed(1)}%
              </div>
            </div>
            <div>
              <div className="text-xs text-muted-foreground">
                {t("totalActive")}
              </div>
              <div className="text-xl font-bold tabular-nums text-green-600">
                {formatHours(totalActive)}
              </div>
            </div>
            <div>
              <div className="text-xs text-muted-foreground">
                {t("totalIdle")}
              </div>
              <div className="text-xl font-bold tabular-nums text-amber-600">
                {formatHours(totalIdle)}
              </div>
            </div>
          </div>
        </div>
      </div>

      <div className="rounded-xl border bg-card p-6">
        <h2 className="text-base font-semibold mb-4">{t("dailyTitle")}</h2>
        <div className="space-y-3">
          {items.map((row) => {
            const total = row.active_seconds + row.idle_seconds;
            const activePct = total === 0 ? 0 : (row.active_seconds / total) * 100;
            const totalBarPct = (total / maxTotal) * 100;
            return (
              <div key={row.date}>
                <div className="flex items-center justify-between text-sm mb-1">
                  <div className="flex items-center gap-3">
                    <span className="text-xs text-muted-foreground w-24 tabular-nums">
                      {row.date}
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {row.employee_count} {t("employees")}
                    </span>
                  </div>
                  <span className="text-xs tabular-nums">
                    {formatHours(total)}
                    <span className="text-muted-foreground ml-2">
                      ({(row.active_ratio * 100).toFixed(0)}% aktif)
                    </span>
                  </span>
                </div>
                <div className="h-3 rounded-full bg-muted overflow-hidden relative">
                  <div
                    className="h-full flex"
                    style={{ width: `${totalBarPct}%` }}
                  >
                    <div
                      className="h-full bg-green-500"
                      style={{ width: `${activePct}%` }}
                    />
                    <div
                      className="h-full bg-amber-400 flex-1"
                    />
                  </div>
                </div>
              </div>
            );
          })}
        </div>
        <div className="flex items-center gap-4 mt-4 text-xs">
          <span className="inline-flex items-center gap-1.5">
            <span className="h-3 w-3 rounded bg-green-500" />
            <span className="text-muted-foreground">{t("legendActive")}</span>
          </span>
          <span className="inline-flex items-center gap-1.5">
            <span className="h-3 w-3 rounded bg-amber-400" />
            <span className="text-muted-foreground">{t("legendIdle")}</span>
          </span>
        </div>
      </div>
    </div>
  );
}
