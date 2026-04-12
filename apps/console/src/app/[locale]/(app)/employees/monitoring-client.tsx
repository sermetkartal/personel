"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useTranslations } from "next-intl";
import {
  Activity,
  Clock,
  Camera,
  Keyboard,
  Monitor,
  Search,
} from "lucide-react";
import type {
  EmployeeOverviewResponse,
  EmployeeOverviewRow,
  TopApp,
} from "@/lib/api/employees";

interface Props {
  initialOverview: EmployeeOverviewResponse | null;
}

export function EmployeesMonitoringClient({ initialOverview }: Props): JSX.Element {
  const t = useTranslations("employees");
  const params = useParams<{ locale: string }>();
  const locale = params?.locale ?? "tr";
  const [search, setSearch] = useState("");
  const [deptFilter, setDeptFilter] = useState<string>("all");

  const rows = initialOverview?.items ?? [];

  const departments = useMemo(() => {
    const set = new Set<string>();
    rows.forEach((r) => {
      if (r.department) set.add(r.department);
    });
    return Array.from(set).sort();
  }, [rows]);

  const filtered = useMemo(() => {
    return rows.filter((r) => {
      if (deptFilter !== "all" && r.department !== deptFilter) return false;
      if (search) {
        const q = search.toLocaleLowerCase("tr");
        if (
          !r.username.toLocaleLowerCase("tr").includes(q) &&
          !r.email.toLocaleLowerCase("tr").includes(q) &&
          !(r.full_name ?? "").toLocaleLowerCase("tr").includes(q)
        ) {
          return false;
        }
      }
      return true;
    });
  }, [rows, search, deptFilter]);

  // Aggregate stats for the header cards
  const stats = useMemo(() => {
    const activeNow = filtered.filter((r) => r.is_currently_active).length;
    const totalActiveMin = filtered.reduce((s, r) => s + r.today.active_minutes, 0);
    const totalIdleMin = filtered.reduce((s, r) => s + r.today.idle_minutes, 0);
    const totalScreens = filtered.reduce((s, r) => s + r.today.screenshot_count, 0);
    const avgScore =
      filtered.length === 0
        ? 0
        : Math.round(
            filtered.reduce((s, r) => s + r.today.productivity_score, 0) / filtered.length,
          );
    return { activeNow, totalActiveMin, totalIdleMin, totalScreens, avgScore };
  }, [filtered]);

  return (
    <div className="space-y-6">
      {/* Summary cards */}
      <div className="grid grid-cols-2 lg:grid-cols-5 gap-3">
        <StatCard
          label={t("stats.activeNow")}
          value={`${stats.activeNow}/${filtered.length}`}
          icon={<Activity className="h-4 w-4" />}
          tone="success"
        />
        <StatCard
          label={t("stats.totalActive")}
          value={formatHours(stats.totalActiveMin)}
          icon={<Clock className="h-4 w-4" />}
        />
        <StatCard
          label={t("stats.totalIdle")}
          value={formatHours(stats.totalIdleMin)}
          icon={<Clock className="h-4 w-4" />}
          tone="warn"
        />
        <StatCard
          label={t("stats.screenshots")}
          value={stats.totalScreens.toLocaleString("tr-TR")}
          icon={<Camera className="h-4 w-4" />}
        />
        <StatCard
          label={t("stats.avgScore")}
          value={`${stats.avgScore}/100`}
          icon={<Monitor className="h-4 w-4" />}
          tone={stats.avgScore >= 60 ? "success" : stats.avgScore >= 40 ? "warn" : "danger"}
        />
      </div>

      {/* Filter bar */}
      <div className="flex flex-wrap items-center gap-3">
        <div className="relative flex-1 min-w-[240px]">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <input
            type="search"
            placeholder={t("search.placeholder")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full rounded-md border bg-background pl-9 pr-3 py-2 text-sm"
          />
        </div>
        <select
          value={deptFilter}
          onChange={(e) => setDeptFilter(e.target.value)}
          className="rounded-md border bg-background px-3 py-2 text-sm"
        >
          <option value="all">{t("filter.allDepartments")}</option>
          {departments.map((d) => (
            <option key={d} value={d}>
              {d}
            </option>
          ))}
        </select>
      </div>

      {/* Employee table */}
      <div className="overflow-x-auto rounded-md border">
        <table className="w-full text-sm">
          <thead className="bg-muted/50">
            <tr>
              <th className="px-4 py-2 text-left font-semibold">{t("col.employee")}</th>
              <th className="px-4 py-2 text-left font-semibold">{t("col.department")}</th>
              <th className="px-4 py-2 text-left font-semibold">{t("col.activeToday")}</th>
              <th className="px-4 py-2 text-left font-semibold">{t("col.topApps")}</th>
              <th className="px-4 py-2 text-right font-semibold">{t("col.screenshots")}</th>
              <th className="px-4 py-2 text-right font-semibold">{t("col.score")}</th>
              <th className="px-4 py-2 text-center font-semibold">{t("col.status")}</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {filtered.map((r) => (
              <EmployeeRow key={r.user_id} row={r} locale={locale} />
            ))}
            {filtered.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-12 text-center text-muted-foreground">
                  {rows.length === 0 ? t("empty.noData") : t("empty.noMatch")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {initialOverview?.day && (
        <p className="text-xs text-muted-foreground text-right">
          {t("overview.day", { day: initialOverview.day })}
        </p>
      )}
    </div>
  );
}

function EmployeeRow({
  row,
  locale,
}: {
  row: EmployeeOverviewRow;
  locale: string;
}): JSX.Element {
  const activeH = formatHours(row.today.active_minutes);
  const idleH = formatHours(row.today.idle_minutes);
  const activePct = clampPct(
    row.today.active_minutes /
      Math.max(1, row.today.active_minutes + row.today.idle_minutes),
  );
  const initials = (row.username || row.email || "?")
    .slice(0, 2)
    .toLocaleUpperCase("tr");

  return (
    <tr
      className="hover:bg-muted/40 cursor-pointer transition-colors"
      onClick={() => {
        window.location.href = `/${locale}/employees/${row.user_id}`;
      }}
    >
      <td className="px-4 py-3">
        <Link
          href={`/${locale}/employees/${row.user_id}`}
          onClick={(e) => e.stopPropagation()}
          className="flex items-center gap-3"
        >
          <div
            className={`h-8 w-8 rounded-full flex items-center justify-center text-xs font-bold ${
              row.is_currently_active
                ? "bg-green-500/20 text-green-900"
                : "bg-muted text-muted-foreground"
            }`}
          >
            {initials}
          </div>
          <div className="min-w-0">
            <div className="font-semibold truncate">{row.username}</div>
            <div className="text-xs text-muted-foreground truncate">{row.email}</div>
          </div>
        </Link>
      </td>
      <td className="px-4 py-3 text-muted-foreground">
        <div>{row.department || "—"}</div>
        <div className="text-xs">{row.job_title || ""}</div>
      </td>
      <td className="px-4 py-3 min-w-[180px]">
        <div className="flex items-center gap-2">
          <div className="flex-1 h-2 rounded-full bg-muted overflow-hidden">
            <div
              className="h-full bg-green-500"
              style={{ width: `${activePct}%` }}
              aria-label={`${activePct}% aktif`}
            />
          </div>
          <span className="text-xs tabular-nums whitespace-nowrap">
            {activeH} / {idleH}
          </span>
        </div>
      </td>
      <td className="px-4 py-3">
        <div className="flex flex-wrap gap-1">
          {row.today.top_apps.slice(0, 3).map((app) => (
            <AppChip key={app.name} app={app} />
          ))}
          {row.today.top_apps.length === 0 && (
            <span className="text-xs text-muted-foreground">—</span>
          )}
        </div>
      </td>
      <td className="px-4 py-3 text-right tabular-nums">
        <span className="inline-flex items-center gap-1">
          <Camera className="h-3 w-3 text-muted-foreground" />
          {row.today.screenshot_count}
        </span>
      </td>
      <td className="px-4 py-3 text-right">
        <ScorePill score={row.today.productivity_score} />
      </td>
      <td className="px-4 py-3 text-center">
        {row.is_currently_active ? (
          <span className="inline-flex items-center gap-1 text-xs text-green-700">
            <span className="h-2 w-2 rounded-full bg-green-500 animate-pulse" />
            Aktif
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">Çevrimdışı</span>
        )}
      </td>
    </tr>
  );
}

function AppChip({ app }: { app: TopApp }): JSX.Element {
  const tone =
    app.category === "productive"
      ? "bg-green-500/15 text-green-800 border-green-500/30"
      : app.category === "distracting"
        ? "bg-red-500/15 text-red-800 border-red-500/30"
        : "bg-muted text-foreground border-muted-foreground/20";
  return (
    <span
      className={`inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-xs ${tone}`}
      title={`${app.name} — ${formatHours(app.minutes)}`}
    >
      <span className="truncate max-w-[120px]">{app.name}</span>
      <span className="tabular-nums opacity-70">{formatHours(app.minutes)}</span>
    </span>
  );
}

function ScorePill({ score }: { score: number }): JSX.Element {
  const tone =
    score >= 70
      ? "bg-green-500/15 text-green-800 border-green-500/30"
      : score >= 50
        ? "bg-amber-500/15 text-amber-800 border-amber-500/30"
        : "bg-red-500/15 text-red-800 border-red-500/30";
  return (
    <span
      className={`inline-block rounded border px-2 py-0.5 text-xs font-semibold tabular-nums ${tone}`}
    >
      {score}
    </span>
  );
}

function StatCard({
  label,
  value,
  icon,
  tone,
}: {
  label: string;
  value: string;
  icon: React.ReactNode;
  tone?: "success" | "warn" | "danger";
}): JSX.Element {
  const toneClass =
    tone === "success"
      ? "border-green-500/30 bg-green-500/5"
      : tone === "warn"
        ? "border-amber-500/30 bg-amber-500/5"
        : tone === "danger"
          ? "border-red-500/30 bg-red-500/5"
          : "";
  return (
    <div className={`rounded-md border p-3 ${toneClass}`}>
      <div className="flex items-center gap-2 text-xs text-muted-foreground uppercase tracking-wide">
        {icon}
        {label}
      </div>
      <div className="mt-1 text-xl font-bold tabular-nums">{value}</div>
    </div>
  );
}

function formatHours(minutes: number): string {
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  if (h === 0) return `${m}dk`;
  if (m === 0) return `${h}s`;
  return `${h}s ${m}dk`;
}

function clampPct(r: number): number {
  return Math.min(100, Math.max(0, Math.round(r * 100)));
}
