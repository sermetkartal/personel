import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { getIdleActivePreview, ReportFetchError } from "@/lib/api/reports";
import { IdleActiveChart } from "./idle-active-chart";
import { ReportEmpty } from "../_components/report-empty";
import { ReportError } from "../_components/report-error";
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

  let data: Awaited<ReturnType<typeof getIdleActivePreview>> | null = null;
  let fetchError: ReportFetchError | null = null;
  try {
    data = await getIdleActivePreview({}, { token: session.user.access_token });
  } catch (e) {
    if (e instanceof ReportFetchError) {
      fetchError = e;
    } else {
      throw e;
    }
  }

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

      {fetchError ? (
        <ReportError status={fetchError.status} code={fetchError.code} />
      ) : !data || data.items.length === 0 ? (
        <ReportEmpty titleKey="emptyTitle" descKey="emptyDesc" />
      ) : (
        <IdleActiveChart items={data.items} from={data.from} to={data.to} />
      )}
    </div>
  );
}
