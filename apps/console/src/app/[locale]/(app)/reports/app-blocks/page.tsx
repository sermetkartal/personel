import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { getAppBlocksPreview, ReportFetchError } from "@/lib/api/reports";
import { ReportError } from "../_components/report-error";
import { ShieldOff, Info } from "lucide-react";

interface PageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("reports");
  return { title: t("appBlocks.title") };
}

export default async function AppBlocksReportPage({
  params,
}: PageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();
  if (!session?.user || !can(session.user.role, "view:reports")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("reports");

  let data: Awaited<ReturnType<typeof getAppBlocksPreview>> | null = null;
  let fetchError: ReportFetchError | null = null;
  try {
    data = await getAppBlocksPreview({}, { token: session.user.access_token });
  } catch (e) {
    if (e instanceof ReportFetchError) {
      fetchError = e;
    } else {
      throw e;
    }
  }

  if (fetchError) {
    return (
      <div className="space-y-6 animate-fade-in">
        <div className="flex items-center gap-3">
          <ShieldOff className="h-6 w-6 text-muted-foreground" />
          <div>
            <h1 className="text-2xl font-bold tracking-tight">
              {t("appBlocks.title")}
            </h1>
            <p className="text-muted-foreground">{t("appBlocks.description")}</p>
          </div>
        </div>
        <ReportError status={fetchError.status} code={fetchError.code} />
      </div>
    );
  }

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center gap-3">
        <ShieldOff className="h-6 w-6 text-muted-foreground" />
        <div>
          <h1 className="text-2xl font-bold tracking-tight">
            {t("appBlocks.title")}
          </h1>
          <p className="text-muted-foreground">{t("appBlocks.description")}</p>
        </div>
      </div>

      {data?.notice_code && (
        <div className="flex items-start gap-3 rounded-xl border border-blue-500/30 bg-blue-500/5 p-4">
          <Info className="h-5 w-5 text-blue-600 mt-0.5" />
          <div className="text-sm">
            <div className="font-medium">{t("appBlocks.noticeTitle")}</div>
            <p className="text-muted-foreground mt-1">{data.notice_hint}</p>
          </div>
        </div>
      )}

      <div className="rounded-xl border bg-card">
        <table className="w-full text-sm">
          <thead className="text-left">
            <tr className="border-b">
              <th className="px-4 py-3 font-medium">
                {t("appBlocks.col.occurredAt")}
              </th>
              <th className="px-4 py-3 font-medium">
                {t("appBlocks.col.appName")}
              </th>
              <th className="px-4 py-3 font-medium text-right">
                {t("appBlocks.col.count")}
              </th>
            </tr>
          </thead>
          <tbody>
            {!data || data.items.length === 0 ? (
              <tr>
                <td
                  colSpan={3}
                  className="px-4 py-12 text-center text-sm text-muted-foreground"
                >
                  {t("appBlocks.noEvents")}
                </td>
              </tr>
            ) : (
              data.items.map((row, i) => (
                <tr key={i} className="border-b last:border-0">
                  <td className="px-4 py-3 tabular-nums">{row.occurred_at}</td>
                  <td className="px-4 py-3">{row.app_name}</td>
                  <td className="px-4 py-3 text-right tabular-nums">
                    {row.count}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
