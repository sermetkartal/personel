import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { ShieldAlert } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { getDpia, type DpiaInfo } from "@/lib/api/kvkk";
import { DpiaForm } from "./dpia-form";

interface KvkkDpiaPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("kvkk.dpia");
  return { title: t("title") };
}

// DLP state is not exposed via a stable getter from the console right now —
// it's an operator-toggled profile at the compose level. Sprint 2C only
// needs to *display* this; the guidance to the operator is the real value.
// If/when `/v1/system/module-state` lands, replace the hardcoded false here.
async function fetchDlpState(): Promise<boolean> {
  return false;
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

  let initial: DpiaInfo | null = null;
  try {
    const resp = await getDpia({ token: session.user.access_token });
    if (resp?.amendment_key) {
      initial = resp;
    }
  } catch {
    // Degraded state.
  }

  const dlpActive = await fetchDlpState();
  const canUpload = can(session.user.role, "manage:kvkk");

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

      <div>
        {dlpActive ? (
          <Badge variant="critical">{t("dlpStatusActive")}</Badge>
        ) : (
          <Badge variant="outline">{t("dlpStatusInactive")}</Badge>
        )}
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("whyNeededTitle")}</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">{t("whyNeededBody")}</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("upload")}</CardTitle>
        </CardHeader>
        <CardContent>
          <DpiaForm initial={initial} canUpload={canUpload} />
        </CardContent>
      </Card>
    </div>
  );
}
