"use client";

import { useState } from "react";
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
  ChevronDown,
  ChevronRight,
  Globe,
  Mail,
  FolderOpen,
  Network,
  Usb,
  Bluetooth,
  Smartphone,
  PowerOff,
  Cpu,
  Printer,
  Clipboard,
  ShieldAlert,
  Lock,
  Eye,
  Ban,
} from "lucide-react";
import type {
  EmployeeDetail,
  HourlyBucket,
  TopApp,
  DailyStatsCompact,
  RichSignals,
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

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
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
  const [expandedApp, setExpandedApp] = useState<string | null>(null);

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
  const richSignals = today.rich_signals;

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
            {topApps.map((app) => {
              const isExpanded = expandedApp === app.name;
              const hasFiles = (app.files?.length ?? 0) > 0;
              return (
                <div key={app.name}>
                  <button
                    type="button"
                    onClick={() => hasFiles && setExpandedApp(isExpanded ? null : app.name)}
                    className={`w-full text-left ${hasFiles ? "cursor-pointer" : "cursor-default"}`}
                  >
                    <div className="flex items-center justify-between mb-1">
                      <span className="flex items-center gap-1.5">
                        {hasFiles && (
                          isExpanded ? (
                            <ChevronDown className="h-3 w-3 text-muted-foreground" />
                          ) : (
                            <ChevronRight className="h-3 w-3 text-muted-foreground" />
                          )
                        )}
                        <span
                          className={`text-xs px-1.5 py-0.5 rounded border ${categoryClass(app.category)}`}
                        >
                          {app.name}
                        </span>
                        {hasFiles && (
                          <span className="text-[10px] text-muted-foreground">
                            {app.files!.length} dosya
                          </span>
                        )}
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
                  </button>
                  {isExpanded && hasFiles && (
                    <div className="mt-2 ml-5 border-l-2 border-border pl-3 space-y-1.5 animate-fade-in">
                      {app.files!.map((f) => (
                        <div
                          key={f.path}
                          className="flex items-center justify-between text-xs gap-2"
                        >
                          <span
                            className="font-mono truncate text-muted-foreground"
                            title={f.path}
                          >
                            {f.path}
                          </span>
                          <span className="tabular-nums whitespace-nowrap text-foreground/80">
                            {formatHours(f.minutes)}
                          </span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      </div>

      {/* Rich monitoring signals — every collector category has a card here */}
      {richSignals && <RichSignalsGrid signals={richSignals} />}

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
      <div className="flex gap-1 h-40 items-stretch">
        {hourly.map((h) => {
          // Each column is its own hour; the bar fills a fraction of the
          // column where 60 minutes = 100% column height. Clamp
          // defensively — data should never exceed 60 but noise could.
          const activePct = Math.min(100, (h.active_minutes / 60) * 100);
          const idlePct = Math.min(
            Math.max(0, 100 - activePct),
            (h.idle_minutes / 60) * 100,
          );
          const total = h.active_minutes + h.idle_minutes;
          return (
            <div
              key={h.hour}
              className="flex-1 h-full flex flex-col justify-end group relative min-w-0"
              title={`${String(h.hour).padStart(2, "0")}:00 — ${h.active_minutes}dk aktif / ${h.idle_minutes}dk boşta${h.top_app ? ` · ${h.top_app}` : ""}`}
            >
              {total === 0 ? (
                <div className="h-px w-full bg-border/60" />
              ) : (
                <>
                  <div
                    className="w-full bg-amber-400 transition-opacity group-hover:opacity-80"
                    style={{ height: `${idlePct}%` }}
                  />
                  <div
                    className="w-full bg-green-500 transition-opacity group-hover:opacity-80"
                    style={{ height: `${activePct}%` }}
                  />
                </>
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

function RichSignalsGrid({ signals }: { signals: RichSignals }) {
  return (
    <div className="space-y-4">
      <h2 className="text-sm font-semibold text-foreground/80 uppercase tracking-wider">
        Zengin Sinyaller (tüm toplayıcılar)
      </h2>
      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
        {signals.browser && <BrowserCard data={signals.browser} />}
        {signals.email && <EmailCard data={signals.email} />}
        {signals.filesystem && <FilesystemCard data={signals.filesystem} />}
        {signals.network && <NetworkCard data={signals.network} />}
        {signals.usb && <UsbCard data={signals.usb} />}
        {signals.bluetooth && <BluetoothCard data={signals.bluetooth} />}
        {signals.mtp && <MtpCard data={signals.mtp} />}
        {signals.system && <SystemCard data={signals.system} />}
        {signals.device && <DeviceCard data={signals.device} />}
        {signals.print && <PrintCard data={signals.print} />}
        {signals.clipboard && <ClipboardCard data={signals.clipboard} />}
        {signals.keystroke && <KeystrokeCard data={signals.keystroke} />}
        {signals.liveview && <LiveViewCard data={signals.liveview} />}
        {signals.policy && <PolicyCard data={signals.policy} />}
        {signals.tamper && <TamperCard data={signals.tamper} />}
      </div>
    </div>
  );
}

function SignalCard({
  icon: Icon,
  title,
  subtitle,
  children,
  accent = "blue",
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  subtitle?: string;
  children: React.ReactNode;
  accent?: "blue" | "green" | "amber" | "red" | "purple" | "slate";
}) {
  const accentClass = {
    blue: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
    green: "bg-green-500/10 text-green-600 dark:text-green-400",
    amber: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
    red: "bg-red-500/10 text-red-600 dark:text-red-400",
    purple: "bg-purple-500/10 text-purple-600 dark:text-purple-400",
    slate: "bg-slate-500/10 text-slate-600 dark:text-slate-400",
  }[accent];
  return (
    <div className="rounded-lg border border-border bg-card p-4 space-y-3">
      <div className="flex items-center gap-2">
        <div className={`p-1.5 rounded ${accentClass}`}>
          <Icon className="h-4 w-4" />
        </div>
        <div className="flex-1 min-w-0">
          <div className="text-sm font-semibold">{title}</div>
          {subtitle && (
            <div className="text-xs text-muted-foreground truncate">
              {subtitle}
            </div>
          )}
        </div>
      </div>
      <div className="text-xs space-y-1.5">{children}</div>
    </div>
  );
}

function StatRow({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: React.ReactNode;
  mono?: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-2">
      <span className="text-muted-foreground truncate">{label}</span>
      <span
        className={`tabular-nums whitespace-nowrap font-medium ${mono ? "font-mono" : ""}`}
      >
        {value}
      </span>
    </div>
  );
}

function BrowserCard({ data }: { data: NonNullable<RichSignals["browser"]> }) {
  return (
    <SignalCard
      icon={Globe}
      title="Tarayıcı"
      subtitle={`${data.top_domains?.length ?? 0} domain`}
      accent="blue"
    >
      {data.top_domains?.slice(0, 6).map((d) => (
        <div
          key={d.domain}
          className="flex items-center justify-between gap-2"
        >
          <span className="font-mono truncate text-foreground/80">
            {d.domain}
          </span>
          <span className="tabular-nums whitespace-nowrap text-muted-foreground">
            {d.visits}× · {formatHours(d.minutes)}
          </span>
        </div>
      ))}
      {data.incognito_blocked !== undefined && data.incognito_blocked > 0 && (
        <div className="pt-1.5 border-t border-border">
          <StatRow
            label="Gizli mod engellendi"
            value={
              <span className="text-amber-600 dark:text-amber-400">
                {data.incognito_blocked}
              </span>
            }
          />
        </div>
      )}
    </SignalCard>
  );
}

function EmailCard({ data }: { data: NonNullable<RichSignals["email"]> }) {
  return (
    <SignalCard
      icon={Mail}
      title="E-posta"
      subtitle={`${data.sent} gönderildi · ${data.received} alındı`}
      accent="purple"
    >
      {data.top_correspondents?.slice(0, 5).map((c) => (
        <div
          key={c.address}
          className="flex items-center justify-between gap-2"
        >
          <span className="font-mono truncate text-foreground/80">
            {c.address}
          </span>
          <span className="tabular-nums text-muted-foreground">
            {c.count}
          </span>
        </div>
      ))}
      {data.redacted_subjects !== undefined && (
        <div className="pt-1.5 border-t border-border">
          <StatRow
            label="KVKK maskeleme"
            value={`${data.redacted_subjects} konu`}
          />
        </div>
      )}
    </SignalCard>
  );
}

function FilesystemCard({
  data,
}: {
  data: NonNullable<RichSignals["filesystem"]>;
}) {
  return (
    <SignalCard
      icon={FolderOpen}
      title="Dosya Sistemi"
      subtitle={`${data.created + data.written + data.deleted} olay`}
      accent="green"
    >
      <StatRow label="Oluşturuldu" value={data.created} />
      <StatRow label="Yazıldı" value={data.written} />
      <StatRow label="Silindi" value={data.deleted} />
      {data.sensitive_hashed !== undefined && (
        <StatRow
          label="Hassas (hash'li)"
          value={
            <span className="text-amber-600 dark:text-amber-400">
              {data.sensitive_hashed}
            </span>
          }
        />
      )}
      {data.top_paths && data.top_paths.length > 0 && (
        <div className="pt-1.5 border-t border-border space-y-1">
          {data.top_paths.slice(0, 3).map((p) => (
            <div
              key={p.path}
              className="flex items-center justify-between gap-2"
            >
              <span
                className="font-mono truncate text-foreground/70 text-[10px]"
                title={p.path}
              >
                {p.path}
              </span>
              <span className="tabular-nums text-muted-foreground text-[10px]">
                {p.events}
              </span>
            </div>
          ))}
        </div>
      )}
    </SignalCard>
  );
}

function NetworkCard({ data }: { data: NonNullable<RichSignals["network"]> }) {
  return (
    <SignalCard
      icon={Network}
      title="Ağ"
      subtitle={`${data.flows.toLocaleString("tr-TR")} akış`}
      accent="blue"
    >
      <StatRow label="DNS sorgusu" value={data.dns_queries.toLocaleString("tr-TR")} />
      {data.top_hosts?.slice(0, 4).map((h) => (
        <div key={h.host} className="flex items-center justify-between gap-2">
          <span className="font-mono truncate text-foreground/80">
            {h.host}
          </span>
          <span className="tabular-nums text-muted-foreground">
            {formatBytes(h.bytes)}
          </span>
        </div>
      ))}
      {data.geoip && data.geoip.length > 0 && (
        <div className="pt-1.5 border-t border-border">
          <div className="flex flex-wrap gap-1">
            {data.geoip.slice(0, 6).map((g) => (
              <span
                key={g.country}
                className="px-1.5 py-0.5 rounded bg-muted text-[10px]"
              >
                {g.country} · {g.ip_count}
              </span>
            ))}
          </div>
        </div>
      )}
    </SignalCard>
  );
}

function UsbCard({ data }: { data: NonNullable<RichSignals["usb"]> }) {
  return (
    <SignalCard
      icon={Usb}
      title="USB"
      subtitle={`${data.attached} takıldı · ${data.removed} çıkarıldı`}
      accent="amber"
    >
      {data.timeline?.slice(0, 5).map((t, idx) => (
        <div
          key={`${t.ts}-${idx}`}
          className="flex items-center justify-between gap-2"
        >
          <span className="truncate text-foreground/80">
            <span
              className={`inline-block w-1.5 h-1.5 rounded-full mr-1.5 ${
                t.event === "attached" ? "bg-green-500" : "bg-red-500"
              }`}
            />
            {t.vendor} {t.product}
          </span>
          <span className="tabular-nums text-muted-foreground text-[10px]">
            {new Date(t.ts).toLocaleTimeString("tr-TR", {
              hour: "2-digit",
              minute: "2-digit",
            })}
          </span>
        </div>
      ))}
    </SignalCard>
  );
}

function BluetoothCard({
  data,
}: {
  data: NonNullable<RichSignals["bluetooth"]>;
}) {
  return (
    <SignalCard
      icon={Bluetooth}
      title="Bluetooth"
      subtitle={`${data.paired_devices?.length ?? 0} eşleşmiş cihaz`}
      accent="blue"
    >
      {data.paired_devices?.slice(0, 6).map((d) => (
        <div key={d.name} className="flex items-center justify-between gap-2">
          <span className="truncate text-foreground/80">{d.name}</span>
          <span className="text-muted-foreground text-[10px]">{d.class}</span>
        </div>
      ))}
    </SignalCard>
  );
}

function MtpCard({ data }: { data: NonNullable<RichSignals["mtp"]> }) {
  return (
    <SignalCard
      icon={Smartphone}
      title="MTP / Taşınabilir"
      subtitle={`${data.devices?.length ?? 0} cihaz`}
      accent="amber"
    >
      {data.devices?.slice(0, 5).map((d) => (
        <div
          key={d.friendly_name}
          className="flex items-center justify-between gap-2"
        >
          <span className="truncate text-foreground/80">
            {d.friendly_name}
          </span>
          <span className="text-muted-foreground text-[10px]">
            {d.manufacturer}
          </span>
        </div>
      ))}
    </SignalCard>
  );
}

function SystemCard({ data }: { data: NonNullable<RichSignals["system"]> }) {
  return (
    <SignalCard
      icon={PowerOff}
      title="Sistem Olayları"
      subtitle={`${data.locks} kilit · ${data.unlocks} açma`}
      accent="slate"
    >
      <StatRow label="Kilit" value={data.locks} />
      <StatRow label="Açma" value={data.unlocks} />
      <StatRow label="Uyku" value={data.sleeps} />
      <StatRow label="Uyanma" value={data.wakes} />
      {data.av_deactivated !== undefined && data.av_deactivated > 0 && (
        <div className="pt-1.5 border-t border-border">
          <StatRow
            label="AV devre dışı"
            value={
              <span className="text-red-600 dark:text-red-400">
                {data.av_deactivated}
              </span>
            }
          />
        </div>
      )}
    </SignalCard>
  );
}

function DeviceCard({ data }: { data: NonNullable<RichSignals["device"]> }) {
  return (
    <SignalCard
      icon={Cpu}
      title="Cihaz Durumu"
      subtitle={`${data.uptime_hours.toFixed(1)} saat çalışma`}
      accent="green"
    >
      <StatRow label="CPU ortalaması" value={`${data.cpu_avg_percent.toFixed(1)}%`} />
      <StatRow label="RSS ortalaması" value={`${data.rss_avg_mb} MB`} />
      <StatRow
        label="Batarya"
        value={
          <span>
            {data.battery_percent}%
            {data.battery_charging && (
              <span className="ml-1 text-green-600 dark:text-green-400">
                ⚡
              </span>
            )}
          </span>
        }
      />
    </SignalCard>
  );
}

function PrintCard({ data }: { data: NonNullable<RichSignals["print"]> }) {
  return (
    <SignalCard
      icon={Printer}
      title="Yazdırma"
      subtitle={`${data.jobs} iş · ${data.pages} sayfa`}
      accent="purple"
    >
      {data.top_printers?.slice(0, 4).map((p) => (
        <div
          key={p.printer}
          className="flex items-center justify-between gap-2"
        >
          <span className="truncate text-foreground/80">{p.printer}</span>
          <span className="tabular-nums text-muted-foreground">
            {p.jobs}
          </span>
        </div>
      ))}
    </SignalCard>
  );
}

function ClipboardCard({
  data,
}: {
  data: NonNullable<RichSignals["clipboard"]>;
}) {
  return (
    <SignalCard
      icon={Clipboard}
      title="Pano"
      subtitle={`${data.metadata_events} olay`}
      accent="slate"
    >
      {data.redaction_hits && data.redaction_hits.length > 0 ? (
        data.redaction_hits.map((h) => (
          <StatRow
            key={h.rule}
            label={h.rule}
            value={
              <span className="text-amber-600 dark:text-amber-400">
                {h.count}
              </span>
            }
          />
        ))
      ) : (
        <div className="text-muted-foreground">Maskeleme yok</div>
      )}
    </SignalCard>
  );
}

function KeystrokeCard({
  data,
}: {
  data: NonNullable<RichSignals["keystroke"]>;
}) {
  return (
    <SignalCard
      icon={Lock}
      title="Klavye (şifreli)"
      subtitle={
        data.dlp_enabled ? "DLP aktif" : "ADR 0013: içerik şifreli · admin kör"
      }
      accent={data.dlp_enabled ? "amber" : "slate"}
    >
      <StatRow
        label="Toplam olay"
        value={data.total_events.toLocaleString("tr-TR")}
      />
      <StatRow label="Şifreli blob" value={data.encrypted_blobs} />
      <div className="pt-1.5 border-t border-border text-[10px] text-muted-foreground leading-tight">
        ADR 0013 uyarınca ham içerik yöneticiler tarafından kriptografik olarak
        okunamaz.
      </div>
    </SignalCard>
  );
}

function LiveViewCard({
  data,
}: {
  data: NonNullable<RichSignals["liveview"]>;
}) {
  return (
    <SignalCard
      icon={Eye}
      title="Canlı İzleme"
      subtitle={`${data.sessions} oturum`}
      accent="purple"
    >
      {data.last_request_at && (
        <StatRow
          label="Son talep"
          value={new Date(data.last_request_at).toLocaleString("tr-TR", {
            hour: "2-digit",
            minute: "2-digit",
            day: "2-digit",
            month: "2-digit",
          })}
        />
      )}
      {data.last_requested_by && (
        <StatRow label="Talep eden" value={data.last_requested_by} />
      )}
      <div className="pt-1.5 border-t border-border text-[10px] text-muted-foreground leading-tight">
        HR çift-kontrol + hash-zincirli audit
      </div>
    </SignalCard>
  );
}

function PolicyCard({ data }: { data: NonNullable<RichSignals["policy"]> }) {
  return (
    <SignalCard
      icon={Ban}
      title="Politika İhlalleri"
      subtitle={`${data.blocked_app_attempts + data.blocked_web_attempts} girişim`}
      accent="red"
    >
      <StatRow
        label="Engellenen uygulama"
        value={
          <span className="text-red-600 dark:text-red-400">
            {data.blocked_app_attempts}
          </span>
        }
      />
      <StatRow
        label="Engellenen web"
        value={
          <span className="text-red-600 dark:text-red-400">
            {data.blocked_web_attempts}
          </span>
        }
      />
    </SignalCard>
  );
}

function TamperCard({ data }: { data: NonNullable<RichSignals["tamper"]> }) {
  const healthy = data.findings === 0;
  return (
    <SignalCard
      icon={ShieldAlert}
      title="Anti-Tamper"
      subtitle={healthy ? "Sağlıklı" : `${data.findings} bulgu`}
      accent={healthy ? "green" : "red"}
    >
      <StatRow
        label="Bulgu"
        value={
          <span
            className={
              healthy
                ? "text-green-600 dark:text-green-400"
                : "text-red-600 dark:text-red-400"
            }
          >
            {data.findings}
          </span>
        }
      />
      {data.last_check && (
        <StatRow
          label="Son kontrol"
          value={new Date(data.last_check).toLocaleString("tr-TR", {
            hour: "2-digit",
            minute: "2-digit",
            day: "2-digit",
            month: "2-digit",
          })}
        />
      )}
    </SignalCard>
  );
}
