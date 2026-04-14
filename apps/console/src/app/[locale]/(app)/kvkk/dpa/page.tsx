import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { FileSignature } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { getDpa, type DpaInfo } from "@/lib/api/kvkk";
import { DpaForm } from "./dpa-form";

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

  let initial: DpaInfo | null = null;
  try {
    const resp = await getDpa({ token: session.user.access_token });
    if (resp?.document_key) {
      initial = resp;
    }
  } catch {
    // Degraded: form renders in "not signed yet" state.
  }

  const canUpload = can(session.user.role, "manage:kvkk");

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

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("title")}</CardTitle>
        </CardHeader>
        <CardContent>
          <DpaForm initial={initial} canUpload={canUpload} />
        </CardContent>
      </Card>
    </div>
  );
}
