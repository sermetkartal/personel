import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { FileSignature, Construction } from "lucide-react";

interface KvkkDpaPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("kvkk.dpa");
  return { title: t("title") };
}

export default async function KvkkDpaPage({
  params,
}: KvkkDpaPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:kvkk")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("kvkk.dpa");
  const tc = await getTranslations("common");

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center gap-3">
        <FileSignature className="h-7 w-7 text-muted-foreground" aria-hidden="true" />
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
          <p className="text-muted-foreground">{t("subtitle")}</p>
        </div>
      </div>

      <p className="text-sm text-muted-foreground">{t("intro")}</p>

      <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-20 text-center">
        <Construction className="mb-3 h-10 w-10 text-muted-foreground/40" aria-hidden="true" />
        <p className="text-muted-foreground text-sm">{tc("comingSoon")}</p>
        <p className="text-muted-foreground/70 text-xs mt-1">{tc("comingSoonHint")}</p>
      </div>
    </div>
  );
}
