import { getTranslations } from "next-intl/server";
import { redirect } from "next/navigation";
import { getSession } from "@/lib/auth/session";
import { can } from "@/lib/auth/rbac";
import { BackupClient } from "./backup-client";

interface Props {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("settings.backup");
  return { title: t("title") };
}

/**
 * Backup settings — in-site (local disk) and off-site (6 backends) targets.
 *
 * The page is entirely client-driven because every card is interactive:
 * a target can be run ad-hoc, its history expanded, or its config edited
 * without leaving the page. A server component would just re-fetch the
 * same data on every interaction, so we load it once on the client and
 * keep state in TanStack Query.
 */
export default async function BackupSettingsPage({
  params,
}: Props): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:settings")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("settings.backup");

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold tracking-tight">{t("title")}</h2>
        <p className="text-sm text-muted-foreground">{t("subtitle")}</p>
      </div>

      <BackupClient token={session.user.access_token} />
    </div>
  );
}
