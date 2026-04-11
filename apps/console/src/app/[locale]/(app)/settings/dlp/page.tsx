import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { DLPStatePanel } from "@/components/dlp/state-panel";

interface DLPSettingsPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("dlp");
  return { title: t("title") };
}

export default async function DLPSettingsPage({
  params,
}: DLPSettingsPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:dlp-settings")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("dlp");

  return (
    <div className="space-y-6 max-w-3xl animate-fade-in">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
        <p className="text-muted-foreground">{t("subtitle")}</p>
      </div>

      {/* DLP state panel — fully client-rendered for real-time polling */}
      <DLPStatePanel />
    </div>
  );
}
