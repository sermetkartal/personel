import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { getIdleActivePreview } from "@/lib/api/reports";
import { IdleActiveChart } from "./idle-active-chart";
import { ReportEmpty } from "../_components/report-empty";
import { Clock } from "lucide-react";

interface PageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("reports");
  return { title: t("idleActive.title") };
}

export default async function IdleActiveReportPage({
  params,
}: PageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();
  if (!session?.user || !can(session.user.role, "view:reports")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("reports");
  const data = await getIdleActivePreview(
    {},
    { token: session.user.access_token },
  ).catch(() => null);

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center gap-3">
        <Clock className="h-6 w-6 text-muted-foreground" />
        <div>
          <h1 className="text-2xl font-bold tracking-tight">
            {t("idleActive.title")}
          </h1>
          <p className="text-muted-foreground">{t("idleActive.description")}</p>
        </div>
      </div>

      {!data || data.items.length === 0 ? (
        <ReportEmpty titleKey="emptyTitle" descKey="emptyDesc" />
      ) : (
        <IdleActiveChart items={data.items} from={data.from} to={data.to} />
      )}
    </div>
  );
}
