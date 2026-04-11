import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { AuditChainStatus } from "@/components/audit/chain-status";
import { Link } from "@/lib/i18n/navigation";
import { ChevronLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Info } from "lucide-react";

interface VerifyPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("audit");
  return { title: t("verify.title") };
}

export default async function AuditVerifyPage({
  params,
}: VerifyPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:audit-log")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("audit");

  return (
    <div className="space-y-6 max-w-3xl animate-fade-in">
      {/* Back link */}
      <Button variant="ghost" size="sm" className="-ml-2" asChild>
        <Link href="/audit">
          <ChevronLeft className="mr-1 h-4 w-4" aria-hidden="true" />
          {t("backToLog")}
        </Link>
      </Button>

      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("verify.title")}</h1>
        <p className="text-muted-foreground">{t("verify.subtitle")}</p>
      </div>

      {/* Explanation of the hash chain algorithm */}
      <Alert variant="default" role="note">
        <Info className="h-4 w-4" aria-hidden="true" />
        <AlertTitle>{t("verify.algorithmTitle")}</AlertTitle>
        <AlertDescription className="space-y-2 mt-2 text-sm">
          <p>{t("verify.algorithmBody")}</p>
          <code className="block rounded bg-muted px-3 py-2 font-mono text-xs">
            this_hash = SHA-256( seq || payload_hash || prev_hash )
          </code>
          <p className="text-xs text-muted-foreground">{t("verify.retentionNote")}</p>
        </AlertDescription>
      </Alert>

      {/* Live chain status — client-rendered with auto-refresh */}
      <AuditChainStatus compact={false} />
    </div>
  );
}
