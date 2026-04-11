import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { Construction } from "lucide-react";

interface IntegrationsSettingsPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("settings");
  return { title: t("integrations.title") };
}

export default async function IntegrationsSettingsPage({
  params,
}: IntegrationsSettingsPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:settings")) {
    redirect(`/${locale}/unauthorized`);
  }

  return (
    <div className="space-y-6 max-w-3xl animate-fade-in">
      <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-20 text-center">
        <Construction className="mb-3 h-10 w-10 text-muted-foreground/40" aria-hidden="true" />
        <p className="text-muted-foreground text-sm">Phase 2 — Sentry/OTEL/SMTP integrations</p>
      </div>
    </div>
  );
}
