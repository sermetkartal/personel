"use client";

import Link from "next/link";
import { useTranslations } from "next-intl";
import {
  ArrowLeft,
  Activity,
  Clock,
  Camera,
  Keyboard,
  Monitor,
  Video,
  TrendingUp,
  Calendar,
} from "lucide-react";
import type {
  EmployeeDetail,
  HourlyBucket,
  TopApp,
  DailyStatsCompact,
} from "@/lib/api/employees";

interface Props {
  detail: EmployeeDetail;
  locale: string;
}

function formatHours(mins: number): string {
  const h = Math.floor(mins / 60);
  const m = mins % 60;
  if (h === 0) return `${m} dk`;
  if (m === 0) return `${h} sa`;
  return `${h} sa ${m} dk`;
}

function scoreClass(score: number): string {
  if (score >= 75) return "bg-green-500/15 text-green-700 border-green-500/30";
  if (score >= 55) return "bg-blue-500/15 text-blue-700 border-blue-500/30";
  if (score >= 35) return "bg-amber-500/15 text-amber-700 border-amber-500/30";
  return "bg-red-500/15 text-red-700 border-red-500/30";
}

function categoryClass(cat: TopApp["category"]): string {
  switch (cat) {
    case "productive":
      return "bg-green-500/15 text-green-800 border-green-500/30";
    case "distracting":
      return "bg-red-500/15 text-red-800 border-red-500/30";
    default:
      return "bg-muted text-muted-foreground border-border";
  }
}

