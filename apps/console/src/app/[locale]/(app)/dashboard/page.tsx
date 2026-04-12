import { getTranslations } from "next-intl/server";
import { Suspense } from "react";
import { getSession } from "@/lib/auth/session";
import { listEndpoints } from "@/lib/api/endpoints";
import { listDSRs } from "@/lib/api/dsr";
import { listLiveViewRequests } from "@/lib/api/liveview";
import { listAuditRecords } from "@/lib/api/audit";
import { getDLPState } from "@/lib/api/dlp-state";
import { getEmployeesOverview } from "@/lib/api/employees";
import type { EmployeeOverviewRow } from "@/lib/api/employees";
import { DashboardClient } from "./dashboard-client";
import { Skeleton } from "@/components/ui/skeleton";

export async function generateMetadata() {
  const t = await getTranslations("dashboard");
  return { title: t("title") };
}

async function fetchDashboardData(accessToken: string) {
  // Forward the DPO/admin access token explicitly to every API call —
  // server-side fetch from Next.js doesn't auto-forward cookies, so
  // the apiClient needs the token as an explicit option. See the
  // evidence page for the same pattern.
  const opts = { token: accessToken };

  const [endpoints, dsrOpen, dsrAtRisk, dsrOverdue, liveViewPending, recentAudit, dlpState, employeesOverview] =
    await Promise.allSettled([
      listEndpoints({ status: "active", page_size: 1 }, opts),
      listDSRs({ state: "open", page_size: 1 }, opts),
      listDSRs({ state: "at_risk", page_size: 1 }, opts),
      listDSRs({ state: "overdue", page_size: 1 }, opts),
      listLiveViewRequests({ state: "REQUESTED", page_size: 10 }, opts),
      listAuditRecords({ page_size: 5 }, opts),
      getDLPState(),
      getEmployeesOverview(undefined, opts),
    ]);

  const rows: EmployeeOverviewRow[] =
    employeesOverview.status === "fulfilled" ? employeesOverview.value.items : [];

  const uam = buildUAMWidgets(rows);

  return {
    activeEndpointsTotal: endpoints.status === "fulfilled" ? endpoints.value.pagination.total : 0,
    openDSRs: dsrOpen.status === "fulfilled" ? dsrOpen.value.pagination.total : 0,
    atRiskDSRs: dsrAtRisk.status === "fulfilled" ? dsrAtRisk.value.pagination.total : 0,
    overdueDSRs: dsrOverdue.status === "fulfilled" ? dsrOverdue.value.pagination.total : 0,
    pendingLiveViews: liveViewPending.status === "fulfilled" ? liveViewPending.value.pagination.total : 0,
    pendingLiveViewItems: liveViewPending.status === "fulfilled" ? liveViewPending.value.items : [],
    recentAuditItems: recentAudit.status === "fulfilled" ? recentAudit.value.items : [],
    dlpState: dlpState.status === "fulfilled" ? dlpState.value : null,
    uam,
  };
}

export interface UAMWidgets {
  currentlyActive: number;
  totalEmployees: number;
  topApps: { name: string; minutes: number; category: string }[];
  mostIdle: {
    user_id: string;
    username: string;
    department: string;
    idle_minutes: number;
    active_minutes: number;
  }[];
}

function buildUAMWidgets(rows: EmployeeOverviewRow[]): UAMWidgets {
  const currentlyActive = rows.filter((r) => r.is_currently_active).length;

  // Aggregate top apps across everyone's daily top_apps. Same name + same
  // category wins; ties broken by alphabetical order for stability.
  const appMap = new Map<string, { name: string; minutes: number; category: string }>();
  for (const r of rows) {
    for (const app of r.today?.top_apps ?? []) {
      const key = app.name;
      const cur = appMap.get(key);
      if (cur) {
        cur.minutes += app.minutes;
      } else {
        appMap.set(key, { name: app.name, minutes: app.minutes, category: app.category });
      }
    }
  }
  const topApps = Array.from(appMap.values())
    .sort((a, b) => b.minutes - a.minutes || a.name.localeCompare(b.name))
    .slice(0, 5);

  const mostIdle = [...rows]
    .filter((r) => r.today.idle_minutes > 0)
    .sort((a, b) => b.today.idle_minutes - a.today.idle_minutes)
    .slice(0, 5)
    .map((r) => ({
      user_id: r.user_id,
      username: r.username,
      department: r.department,
      idle_minutes: r.today.idle_minutes,
      active_minutes: r.today.active_minutes,
    }));

  return {
    currentlyActive,
    totalEmployees: rows.length,
    topApps,
    mostIdle,
  };
}

export default async function DashboardPage(): Promise<JSX.Element> {
  const t = await getTranslations("dashboard");
  const session = await getSession();

  const data = await fetchDashboardData(session?.user.access_token ?? "").catch(() => ({
    activeEndpointsTotal: 0,
    openDSRs: 0,
    atRiskDSRs: 0,
    overdueDSRs: 0,
    pendingLiveViews: 0,
    pendingLiveViewItems: [],
    recentAuditItems: [],
    dlpState: null,
    uam: {
      currentlyActive: 0,
      totalEmployees: 0,
      topApps: [],
      mostIdle: [],
    },
  }));

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Page header */}
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
        <p className="text-muted-foreground">{t("subtitle")}</p>
      </div>

      <Suspense fallback={<DashboardSkeleton />}>
        <DashboardClient
          activeEndpointsTotal={data.activeEndpointsTotal}
          openDSRs={data.openDSRs}
          atRiskDSRs={data.atRiskDSRs}
          overdueDSRs={data.overdueDSRs}
          pendingLiveViews={data.pendingLiveViews}
          recentAuditItems={data.recentAuditItems}
          dlpState={data.dlpState}
          uam={data.uam}
          userRole={session?.user.role ?? "admin"}
        />
      </Suspense>
    </div>
  );
}

function DashboardSkeleton(): JSX.Element {
  return (
    <div className="space-y-6">
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-28 w-full rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-16 w-full rounded-lg" />
      <Skeleton className="h-64 w-full rounded-lg" />
    </div>
  );
}
