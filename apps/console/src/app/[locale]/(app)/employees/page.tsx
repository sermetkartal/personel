import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { getEmployeesOverview } from "@/lib/api/employees";
import { EmployeesMonitoringClient } from "./monitoring-client";

interface EmployeesPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("employees");
  return { title: t("title") };
}

export default async function EmployeesPage({
  params,
}: EmployeesPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:employees")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("employees");

  // Fetch today's overview server-side so the page renders with data
  // already in place. Failure → empty state with diagnostic log.
  const overview = await getEmployeesOverview(undefined, {
    token: session.user.access_token,
  }).catch((err) => {
    console.error("[employees] overview fetch failed:", err);
    return null;
  });

  return (
    <div className="space-y-6 animate-fade-in">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
        <p className="text-muted-foreground">{t("subtitle")}</p>
      </div>

      <EmployeesMonitoringClient initialOverview={overview} />
    </div>
  );
}
