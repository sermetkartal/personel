import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { Link } from "@/lib/i18n/navigation";
import { Button } from "@/components/ui/button";
import { PlusCircle, Construction } from "lucide-react";

interface LegalHoldPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("legalHold");
  return { title: t("title") };
}

export default async function LegalHoldPage({
  params,
}: LegalHoldPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "place:legal-hold")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("legalHold");
  const tc = await getTranslations("common");

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
          <p className="text-muted-foreground">{t("subtitle")}</p>
        </div>
        <Button size="sm" asChild>
          <Link href="/legal-hold/new">
            <PlusCircle className="mr-2 h-4 w-4" aria-hidden="true" />
            {t("newHold")}
          </Link>
        </Button>
      </div>
      <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-20 text-center">
        <Construction className="mb-3 h-10 w-10 text-muted-foreground/40" aria-hidden="true" />
        <p className="text-muted-foreground text-sm">{tc("comingSoon")}</p>
        <p className="text-muted-foreground/70 text-xs mt-1">{tc("comingSoonHint")}</p>
      </div>
    </div>
  );
}
