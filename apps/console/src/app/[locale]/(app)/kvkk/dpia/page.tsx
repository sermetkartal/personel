import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { ShieldAlert, Construction } from "lucide-react";

interface KvkkDpiaPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("kvkk.dpia");
  return { title: t("title") };
}

export default async function KvkkDpiaPage({
  params,
}: KvkkDpiaPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:kvkk")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("kvkk.dpia");
  const tc = await getTranslations("common");

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center gap-3">
        <ShieldAlert className="h-7 w-7 text-muted-foreground" aria-hidden="true" />
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
          <p className="text-muted-foreground">{t("subtitle")}</p>
        </div>
      </div>

      <p className="text-sm text-muted-foreground">{t("intro")}</p>
      <p className="text-sm font-medium">{t("dlpStatusInactive")}</p>

      <div className="rounded-lg border bg-card p-5">
        <h2 className="mb-2 text-lg font-semibold">{t("whyNeededTitle")}</h2>
        <p className="text-sm text-muted-foreground">{t("whyNeededBody")}</p>
      </div>

      <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12 text-center">
        <Construction className="mb-3 h-10 w-10 text-muted-foreground/40" aria-hidden="true" />
        <p className="text-muted-foreground text-sm">{tc("comingSoon")}</p>
        <p className="text-muted-foreground/70 text-xs mt-1">{tc("comingSoonHint")}</p>
      </div>
    </div>
  );
}
