import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import Link from "next/link";
import { listTenants } from "@/lib/api/settings";
import type { TenantList } from "@/lib/api/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Plus, Building2, ChevronRight } from "lucide-react";
import { formatDateTR } from "@/lib/utils";

interface TenantsSettingsPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("settings");
  return { title: t("tenants.title") };
}

export default async function TenantsSettingsPage({
  params,
}: TenantsSettingsPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "manage:tenants")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("settings.tenants");

  let data: TenantList = {
    items: [],
    pagination: { page: 1, page_size: 20, total: 0 },
  };
  try {
    data = await listTenants(
      { page: 1, page_size: 100 },
      { token: session.user.access_token },
    );
  } catch {
    // Empty state.
  }

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("listTitle")}</h1>
          <p className="text-muted-foreground">{t("listSubtitle")}</p>
        </div>
        <Link href={`/${locale}/settings/tenants/new`}>
          <Button>
            <Plus className="mr-2 h-4 w-4" aria-hidden="true" />
            {t("create")}
          </Button>
        </Link>
      </div>

      {data.items.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-20 text-center">
          <Building2 className="mb-3 h-10 w-10 text-muted-foreground/40" aria-hidden="true" />
          <p className="text-sm text-muted-foreground">{t("empty")}</p>
        </div>
      ) : (
        <Card>
          <CardContent className="p-0">
            <table className="w-full">
              <thead className="border-b bg-muted/30">
                <tr>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    {t("name")}
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    {t("slug")}
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    {t("created")}
                  </th>
                  <th className="px-4 py-3" />
                </tr>
              </thead>
              <tbody>
                {data.items.map((tenant) => (
                  <tr
                    key={tenant.id}
                    className="border-b transition-colors hover:bg-muted/30"
                  >
                    <td className="px-4 py-3">
                      <Link
                        href={`/${locale}/settings/tenants/${tenant.id}`}
                        className="font-medium hover:underline"
                      >
                        {tenant.display_name}
                      </Link>
                      <div className="mt-0.5 font-mono text-[10px] text-muted-foreground">
                        {tenant.id.slice(0, 8)}...
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant="outline" className="font-mono text-[11px]">
                        {tenant.slug}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      <time dateTime={tenant.created_at}>
                        {formatDateTR(tenant.created_at, "d MMM yyyy")}
                      </time>
                    </td>
                    <td className="px-4 py-3 text-right">
                      <Link href={`/${locale}/settings/tenants/${tenant.id}`}>
                        <Button variant="ghost" size="sm">
                          {t("view")}
                          <ChevronRight className="ml-1 h-3 w-3" aria-hidden="true" />
                        </Button>
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
