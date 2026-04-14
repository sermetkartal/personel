import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { LiveViewRequestForm } from "@/components/live-view/request-form";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { ShieldAlert, ShieldCheck } from "lucide-react";

interface RequestPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("liveView.request");
  return { title: t("title") };
}

export default async function LiveViewRequestPage({
  params,
}: RequestPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "request:live-view")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("liveView.request");
  const isAdmin = session.user.role === "admin";

  return (
    <div className="space-y-6 max-w-2xl animate-fade-in">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
        <p className="text-muted-foreground">{t("subtitle")}</p>
      </div>

      {isAdmin ? (
        // ADR 0026 — admin bypasses the HR/IT dual-control gate. The session
        // is auto-approved on submit and LiveKit provisioned in-line. Show
        // a distinct notice so the admin knows they are exercising elevated
        // authority; every such session is still flagged admin_bypass=true
        // in the audit trail.
        <Alert variant="default" role="note">
          <ShieldCheck className="h-4 w-4" aria-hidden="true" />
          <AlertTitle>Yönetici erişimi</AlertTitle>
          <AlertDescription>
            Yönetici rolü ikili kontrol onayını atlayabilir. Oturum talebiniz
            gönderildiği anda otomatik onaylanır ve canlı izleme başlar.
            Denetim kaydına <code>admin_bypass=true</code> olarak işlenir.
          </AlertDescription>
        </Alert>
      ) : (
        // KVKK compliance notice for non-admin (dual-control path).
        <Alert variant="warning">
          <ShieldAlert className="h-4 w-4" aria-hidden="true" />
          <AlertTitle>{t("kvkkNoticeTitle")}</AlertTitle>
          <AlertDescription>{t("kvkkNoticeBody")}</AlertDescription>
        </Alert>
      )}

      <LiveViewRequestForm isAdmin={isAdmin} />
    </div>
  );
}
