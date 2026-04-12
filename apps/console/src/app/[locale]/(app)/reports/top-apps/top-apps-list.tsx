"use client";

import { useTranslations } from "next-intl";
import type { TopAppPreviewRow } from "@/lib/api/reports";

interface Props {
  items: TopAppPreviewRow[];
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

function categoryBar(cat: TopAppPreviewRow["category"]): string {
  switch (cat) {
    case "productive":
      return "bg-green-500";
    case "distracting":
      return "bg-red-500";
    default:
      return "bg-muted-foreground/50";
  }
}

function categoryLabel(
  cat: TopAppPreviewRow["category"],
  t: (k: string) => string,
): string {
  switch (cat) {
    case "productive":
      return t("category.productive");
    case "distracting":
      return t("category.distracting");
    default:
      return t("category.neutral");
  }
}

export function TopAppsList({ items, from, to }: Props): JSX.Element {
  const t = useTranslations("reports.topApps");

  const totals = {
    productive: items
      .filter((a) => a.category === "productive")
      .reduce((s, a) => s + a.focus_seconds, 0),
    neutral: items
      .filter((a) => a.category === "neutral")
      .reduce((s, a) => s + a.focus_seconds, 0),
    distracting: items
      .filter((a) => a.category === "distracting")
      .reduce((s, a) => s + a.focus_seconds, 0),
  };
  const grandTotal = totals.productive + totals.neutral + totals.distracting;

  const maxFocus = Math.max(1, ...items.map((a) => a.focus_seconds));

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
          <div className="flex items-center gap-6 text-sm">
            <MiniStat
              color="bg-green-500"
              label={t("category.productive")}
              value={formatHours(totals.productive)}
              pct={grandTotal > 0 ? (totals.productive / grandTotal) * 100 : 0}
            />
            <MiniStat
              color="bg-muted-foreground/50"
              label={t("category.neutral")}
              value={formatHours(totals.neutral)}
              pct={grandTotal > 0 ? (totals.neutral / grandTotal) * 100 : 0}
            />
            <MiniStat
              color="bg-red-500"
              label={t("category.distracting")}
              value={formatHours(totals.distracting)}
              pct={grandTotal > 0 ? (totals.distracting / grandTotal) * 100 : 0}
            />
          </div>
        </div>
      </div>

      <div className="rounded-xl border bg-card p-6">
        <h2 className="text-base font-semibold mb-4">{t("listTitle")}</h2>
        <ul className="space-y-3">
          {items.map((app, idx) => {
            const pct = Math.round((app.focus_seconds / maxFocus) * 100);
            return (
              <li key={app.app_name}>
                <div className="flex items-center justify-between mb-1 text-sm">
                  <div className="flex items-center gap-3 min-w-0">
                    <span className="text-xs text-muted-foreground tabular-nums w-5">
                      {idx + 1}
                    </span>
                    <span className="font-medium truncate">{app.app_name}</span>
                    <span className="text-xs text-muted-foreground">
                      · {categoryLabel(app.category, t)}
                    </span>
                  </div>
                  <span className="text-xs tabular-nums text-muted-foreground ml-2">
                    {formatHours(app.focus_seconds)}{" "}
                    <span className="opacity-60">
                      ({app.focus_pct.toFixed(1)}%)
                    </span>
                  </span>
                </div>
                <div className="h-2 rounded-full bg-muted overflow-hidden">
                  <div
                    className={`h-full ${categoryBar(app.category)}`}
                    style={{ width: `${pct}%` }}
                  />
                </div>
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
}

function MiniStat({
  color,
  label,
  value,
  pct,
}: {
  color: string;
  label: string;
  value: string;
  pct: number;
}): JSX.Element {
  return (
    <div>
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <span className={`h-2 w-2 rounded ${color}`} />
        {label}
      </div>
      <div className="text-base font-bold tabular-nums">
        {value}
        <span className="text-xs font-normal text-muted-foreground ml-1">
          ({pct.toFixed(0)}%)
        </span>
      </div>
    </div>
  );
}
