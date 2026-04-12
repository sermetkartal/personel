"use client";

import { useMemo } from "react";
import { useTranslations } from "next-intl";
import type { ProductivityPreviewRow } from "@/lib/api/reports";

interface Props {
  items: ProductivityPreviewRow[];
  from: string;
  to: string;
}

function formatHoursFromSeconds(sec: number): string {
  const m = Math.round(sec / 60);
  if (m < 60) return `${m} dk`;
  const h = Math.floor(m / 60);
  const mm = m % 60;
  return mm === 0 ? `${h} sa` : `${h} sa ${mm} dk`;
}

export function ProductivityChart({ items, from, to }: Props): JSX.Element {
  const t = useTranslations("reports.productivity");

  // Group items by day so the chart renders as a strip per day with 24
  // hour columns. Our Postgres source stores hour buckets; ClickHouse
  // Phase 2 will return identical shape.
  const { days, totals, maxHourSec } = useMemo(() => {
    const byDay = new Map<string, ProductivityPreviewRow[]>();
    for (const r of items) {
      const day = r.hour.slice(0, 10);
      const list = byDay.get(day) ?? [];
      list.push(r);
      byDay.set(day, list);
    }
    const sortedDays = Array.from(byDay.entries()).sort(([a], [b]) =>
      a.localeCompare(b),
    );
    const max = Math.max(
      1,
      ...items.map((r) => r.active_seconds + r.idle_seconds),
    );
    const totalActive = items.reduce((s, r) => s + r.active_seconds, 0);
    const totalIdle = items.reduce((s, r) => s + r.idle_seconds, 0);
    return {
      days: sortedDays,
      totals: { active: totalActive, idle: totalIdle },
      maxHourSec: max,
    };
  }, [items]);

  const fromDisplay = from.slice(0, 10);
  const toDisplay = to.slice(0, 10);

  return (
    <div className="space-y-6">
      <div className="rounded-xl border bg-card p-6">
        <div className="flex items-center justify-between flex-wrap gap-3">
          <div>
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              {t("rangeLabel")}
            </div>
            <div className="text-sm font-medium tabular-nums">
              {fromDisplay} → {toDisplay}
            </div>
          </div>
          <div className="flex items-center gap-6">
            <div>
              <div className="text-xs text-muted-foreground">
                {t("totalActive")}
              </div>
              <div className="text-xl font-bold tabular-nums text-green-600">
                {formatHoursFromSeconds(totals.active)}
              </div>
            </div>
            <div>
              <div className="text-xs text-muted-foreground">
                {t("totalIdle")}
              </div>
              <div className="text-xl font-bold tabular-nums text-amber-600">
                {formatHoursFromSeconds(totals.idle)}
              </div>
            </div>
          </div>
        </div>
      </div>

      <div className="rounded-xl border bg-card p-6">
        <h2 className="text-base font-semibold mb-4">{t("dailyStripsTitle")}</h2>
        <div className="space-y-4">
          {days.map(([day, rows]) => {
            const byHour = new Map<number, ProductivityPreviewRow>();
            for (const r of rows) {
              const hour = parseInt(r.hour.slice(11, 13), 10);
              byHour.set(hour, r);
            }
            return (
              <div key={day}>
                <div className="text-xs text-muted-foreground mb-1 tabular-nums">
                  {day}
                </div>
                <div className="flex gap-0.5 h-20 items-stretch">
                  {Array.from({ length: 24 }, (_, h) => {
                    const row = byHour.get(h);
                    const total = row
                      ? row.active_seconds + row.idle_seconds
                      : 0;
                    const activePct =
                      total === 0
                        ? 0
                        : Math.min(100, (row!.active_seconds / maxHourSec) * 100);
                    const idlePct =
                      total === 0
                        ? 0
                        : Math.min(100, (row!.idle_seconds / maxHourSec) * 100);
                    return (
                      <div
                        key={h}
                        className="flex-1 h-full flex flex-col justify-end min-w-0 group relative"
                        title={`${String(h).padStart(2, "0")}:00 — ${row ? formatHoursFromSeconds(row.active_seconds) : "0 dk"} aktif`}
                      >
                        {total === 0 ? (
                          <div className="h-px w-full bg-border/60" />
                        ) : (
                          <>
                            <div
                              className="w-full bg-amber-400/80"
                              style={{ height: `${idlePct}%` }}
                            />
                            <div
                              className="w-full bg-green-500"
                              style={{ height: `${activePct}%` }}
                            />
                          </>
                        )}
                      </div>
                    );
                  })}
                </div>
                <div className="flex gap-0.5 mt-1 text-[9px] text-muted-foreground tabular-nums">
                  {Array.from({ length: 24 }, (_, h) => (
                    <div key={h} className="flex-1 text-center">
                      {h % 6 === 0 ? String(h).padStart(2, "0") : ""}
                    </div>
                  ))}
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
