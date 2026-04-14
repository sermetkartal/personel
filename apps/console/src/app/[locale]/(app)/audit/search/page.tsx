import { getTranslations } from "next-intl/server";
import { redirect } from "next/navigation";
import { getSession } from "@/lib/auth/session";
import { can } from "@/lib/auth/rbac";
import { Link } from "@/lib/i18n/navigation";
import { ArrowLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import { AuditSearchClient } from "./audit-search-client";

interface AuditSearchPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("audit.search");
  return { title: t("title") };
}

export default async function AuditSearchPage({
  params,
}: AuditSearchPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  // Reuse existing view:audit-log permission (admin, dpo, auditor).
  // Investigator has its own live-feed scope on the dashboard; the search
  // UI gates on the same list.
  if (!session?.user || !can(session.user.role, "view:audit-log")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("audit.search");

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
          <p className="text-muted-foreground">{t("subtitle")}</p>
        </div>
        <Button variant="outline" size="sm" asChild>
          <Link href="/audit">
            <ArrowLeft className="mr-2 h-4 w-4" aria-hidden="true" />
            {t("backToLog")}
          </Link>
        </Button>
      </div>

      <AuditSearchClient token={session.user.access_token} />
    </div>
  );
}
