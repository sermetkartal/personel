import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import Link from "next/link";
import { listPolicies } from "@/lib/api/policy";
import type { PolicyList } from "@/lib/api/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Plus, Shield, Edit } from "lucide-react";
import { formatDateTR } from "@/lib/utils";

interface PoliciesPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("policies");
  return { title: t("title") };
}

export default async function PoliciesPage({
  params,
}: PoliciesPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:policies")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("policies");
  const canManage = can(session.user.role, "manage:policies");

  let data: PolicyList = {
    items: [],
    pagination: { page: 1, page_size: 20, total: 0 },
  };
  try {
    data = await listPolicies(
      { page: 1, page_size: 50 },
      { token: session.user.access_token },
    );
  } catch {
    // Render empty state on fetch failure.
  }

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
          <p className="text-muted-foreground">{t("subtitle")}</p>
        </div>
        {canManage && (
          <Link href={`/${locale}/policies/editor`}>
            <Button>
              <Plus className="mr-2 h-4 w-4" aria-hidden="true" />
              {t("create")}
            </Button>
          </Link>
        )}
      </div>

      {data.items.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-20 text-center">
          <Shield className="mb-3 h-10 w-10 text-muted-foreground/40" aria-hidden="true" />
          <p className="text-muted-foreground text-sm">{t("emptyList")}</p>
        </div>
      ) : (
        <Card>
          <CardContent className="p-0">
            <table className="w-full">
              <thead className="border-b bg-muted/30">
                <tr>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    {t("detail.name")}
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    {t("detail.version")}
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    {t("listUpdated")}
                  </th>
                  <th className="px-4 py-3" />
                </tr>
              </thead>
              <tbody>
                {data.items.map((p) => (
                  <tr
                    key={p.id}
                    className="border-b transition-colors hover:bg-muted/30"
                  >
                    <td className="px-4 py-3">
                      <div className="font-medium">{p.name}</div>
                      {p.description && (
                        <div className="mt-0.5 text-xs text-muted-foreground">
                          {p.description}
                        </div>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant="outline" className="font-mono text-[11px]">
                        v{p.version}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      <time dateTime={p.updated_at ?? p.created_at}>
                        {formatDateTR(
                          p.updated_at ?? p.created_at,
                          "d MMM yyyy HH:mm",
                        )}
                      </time>
                    </td>
                    <td className="px-4 py-3 text-right">
                      {canManage && (
                        <Link href={`/${locale}/policies/editor?id=${p.id}`}>
                          <Button variant="ghost" size="sm">
                            <Edit className="mr-1 h-3 w-3" aria-hidden="true" />
                            {t("edit")}
                          </Button>
                        </Link>
                      )}
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
