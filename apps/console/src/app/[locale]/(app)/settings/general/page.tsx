import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Image as ImageIcon } from "lucide-react";
import { GeneralSettingsForm } from "./general-form";

interface GeneralSettingsPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("settings.general");
  return { title: t("title") };
}

export default async function GeneralSettingsPage({
  params,
}: GeneralSettingsPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:settings")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("settings.general");

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold tracking-tight">{t("title")}</h2>
        <p className="text-sm text-muted-foreground">{t("subtitle")}</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("tenantSectionTitle")}</CardTitle>
        </CardHeader>
        <CardContent>
          <GeneralSettingsForm
            initialDisplayName={""}
            initialSlug={""}
            initialLocale={locale}
            initialTimezone="Europe/Istanbul"
            canEdit={can(session.user.role, "manage:tenants")}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0">
          <CardTitle className="text-base">{t("brandingTitle")}</CardTitle>
          <Badge variant="outline">{t("comingSoon")}</Badge>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12 text-center">
            <ImageIcon className="mb-3 h-10 w-10 text-muted-foreground/40" aria-hidden="true" />
            <p className="text-sm text-muted-foreground">{t("brandingHint")}</p>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
