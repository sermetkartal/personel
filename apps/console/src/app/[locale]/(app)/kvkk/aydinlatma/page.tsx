import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { FilePen } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { getAydinlatma, type AydinlatmaInfo } from "@/lib/api/kvkk";
import { AydinlatmaEditor } from "./aydinlatma-editor";

interface KvkkAydinlatmaPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("kvkk.aydinlatma");
  return { title: t("title") };
}

export default async function KvkkAydinlatmaPage({
  params,
}: KvkkAydinlatmaPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:kvkk")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("kvkk.aydinlatma");

  let initial: AydinlatmaInfo | null = null;
  try {
    const resp = await getAydinlatma({ token: session.user.access_token });
    if (resp?.markdown || resp?.version != null) {
      initial = resp;
    }
  } catch {
    // Degraded: editor still loads with blank content.
  }

  const canPublish = can(session.user.role, "manage:kvkk");

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center gap-3">
        <FilePen className="h-7 w-7 text-muted-foreground" aria-hidden="true" />
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
          <p className="text-muted-foreground">{t("subtitle")}</p>
        </div>
      </div>

      <p className="text-sm text-muted-foreground">{t("intro")}</p>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("editorLabel")}</CardTitle>
        </CardHeader>
        <CardContent>
          <AydinlatmaEditor initial={initial} canPublish={canPublish} />
        </CardContent>
      </Card>
    </div>
  );
}
