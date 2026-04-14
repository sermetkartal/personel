import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { listDSRs } from "@/lib/api/dsr";
import { DSRDashboardClient } from "./dsr-dashboard-client";
import { can } from "@/lib/auth/rbac";

interface DSRPageProps {
  searchParams: Promise<{ state?: string; page?: string }>;
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("dsr");
  return { title: t("title") };
}

export default async function DSRPage({
  searchParams,
  params,
}: DSRPageProps): Promise<JSX.Element> {
  const t = await getTranslations("dsr");
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "manage:dsr")) {
    redirect(`/${locale}/unauthorized`);
  }

  const sp = await searchParams;
  const page = sp.page ? parseInt(sp.page, 10) : 1;

  const opts = { token: session.user.access_token };
  const [openDSRs, atRiskDSRs, overdueDSRs, allDSRs] = await Promise.allSettled([
    listDSRs({ state: "open", page_size: 1 }, opts),
    listDSRs({ state: "at_risk", page_size: 1 }, opts),
    listDSRs({ state: "overdue", page_size: 1 }, opts),
    listDSRs(
      {
        state: sp.state as "open" | "at_risk" | "overdue" | "resolved" | "rejected" | undefined,
        page,
        page_size: 20,
      },
      opts,
    ),
  ]);

  return (
    <div className="space-y-6 animate-fade-in">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
        <p className="text-muted-foreground">{t("subtitle")}</p>
      </div>

      <DSRDashboardClient
        openCount={openDSRs.status === "fulfilled" ? openDSRs.value.pagination.total : 0}
        atRiskCount={atRiskDSRs.status === "fulfilled" ? atRiskDSRs.value.pagination.total : 0}
        overdueCount={overdueDSRs.status === "fulfilled" ? overdueDSRs.value.pagination.total : 0}
        initialList={allDSRs.status === "fulfilled" ? allDSRs.value : { items: [], pagination: { page: 1, page_size: 20, total: 0 } }}
        currentState={sp.state}
        currentPage={page}
      />
    </div>
  );
}
