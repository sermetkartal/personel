import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { UsersClient } from "./users-client";

interface UsersSettingsPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("settings.users");
  return { title: t("title") };
}

export default async function UsersSettingsPage({
  params,
}: UsersSettingsPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "manage:users")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("settings.users");

  return (
    <div className="space-y-6 animate-fade-in">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
        <p className="text-muted-foreground">{t("subtitle")}</p>
      </div>

      <UsersClient currentUserId={session.user.id} />
    </div>
  );
}