export function EmployeeDetailClient({ detail, locale }: Props): JSX.Element {
  const t = useTranslations("employees");
  const { profile, today, hourly, last_7_days, assigned_endpoints } = detail;

  const initials = (profile.username || profile.email || "?")
    .slice(0, 2)
    .toLocaleUpperCase("tr");

  const maxHourActivity = Math.max(
    1,
    ...hourly.map((h) => h.active_minutes + h.idle_minutes),
  );

  // The `today.top_apps` array already contains the daily top-N. Sort by
  // minutes descending, keep at most 10.
  const topApps = [...(today.top_apps ?? [])]
    .sort((a, b) => b.minutes - a.minutes)
    .slice(0, 10);

  const maxAppMinutes = Math.max(1, ...topApps.map((a) => a.minutes));

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Back link */}
      <Link
        href={`/${locale}/employees`}
        className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
      >
        <ArrowLeft className="h-4 w-4" />
        {t("detail.backToList")}
      </Link>

      {/* Profile header */}
      <div className="rounded-xl border bg-card p-6">
        <div className="flex items-start justify-between gap-4 flex-wrap">
          <div className="flex items-center gap-4">
            <div
              className={`h-16 w-16 rounded-full flex items-center justify-center text-xl font-bold ${
                detail.is_currently_active
                  ? "bg-green-500/20 text-green-900"
                  : "bg-muted text-muted-foreground"
              }`}
            >
              {initials}
            </div>
            <div>
              <h1 className="text-2xl font-bold tracking-tight">
                {profile.username}
              </h1>
              <div className="text-sm text-muted-foreground">
                {profile.email}
              </div>
              <div className="flex items-center gap-2 mt-2 text-sm">
                <span className="font-medium">{profile.department || "—"}</span>
                {profile.job_title && (
                  <>
                    <span className="text-muted-foreground">·</span>
                    <span className="text-muted-foreground">
                      {profile.job_title}
                    </span>
                  </>
                )}
              </div>
            </div>
          </div>

          <div className="flex items-center gap-2">
            {detail.is_currently_active && (
              <span className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-green-500/15 text-green-700 border border-green-500/30 text-sm">
                <span className="h-2 w-2 rounded-full bg-green-500 animate-pulse" />
                {t("detail.currentlyActive")}
              </span>
            )}
            {assigned_endpoints[0] && (
              <Link
                href={`/${locale}/live-view/request?endpoint=${assigned_endpoints[0].endpoint_id}`}
                className="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-primary text-primary-foreground text-sm font-medium hover:bg-primary/90 transition-colors"
              >
                <Video className="h-4 w-4" />
                {t("detail.watchLive")}
              </Link>
            )}
          </div>
        </div>
      </div>

      {/* KPI cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <KpiCard
          icon={<Activity className="h-5 w-5" />}
          label={t("detail.kpi.activeToday")}
          value={formatHours(today.active_minutes)}
          accent="green"
        />
        <KpiCard
          icon={<Clock className="h-5 w-5" />}
          label={t("detail.kpi.idleToday")}
          value={formatHours(today.idle_minutes)}
          accent="amber"
        />
        <KpiCard
          icon={<Camera className="h-5 w-5" />}
          label={t("detail.kpi.screenshots")}
          value={today.screenshot_count.toString()}
          accent="blue"
        />
        <KpiCard
          icon={<TrendingUp className="h-5 w-5" />}
          label={t("detail.kpi.productivityScore")}
          value={`${today.productivity_score}`}
          suffix="/100"
          accent={
            today.productivity_score >= 75
              ? "green"
              : today.productivity_score >= 55
                ? "blue"
                : today.productivity_score >= 35
                  ? "amber"
                  : "red"
          }
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Hourly activity chart — 2/3 width */}
        <div className="lg:col-span-2 rounded-xl border bg-card p-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold">{t("detail.hourlyTitle")}</h2>
            <span className="text-xs text-muted-foreground">
              {t("detail.hourlySubtitle")}
            </span>
          </div>
          <HourlyChart hourly={hourly} maxTotal={maxHourActivity} />
        </div>

        {/* Top apps — 1/3 width */}
        <div className="rounded-xl border bg-card p-6">
          <h2 className="text-lg font-semibold mb-4">
            {t("detail.topAppsTitle")}
          </h2>
          <div className="space-y-3">
            {topApps.length === 0 && (
              <div className="text-sm text-muted-foreground">
                {t("detail.noData")}
              </div>
            )}
            {topApps.map((app) => (
              <div key={app.name}>
                <div className="flex items-center justify-between mb-1">
                  <span
                    className={`text-xs px-1.5 py-0.5 rounded border ${categoryClass(app.category)}`}
                  >
                    {app.name}
                  </span>
                  <span className="text-xs tabular-nums text-muted-foreground">
                    {formatHours(app.minutes)}
                  </span>
                </div>
                <div className="h-1.5 rounded-full bg-muted overflow-hidden">
                  <div
                    className={
                      app.category === "productive"
                        ? "h-full bg-green-500"
                        : app.category === "distracting"
                          ? "h-full bg-red-500"
                          : "h-full bg-muted-foreground/50"
                    }
                    style={{
                      width: `${Math.round((app.minutes / maxAppMinutes) * 100)}%`,
                    }}
                  />
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* Last 7 days + assigned endpoints */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="rounded-xl border bg-card p-6">
          <h2 className="text-lg font-semibold mb-4 flex items-center gap-2">
            <Calendar className="h-5 w-5 text-muted-foreground" />
            {t("detail.last7DaysTitle")}
          </h2>
          <Last7Days days={last_7_days} />
        </div>

        <div className="rounded-xl border bg-card p-6">
          <h2 className="text-lg font-semibold mb-4 flex items-center gap-2">
            <Monitor className="h-5 w-5 text-muted-foreground" />
            {t("detail.endpointsTitle")}
          </h2>
          {assigned_endpoints.length === 0 ? (
            <div className="text-sm text-muted-foreground">
              {t("detail.noEndpoints")}
            </div>
          ) : (
            <ul className="space-y-2">
              {assigned_endpoints.map((ep) => (
                <li
                  key={ep.endpoint_id}
                  className="flex items-center justify-between p-3 rounded-lg border bg-background"
                >
                  <div>
                    <div className="font-medium">{ep.hostname}</div>
                    <div className="text-xs text-muted-foreground font-mono">
                      {ep.endpoint_id.slice(0, 8)}…
                    </div>
                  </div>
                  <span
                    className={`inline-flex items-center gap-1.5 text-xs ${
                      ep.is_online ? "text-green-700" : "text-muted-foreground"
                    }`}
                  >
                    <span
                      className={`h-2 w-2 rounded-full ${
                        ep.is_online ? "bg-green-500 animate-pulse" : "bg-muted-foreground"
                      }`}
                    />
                    {ep.is_online ? t("detail.online") : t("detail.offline")}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}

function KpiCard({
  icon,
  label,
  value,
  suffix,
  accent,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  suffix?: string;
  accent: "green" | "blue" | "amber" | "red";
}): JSX.Element {
  const accentClass = {
    green: "text-green-600",
    blue: "text-blue-600",
    amber: "text-amber-600",
    red: "text-red-600",
  }[accent];

  return (
    <div className="rounded-xl border bg-card p-4">
      <div className={`flex items-center gap-2 ${accentClass}`}>
        {icon}
        <span className="text-xs uppercase tracking-wide">{label}</span>
      </div>
      <div className="mt-2 text-2xl font-bold tabular-nums">
        {value}
        {suffix && (
          <span className="text-sm font-normal text-muted-foreground ml-1">
            {suffix}
          </span>
        )}
      </div>
    </div>
  );
}

function HourlyChart({
  hourly,
  maxTotal,
}: {
  hourly: HourlyBucket[];
  maxTotal: number;
}): JSX.Element {
  return (
    <div>
      <div className="flex items-end gap-1 h-40">
        {hourly.map((h) => {
          const total = h.active_minutes + h.idle_minutes;
          const activePct = total === 0 ? 0 : (h.active_minutes / 60) * 100;
          const idlePct = total === 0 ? 0 : (h.idle_minutes / 60) * 100;
          const isBusinessHour = h.hour >= 9 && h.hour <= 17;
          return (
            <div
              key={h.hour}
              className="flex-1 flex flex-col justify-end group relative"
              title={`${String(h.hour).padStart(2, "0")}:00 — ${h.active_minutes}dk aktif / ${h.idle_minutes}dk boşta${h.top_app ? ` · ${h.top_app}` : ""}`}
            >
              <div
                className="w-full bg-amber-400 transition-opacity group-hover:opacity-80"
                style={{ height: `${idlePct}%` }}
              />
              <div
                className="w-full bg-green-500 transition-opacity group-hover:opacity-80"
                style={{ height: `${activePct}%` }}
              />
              {!isBusinessHour && total === 0 && (
                <div className="h-px bg-border" />
              )}
            </div>
          );
        })}
      </div>
      {/* Hour axis */}
      <div className="flex gap-1 mt-2 text-[10px] text-muted-foreground tabular-nums">
        {hourly.map((h) => (
          <div
            key={h.hour}
            className="flex-1 text-center"
          >
            {h.hour % 3 === 0 ? String(h.hour).padStart(2, "0") : ""}
          </div>
        ))}
      </div>
      {/* Legend */}
      <div className="flex items-center gap-4 mt-3 text-xs">
        <span className="inline-flex items-center gap-1.5">
          <span className="h-3 w-3 rounded bg-green-500" />
          <span className="text-muted-foreground">Aktif</span>
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="h-3 w-3 rounded bg-amber-400" />
          <span className="text-muted-foreground">Boşta</span>
        </span>
      </div>
    </div>
  );
}

function Last7Days({ days }: { days: DailyStatsCompact[] }): JSX.Element {
  if (days.length === 0) {
    return <div className="text-sm text-muted-foreground">Veri yok</div>;
  }
  const maxActive = Math.max(1, ...days.map((d) => d.active_minutes));
  return (
    <div className="space-y-2">
      {days.map((d) => {
        const pct = Math.round((d.active_minutes / maxActive) * 100);
        return (
          <div key={d.day} className="flex items-center gap-3">
            <div className="text-xs text-muted-foreground w-20 tabular-nums">
              {d.day}
            </div>
            <div className="flex-1 h-2 rounded-full bg-muted overflow-hidden">
              <div
                className="h-full bg-green-500"
                style={{ width: `${pct}%` }}
              />
            </div>
            <div className="text-xs tabular-nums w-16 text-right">
              {formatHours(d.active_minutes)}
            </div>
            <div
              className={`text-xs tabular-nums w-10 text-right px-1 rounded ${scoreClass(d.productivity_score)}`}
            >
              {d.productivity_score}
            </div>
          </div>
        );
      })}
    </div>
  );
}
