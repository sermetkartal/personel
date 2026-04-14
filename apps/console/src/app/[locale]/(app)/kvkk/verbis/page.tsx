import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { FileCheck2 } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { getVerbis, type VerbisInfo } from "@/lib/api/kvkk";
import { VerbisForm } from "./verbis-form";

interface KvkkVerbisPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("kvkk.verbis");
  return { title: t("title") };
}

export default async function KvkkVerbisPage({
  params,
}: KvkkVerbisPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:kvkk")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("kvkk.verbis");

  // Fetch current VERBİS registration. Graceful degradation: if the backend
  // is unreachable or the record is empty, render the form in "create" mode.
  let initial: VerbisInfo | null = null;
  try {
    const resp = await getVerbis({ token: session.user.access_token });
    if (resp?.registration_number) {
      initial = resp;
    }
  } catch {
    // Degraded: form still renders with empty values.
  }

  const canEdit = can(session.user.role, "manage:kvkk");

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center gap-3">
        <FileCheck2 className="h-7 w-7 text-muted-foreground" aria-hidden="true" />
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
          <p className="text-muted-foreground">{t("subtitle")}</p>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("whoNeedsTitle")}</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">{t("whoNeedsBody")}</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("stepsTitle")}</CardTitle>
        </CardHeader>
        <CardContent>
          <ol className="list-decimal space-y-2 pl-5 text-sm text-muted-foreground">
            <li>{t("step1")}</li>
            <li>{t("step2")}</li>
            <li>{t("step3")}</li>
            <li>{t("step4")}</li>
            <li>{t("step5")}</li>
          </ol>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("statusLabel")}</CardTitle>
        </CardHeader>
        <CardContent>
          <VerbisForm initial={initial} canEdit={canEdit} />
        </CardContent>
      </Card>
    </div>
  );
}
