"use client";

import { useMemo, useState } from "react";
import { useTranslations } from "next-intl";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Download, AlertTriangle, CheckCircle2 } from "lucide-react";
import {
  buildEvidencePackURL,
  evidenceKeys,
  getEvidenceCoverage,
  type CoverageResponse,
} from "@/lib/api/evidence";

interface EvidenceCoverageClientProps {
  initialCoverage: CoverageResponse | null;
  initialPeriod: string;
  canDownload: boolean;
}

// lastNPeriods returns the last n calendar months in descending order
// (most recent first) in YYYY-MM format. Used to render the 12-month
// history heatmap so DPO can see coverage trends across the Type II
// observation window without having to switch periods manually.
function lastNPeriods(n: number, anchor: Date = new Date()): string[] {
  const out: string[] = [];
  const d = new Date(anchor.getFullYear(), anchor.getMonth(), 1);
  for (let i = 0; i < n; i++) {
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, "0");
    out.push(`${y}-${m}`);
    d.setMonth(d.getMonth() - 1);
  }
  return out;
}

export function EvidenceCoverageClient({
  initialCoverage,
  initialPeriod,
  canDownload,
}: EvidenceCoverageClientProps): JSX.Element {
  const t = useTranslations("evidence");
  const [period, setPeriod] = useState<string>(initialPeriod);

  const { data, isFetching } = useQuery({
    queryKey: evidenceKeys.coverage(period),
    queryFn: () => getEvidenceCoverage(period),
    initialData: period === initialPeriod ? initialCoverage ?? undefined : undefined,
  });

  // 12-month history: parallel fetches, one query per month. TanStack
  // Query deduplicates by queryKey, so opening and closing the page
  // multiple times doesn't thrash the API.
  const historyPeriods = useMemo(() => lastNPeriods(12), []);
  const historyQueries = useQueries({
    queries: historyPeriods.map((p) => ({
      queryKey: evidenceKeys.coverage(p),
      queryFn: () => getEvidenceCoverage(p),
      staleTime: 5 * 60_000,
    })),
  });

  const gapSet = useMemo(
    () => new Set((data?.gap_controls ?? []).map((c) => c)),
    [data]
  );

  const downloadURL = buildEvidencePackURL(period);

  // Build history matrix: map period → { control → count }.
  // Controls are the union of every control seen across the 12 months
  // so a control that drops to zero mid-window still shows a column.
  const historyMatrix = useMemo(() => {
    const controlSet = new Set<string>();
    const byPeriod: Record<string, Record<string, number>> = {};
    historyQueries.forEach((q, i) => {
      const p = historyPeriods[i]!;
      const resp = q.data;
      byPeriod[p] = {};
      if (resp) {
        resp.by_control.forEach((row) => {
          controlSet.add(row.control);
          byPeriod[p]![row.control] = row.count;
        });
      }
    });
    const controls = Array.from(controlSet).sort();
    return { controls, byPeriod };
  }, [historyQueries, historyPeriods]);

  return (
    <div className="space-y-6">
      {/* Period picker */}
      <div className="flex items-end gap-4">
        <div>
          <label
            htmlFor="period"
            className="block text-sm font-medium text-muted-foreground"
          >
            {t("periodLabel")}
          </label>
          <input
            id="period"
            type="month"
            value={period}
            onChange={(e) => setPeriod(e.target.value)}
            className="mt-1 rounded-md border bg-background px-3 py-2 text-sm"
          />
        </div>

        {canDownload && (
          <a
            href={downloadURL}
            download
            className="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground hover:opacity-90"
          >
            <Download className="h-4 w-4" aria-hidden />
            {t("downloadPack")}
          </a>
        )}
      </div>

      {/* Summary cards */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <SummaryCard
          label={t("totalItems")}
          value={data?.total_items ?? 0}
          tone="neutral"
        />
        <SummaryCard
          label={t("controlsCovered")}
          value={(data?.by_control.filter((r) => r.count > 0).length ?? 0)}
          tone="success"
        />
        <SummaryCard
          label={t("gapControls")}
          value={gapSet.size}
          tone={gapSet.size > 0 ? "warn" : "success"}
        />
      </div>

      {/* Gap alert */}
      {gapSet.size > 0 && (
        <div className="rounded-md border border-amber-500/40 bg-amber-500/5 p-4">
          <div className="flex items-start gap-2">
            <AlertTriangle className="h-5 w-5 text-amber-600 mt-0.5" aria-hidden />
            <div>
              <p className="font-semibold text-amber-700">{t("gapTitle")}</p>
              <p className="text-sm text-muted-foreground mt-1">
                {t("gapDescription")}
              </p>
              <ul className="mt-2 flex flex-wrap gap-2">
                {Array.from(gapSet)
                  .sort()
                  .map((c) => (
                    <li
                      key={c}
                      className="rounded bg-amber-500/20 px-2 py-1 text-xs font-mono font-semibold text-amber-800"
                    >
                      {c}
                    </li>
                  ))}
              </ul>
            </div>
          </div>
        </div>
      )}

      {/* Current-period coverage matrix */}
      <div>
        <h2 className="mb-2 text-sm font-semibold text-muted-foreground uppercase tracking-wide">
          {t("matrixHeading")}
        </h2>
        <div className="overflow-hidden rounded-md border">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="px-4 py-2 text-left font-semibold">{t("colControl")}</th>
                <th className="px-4 py-2 text-right font-semibold">{t("colCount")}</th>
                <th className="px-4 py-2 text-left font-semibold">{t("colStatus")}</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {(data?.by_control ?? []).map((row) => (
                <tr key={row.control} className="hover:bg-muted/30">
                  <td className="px-4 py-2 font-mono font-semibold">{row.control}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{row.count}</td>
                  <td className="px-4 py-2">
                    {row.count > 0 ? (
                      <span className="inline-flex items-center gap-1 text-green-700">
                        <CheckCircle2 className="h-4 w-4" aria-hidden />
                        {t("statusCovered")}
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1 text-amber-700">
                        <AlertTriangle className="h-4 w-4" aria-hidden />
                        {t("statusGap")}
                      </span>
                    )}
                  </td>
                </tr>
              ))}
              {data?.by_control.length === 0 && (
                <tr>
                  <td colSpan={3} className="px-4 py-8 text-center text-muted-foreground">
                    {t("emptyState")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* 12-month history heatmap */}
      <div>
        <h2 className="mb-2 text-sm font-semibold text-muted-foreground uppercase tracking-wide">
          {t("historyHeading")}
        </h2>
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full text-xs">
            <thead className="bg-muted/50">
              <tr>
                <th className="px-3 py-2 text-left font-semibold">{t("colControl")}</th>
                {historyPeriods
                  .slice()
                  .reverse()
                  .map((p) => (
                    <th
                      key={p}
                      className="px-2 py-2 text-center font-mono font-semibold"
                      title={p}
                    >
                      {p.slice(5)}
                    </th>
                  ))}
              </tr>
            </thead>
            <tbody className="divide-y">
              {historyMatrix.controls.map((c) => (
                <tr key={c} className="hover:bg-muted/30">
                  <td className="px-3 py-1.5 font-mono font-semibold">{c}</td>
                  {historyPeriods
                    .slice()
                    .reverse()
                    .map((p) => {
                      const count = historyMatrix.byPeriod[p]?.[c] ?? 0;
                      return (
                        <td
                          key={p}
                          className={`px-2 py-1.5 text-center tabular-nums ${heatClass(count)}`}
                          title={`${c} / ${p}: ${count}`}
                        >
                          {count}
                        </td>
                      );
                    })}
                </tr>
              ))}
              {historyMatrix.controls.length === 0 && (
                <tr>
                  <td
                    colSpan={13}
                    className="px-4 py-8 text-center text-muted-foreground"
                  >
                    {t("historyLoading")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        <p className="mt-2 text-xs text-muted-foreground">{t("historyHint")}</p>
      </div>

      {isFetching && (
        <p className="text-xs text-muted-foreground">{t("refreshing")}</p>
      )}
    </div>
  );
}

// heatClass returns a Tailwind background class based on the item count.
// 0 = amber gap, 1–2 = muted green, 3+ = stronger green. Keeps the
// palette aligned with the rest of the app (no new colors introduced).
function heatClass(count: number): string {
  if (count === 0) return "bg-amber-500/15 text-amber-800";
  if (count <= 2) return "bg-green-500/10 text-green-800";
  if (count <= 5) return "bg-green-500/20 text-green-800";
  return "bg-green-500/30 text-green-900 font-semibold";
}

interface SummaryCardProps {
  label: string;
  value: number;
  tone: "neutral" | "success" | "warn";
}

function SummaryCard({ label, value, tone }: SummaryCardProps): JSX.Element {
  const toneClass =
    tone === "success"
      ? "border-green-500/30 bg-green-500/5"
      : tone === "warn"
      ? "border-amber-500/30 bg-amber-500/5"
      : "";
  return (
    <div className={`rounded-md border p-4 ${toneClass}`}>
      <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </p>
      <p className="mt-1 text-2xl font-bold tabular-nums">{value}</p>
    </div>
  );
}
