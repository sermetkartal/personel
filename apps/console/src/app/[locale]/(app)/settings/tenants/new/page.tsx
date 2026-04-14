import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { NewTenantForm } from "./new-tenant-form";

interface NewTenantPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("settings.tenants");
  return { title: t("create") };
}

export default async function NewTenantPage({
  params,
}: NewTenantPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "manage:tenants")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("settings.tenants");

  return (
    <div className="space-y-6 max-w-2xl animate-fade-in">
      <div>
        <Link
          href={`/${locale}/settings/tenants`}
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden="true" />
          {t("backToList")}
        </Link>
        <h1 className="mt-2 text-2xl font-bold tracking-tight">{t("create")}</h1>
        <p className="text-muted-foreground">{t("createSubtitle")}</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("profileTitle")}</CardTitle>
        </CardHeader>
        <CardContent>
          <NewTenantForm locale={locale} />
        </CardContent>
      </Card>
    </div>
  );
}
