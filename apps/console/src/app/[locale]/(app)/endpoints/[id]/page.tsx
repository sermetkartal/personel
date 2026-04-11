import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect, notFound } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { getEndpoint } from "@/lib/api/endpoints";
import { Link } from "@/lib/i18n/navigation";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ChevronLeft } from "lucide-react";
import { formatDateTR } from "@/lib/utils";

interface EndpointDetailPageProps {
  params: Promise<{ locale: string; id: string }>;
}

export async function generateMetadata({ params }: EndpointDetailPageProps) {
  const { id } = await params;
  return { title: `Endpoint ${id.slice(0, 8)}` };
}

const STATUS_VARIANTS = {
  active: "success",
  revoked: "destructive",
  offline: "warning",
} as const;

export default async function EndpointDetailPage({
  params,
}: EndpointDetailPageProps): Promise<JSX.Element> {
  const { locale, id } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:endpoints")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("endpoints");

  let endpoint;
  try {
    endpoint = await getEndpoint(id);
  } catch {
    notFound();
  }

  return (
    <div className="space-y-6 max-w-3xl animate-fade-in">
      <Button variant="ghost" size="sm" className="-ml-2" asChild>
        <Link href="/endpoints">
          <ChevronLeft className="mr-1 h-4 w-4" aria-hidden="true" />
          {t("backToList")}
        </Link>
      </Button>

      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight font-mono">{endpoint.hostname}</h1>
          <code className="text-xs text-muted-foreground">{endpoint.id}</code>
        </div>
        <Badge variant={STATUS_VARIANTS[endpoint.status] ?? "default"}>
          {endpoint.status}
        </Badge>
      </div>

      <dl className="grid grid-cols-2 gap-4 rounded-lg border bg-card p-4 text-sm">
        <div>
          <dt className="text-xs text-muted-foreground">{t("detail.osVersion")}</dt>
          <dd>{endpoint.os_version ?? "—"}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">{t("detail.agentVersion")}</dt>
          <dd>{endpoint.agent_version ?? "—"}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">{t("detail.enrolledAt")}</dt>
          <dd>
            <time dateTime={endpoint.enrolled_at}>{formatDateTR(endpoint.enrolled_at)}</time>
          </dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">{t("detail.lastSeen")}</dt>
          <dd>
            {endpoint.last_seen_at ? (
              <time dateTime={endpoint.last_seen_at}>{formatDateTR(endpoint.last_seen_at)}</time>
            ) : "—"}
          </dd>
        </div>
      </dl>

      <p className="text-sm text-muted-foreground">Phase 2: timeline, screenshots, sessions</p>
    </div>
  );
}
