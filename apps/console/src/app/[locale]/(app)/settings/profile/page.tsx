import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { formatDateTR } from "@/lib/utils";
import { RoleBadge } from "@/components/layout/role-badge";

interface ProfileSettingsPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("settings");
  return { title: t("profile.title") };
}

export default async function ProfileSettingsPage({
  params,
}: ProfileSettingsPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user) {
    redirect(`/${locale}/login`);
  }

  const t = await getTranslations("settings");
  const { user } = session;

  return (
    <div className="space-y-6 max-w-xl animate-fade-in">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("profile.title")}</h1>
        <p className="text-muted-foreground">{t("profile.subtitle")}</p>
      </div>

      <div className="rounded-lg border bg-card p-6 space-y-4">
        <dl className="grid grid-cols-2 gap-4 text-sm">
          <div>
            <dt className="text-xs text-muted-foreground">{t("profile.username")}</dt>
            <dd className="font-medium">{user.username}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">{t("profile.email")}</dt>
            <dd className="font-medium">{user.email}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">{t("profile.role")}</dt>
            <dd>
              <RoleBadge role={user.role} />
            </dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">{t("profile.userId")}</dt>
            <dd className="font-mono text-xs">{user.id}</dd>
          </div>
        </dl>
        <p className="text-xs text-muted-foreground">{t("profile.keycloakNote")}</p>
      </div>
    </div>
  );
}
