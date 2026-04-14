"use client";

import { useEffect, useState } from "react";
import { useTranslations, useLocale } from "next-intl";
import Link from "next/link";
import { Monitor, FileText, Eye, ShieldAlert } from "lucide-react";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { listEndpoints } from "@/lib/api/endpoints";
import { listDSRs } from "@/lib/api/dsr";
import { listLiveViewRequests } from "@/lib/api/liveview";
import { cn } from "@/lib/utils";

interface LiveMetricsProps {
  token?: string;
  /** Poll interval in ms. Default 15s per spec. */
  pollIntervalMs?: number;
}

interface MetricSnapshot {
  activeEndpoints: number;
  openDsrs: number;
  liveViewPending: number;
  policyViolations: number;
}

// Rolling history — last N samples for the sparkline.
const HISTORY_SIZE = 24;

function useLiveMetrics(
  token: string | undefined,
  pollIntervalMs: number,
): {
  current: MetricSnapshot | null;
  history: MetricSnapshot[];
  lastUpdated: Date | null;
} {
  const [current, setCurrent] = useState<MetricSnapshot | null>(null);
  const [history, setHistory] = useState<MetricSnapshot[]>([]);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function fetchSnapshot(): Promise<void> {
      const opts = token ? { token } : {};
      const [endpoints, dsrs, liveViews] = await Promise.allSettled([
        listEndpoints({ status: "active", page_size: 1 }, opts),
        listDSRs({ state: "open", page_size: 1 }, opts),
        listLiveViewRequests({ state: "REQUESTED", page_size: 1 }, opts),
      ]);

      if (cancelled) return;

      const snapshot: MetricSnapshot = {
        activeEndpoints:
          endpoints.status === "fulfilled"
            ? endpoints.value.pagination.total
            : 0,
        openDsrs:
          dsrs.status === "fulfilled" ? dsrs.value.pagination.total : 0,
        liveViewPending:
          liveViews.status === "fulfilled"
            ? liveViews.value.pagination.total
            : 0,
        // TODO: wire to /v1/policy/violations when that endpoint exists.
        // For now hold at 0 — sparkline will degrade gracefully.
        policyViolations: 0,
      };

      setCurrent(snapshot);
      setHistory((prev) => {
        const next = [...prev, snapshot];
        return next.length > HISTORY_SIZE ? next.slice(-HISTORY_SIZE) : next;
      });
      setLastUpdated(new Date());
    }

    void fetchSnapshot();
    const timer = setInterval(() => {
      void fetchSnapshot();
    }, pollIntervalMs);

    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [token, pollIntervalMs]);

  return { current, history, lastUpdated };
}

// Simple inline SVG sparkline — no external deps. Draws a polyline over the
// last HISTORY_SIZE samples, normalized to the card width.
interface SparklineProps {
  values: number[];
  color?: string;
  width?: number;
  height?: number;
}

function Sparkline({
  values,
  color = "currentColor",
  width = 80,
  height = 24,
}: SparklineProps): JSX.Element {
  if (values.length === 0) {
    return (
      <svg
        width={width}
        height={height}
        className="text-muted-foreground/20"
        aria-hidden="true"
      >
        <line
          x1={0}
          y1={height / 2}
          x2={width}
          y2={height / 2}
          stroke="currentColor"
          strokeWidth={1}
          strokeDasharray="2 2"
        />
      </svg>
    );
  }
  const max = Math.max(...values, 1);
  const min = Math.min(...values, 0);
  const span = max - min || 1;
  const step = values.length > 1 ? width / (values.length - 1) : 0;
  const points = values
    .map((v, i) => {
      const x = i * step;
      const y = height - ((v - min) / span) * height;
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      className={color}
      aria-hidden="true"
    >
      <polyline
        points={points}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.5}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}

interface MetricCardProps {
  title: string;
  value: number;
  icon: React.ElementType;
  history: number[];
  href: string;
  variant?: "default" | "warning" | "critical";
}

function MetricCard({
  title,
  value,
  icon: Icon,
  history,
  href,
  variant = "default",
}: MetricCardProps): JSX.Element {
  const locale = useLocale();
  const color =
    variant === "critical"
      ? "text-red-500"
      : variant === "warning"
        ? "text-amber-500"
        : "text-green-600";
  return (
    <Link href={`/${locale}${href}`} className="block group">
      <Card className="transition-shadow group-hover:shadow-md">
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">{title}</CardTitle>
          <Icon
            className={cn("h-4 w-4", color)}
            aria-hidden="true"
          />
        </CardHeader>
        <CardContent>
          <div className="flex items-end justify-between gap-3">
            <div className={cn("text-3xl font-bold tabular-nums", color)}>
              {value.toLocaleString("tr-TR")}
            </div>
            <Sparkline values={history} color={color} />
          </div>
        </CardContent>
      </Card>
    </Link>
  );
}

export function LiveMetrics({
  token,
  pollIntervalMs = 15_000,
}: LiveMetricsProps): JSX.Element {
  const t = useTranslations("dashboard.liveMetrics");
  const { current, history, lastUpdated } = useLiveMetrics(token, pollIntervalMs);

  // Extract per-metric history arrays for sparklines
  const activeHist = history.map((s) => s.activeEndpoints);
  const dsrHist = history.map((s) => s.openDsrs);
  const lvHist = history.map((s) => s.liveViewPending);
  const policyHist = history.map((s) => s.policyViolations);

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider">
          {t("title")}
        </h2>
        {lastUpdated && (
          <p className="text-xs text-muted-foreground">
            {t("lastUpdated", {
              time: lastUpdated.toLocaleTimeString("tr-TR"),
            })}
          </p>
        )}
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <MetricCard
          title={t("activeEndpoints")}
          value={current?.activeEndpoints ?? 0}
          icon={Monitor}
          history={activeHist}
          href="/endpoints"
        />
        <MetricCard
          title={t("openDsrs")}
          value={current?.openDsrs ?? 0}
          icon={FileText}
          history={dsrHist}
          href="/kvkk/dsr"
          variant={(current?.openDsrs ?? 0) > 0 ? "warning" : "default"}
        />
        <MetricCard
          title={t("liveViewPending")}
          value={current?.liveViewPending ?? 0}
          icon={Eye}
          history={lvHist}
          href="/live-view"
          variant={(current?.liveViewPending ?? 0) > 0 ? "warning" : "default"}
        />
        <MetricCard
          title={t("policyViolations")}
          value={current?.policyViolations ?? 0}
          icon={ShieldAlert}
          history={policyHist}
          href="/audit"
          variant={(current?.policyViolations ?? 0) > 0 ? "critical" : "default"}
        />
      </div>
    </div>
  );
}
