import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect, notFound } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import Link from "next/link";
import { getTenant } from "@/lib/api/settings";
import type { Tenant } from "@/lib/api/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  ArrowLeft,
  Building2,
  Shield,
  ShieldOff,
  KeySquare,
  Plug,
  AlertTriangle,
} from "lucide-react";
import { formatDateTR } from "@/lib/utils";
import { TenantEditForm } from "./tenant-edit-form";

interface TenantDetailPageProps {
  params: Promise<{ locale: string; id: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("settings.tenants");
  return { title: t("detailTitle") };
}

export default async function TenantDetailPage({
  params,
}: TenantDetailPageProps): Promise<JSX.Element> {
  const { locale, id } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "manage:tenants")) {
    redirect(`/${locale}/unauthorized`);
  }

  let tenant: Tenant;
  try {
    tenant = await getTenant(id, { token: session.user.access_token });
  } catch {
    notFound();
  }

  const t = await getTranslations("settings.tenants");

  return (
    <div className="space-y-6 animate-fade-in">
      <div>
        <Link
          href={`/${locale}/settings/tenants`}
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden="true" />
          {t("backToList")}
        </Link>
        <div className="mt-2 flex items-center gap-3">
          <Building2 className="h-6 w-6 text-muted-foreground" aria-hidden="true" />
          <h1 className="text-2xl font-bold tracking-tight">{tenant.display_name}</h1>
          <Badge variant="outline" className="font-mono text-[11px]">
            {tenant.slug}
          </Badge>
        </div>
        <p className="mt-1 font-mono text-xs text-muted-foreground">{tenant.id}</p>
      </div>

      {/* Profile edit */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("profileTitle")}</CardTitle>
        </CardHeader>
        <CardContent>
          <TenantEditForm tenant={tenant} />
        </CardContent>
      </Card>

      {/* Settings quick links */}
      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader className="flex flex-row items-start gap-3 space-y-0 pb-3">
            <ShieldOff className="mt-0.5 h-5 w-5 text-amber-500" aria-hidden="true" />
            <div>
              <CardTitle className="text-sm">{t("dlpTitle")}</CardTitle>
              <p className="mt-1 text-xs text-muted-foreground">
                {t("dlpHint")}
              </p>
            </div>
          </CardHeader>
          <CardContent>
            <Link href={`/${locale}/settings/dlp`}>
              <Button variant="outline" size="sm" className="w-full">
                <KeySquare className="mr-2 h-4 w-4" aria-hidden="true" />
                {t("openDlp")}
              </Button>
            </Link>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-start gap-3 space-y-0 pb-3">
            <Shield className="mt-0.5 h-5 w-5 text-blue-500" aria-hidden="true" />
            <div>
              <CardTitle className="text-sm">{t("retentionTitle")}</CardTitle>
              <p className="mt-1 text-xs text-muted-foreground">
                {t("retentionHint")}
              </p>
            </div>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex justify-between">
              <span className="text-muted-foreground">
                {t("screenshotRetentionDays")}
              </span>
              <span className="font-medium">
                {tenant.settings?.max_screenshot_retention_days ?? "—"}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">
                {t("liveViewHistoryRestricted")}
              </span>
              <span className="font-medium">
                {tenant.settings?.live_view_history_restricted
                  ? t("yes")
                  : t("no")}
              </span>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-start gap-3 space-y-0 pb-3">
            <Plug className="mt-0.5 h-5 w-5 text-muted-foreground" aria-hidden="true" />
            <div>
              <CardTitle className="text-sm">{t("integrationsTitle")}</CardTitle>
              <p className="mt-1 text-xs text-muted-foreground">
                {t("integrationsHint")}
              </p>
            </div>
          </CardHeader>
          <CardContent>
            <Link href={`/${locale}/settings/integrations`}>
              <Button variant="outline" size="sm" className="w-full">
                {t("openIntegrations")}
              </Button>
            </Link>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-sm">{t("metadataTitle")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-1 text-sm">
            <div className="flex justify-between">
              <span className="text-muted-foreground">{t("created")}</span>
              <span>{formatDateTR(tenant.created_at, "d MMM yyyy HH:mm")}</span>
            </div>
            {tenant.updated_at && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">{t("updated")}</span>
                <span>
                  {formatDateTR(tenant.updated_at, "d MMM yyyy HH:mm")}
                </span>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Danger zone */}
      <Card className="border-red-200 dark:border-red-900/50">
        <CardHeader>
          <CardTitle className="text-base text-red-600 dark:text-red-400">
            {t("dangerZoneTitle")}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Alert variant="destructive">
            <AlertTriangle className="h-4 w-4" />
            <AlertTitle>{t("deactivateTitle")}</AlertTitle>
            <AlertDescription>{t("deactivateBody")}</AlertDescription>
          </Alert>
          <Button
            variant="destructive"
            className="mt-4"
            disabled
            title={t("deactivateNotImplemented")}
          >
            {t("deactivate")}
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
