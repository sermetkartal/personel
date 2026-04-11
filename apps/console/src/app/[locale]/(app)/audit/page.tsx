import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { listAuditRecords } from "@/lib/api/audit";
import { AuditClient } from "./audit-client";
import { Link } from "@/lib/i18n/navigation";
import { ShieldCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { AuditList } from "@/lib/api/types";

interface AuditPageProps {
  params: Promise<{ locale: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

export async function generateMetadata() {
  const t = await getTranslations("audit");
  return { title: t("title") };
}

export default async function AuditPage({
  params,
  searchParams,
}: AuditPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const sp = await searchParams;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:audit-log")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("audit");

  // Parse search params for initial server-side fetch
  const page = Number(sp["page"] ?? "1");
  const action = typeof sp["action"] === "string" ? sp["action"] : undefined;
  const actorId = typeof sp["actor_id"] === "string" ? sp["actor_id"] : undefined;
  const from = typeof sp["from"] === "string" ? sp["from"] : undefined;
  const to = typeof sp["to"] === "string" ? sp["to"] : undefined;

  // Server-side prefetch for initial render — client component takes over after hydration
  let initialData: AuditList | undefined;
  try {
    initialData = await listAuditRecords(
      {
        page,
        page_size: 50,
        action,
        actor_id: actorId,
        from,
        to,
      },
      { token: session.user.access_token },
    );
  } catch {
    // Client will handle the error state
  }

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
          <p className="text-muted-foreground">{t("subtitle")}</p>
        </div>
        <Button variant="outline" size="sm" asChild>
          <Link href="/audit/verify">
            <ShieldCheck className="mr-2 h-4 w-4" aria-hidden="true" />
            {t("verifyLink")}
          </Link>
        </Button>
      </div>

      <AuditClient initialData={initialData ?? { items: [], pagination: { page: 1, page_size: 50, total: 0 } }} />
    </div>
  );
}
