"use client";

import { useMemo, useState } from "react";
import { useTranslations } from "next-intl";
import { useQuery } from "@tanstack/react-query";
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

  const gapSet = useMemo(
    () => new Set((data?.gap_controls ?? []).map((c) => c)),
    [data]
  );

  const downloadURL = buildEvidencePackURL(period);

  return (
    <div className="space-y-6">
      {/* Period picker — a simple month input, cheap + no dep */}
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

      {/* Summary */}
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

      {/* Gap list — surfaced prominently since zero-evidence controls
          are the whole reason this page exists */}
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

      {/* Coverage matrix */}
      <div className="overflow-hidden rounded-md border">
        <table className="w-full text-sm">
          <thead className="bg-muted/50">
            <tr>
              <th className="px-4 py-2 text-left font-semibold">
                {t("colControl")}
              </th>
              <th className="px-4 py-2 text-right font-semibold">
                {t("colCount")}
              </th>
              <th className="px-4 py-2 text-left font-semibold">
                {t("colStatus")}
              </th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {(data?.by_control ?? []).map((row) => (
              <tr key={row.control} className="hover:bg-muted/30">
                <td className="px-4 py-2 font-mono font-semibold">
                  {row.control}
                </td>
                <td className="px-4 py-2 text-right tabular-nums">
                  {row.count}
                </td>
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

      {isFetching && (
        <p className="text-xs text-muted-foreground">{t("refreshing")}</p>
      )}
    </div>
  );
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
