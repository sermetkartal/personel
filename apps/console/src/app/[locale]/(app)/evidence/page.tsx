import { getTranslations } from "next-intl/server";
import { redirect } from "next/navigation";
import { getSession } from "@/lib/auth/session";
import { can } from "@/lib/auth/rbac";
import { getEvidenceCoverage } from "@/lib/api/evidence";
import { EvidenceCoverageClient } from "./evidence-client";

interface EvidencePageProps {
  searchParams: Promise<{ period?: string }>;
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("evidence");
  return { title: t("title") };
}

export default async function EvidencePage({
  searchParams,
  params,
}: EvidencePageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:evidence")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("evidence");
  const sp = await searchParams;
  const period = sp.period ?? new Date().toISOString().slice(0, 7);

  // Fetch on the server so the page renders with data already in place.
  // Failure here is a configuration issue (no evidence store wired) —
  // render an empty-state rather than crashing.
  const coverage = await getEvidenceCoverage(period).catch(() => null);

  const canDownload = can(session.user.role, "download:evidence-pack");

  return (
    <div className="space-y-6 animate-fade-in">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
        <p className="text-muted-foreground">{t("subtitle")}</p>
      </div>

      <EvidenceCoverageClient
        initialCoverage={coverage}
        initialPeriod={period}
        canDownload={canDownload}
      />
    </div>
  );
}
