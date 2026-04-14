import { getTranslations } from "next-intl/server";
import { redirect } from "next/navigation";
import { getSession } from "@/lib/auth/session";
import { can } from "@/lib/auth/rbac";
import { ExternalIntegrationsClient } from "./external-integrations-client";

interface IntegrationsSettingsPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("settings.integrations");
  return { title: t("title") };
}

/**
 * External integrations settings — the authoritative UI for configuring
 * MaxMind, Cloudflare, PagerDuty, Slack, and Sentry credentials.
 *
 * Replaces the Phase 2 HRIS/SIEM scaffold; those still live under
 * /settings/integrations but the actual HRIS/SIEM wiring is owned by
 * Phase 2.11 (real adapters) — once that lands, a second tab on this
 * page will surface them alongside the external services.
 */
export default async function IntegrationsSettingsPage({
  params,
}: IntegrationsSettingsPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:settings")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("settings.integrations");

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold tracking-tight">{t("title")}</h2>
        <p className="text-sm text-muted-foreground">{t("subtitle")}</p>
      </div>

      <ExternalIntegrationsClient token={session.user.access_token} />
    </div>
  );
}
