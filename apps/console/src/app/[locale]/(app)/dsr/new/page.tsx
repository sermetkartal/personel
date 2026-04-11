import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { DSRNewForm } from "./dsr-new-form";
import { Link } from "@/lib/i18n/navigation";
import { Button } from "@/components/ui/button";
import { ChevronLeft } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Info } from "lucide-react";

interface DSRNewPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("dsr");
  return { title: t("new.title") };
}

export default async function DSRNewPage({
  params,
}: DSRNewPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "create:dsr")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("dsr");

  return (
    <div className="space-y-6 max-w-2xl animate-fade-in">
      <Button variant="ghost" size="sm" className="-ml-2" asChild>
        <Link href="/dsr">
          <ChevronLeft className="mr-1 h-4 w-4" aria-hidden="true" />
          {t("backToList")}
        </Link>
      </Button>

      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("new.title")}</h1>
        <p className="text-muted-foreground">{t("new.subtitle")}</p>
      </div>

      <Alert variant="default" role="note">
        <Info className="h-4 w-4" aria-hidden="true" />
        <AlertTitle>{t("new.kvkkTitle")}</AlertTitle>
        <AlertDescription className="text-sm">{t("new.kvkkBody")}</AlertDescription>
      </Alert>

      <DSRNewForm />
    </div>
  );
}
