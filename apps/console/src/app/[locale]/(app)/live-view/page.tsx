import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { LiveViewSessionsClient } from "./sessions-client";
import { Link } from "@/lib/i18n/navigation";
import { Button } from "@/components/ui/button";
import { PlusCircle } from "lucide-react";

interface LiveViewPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("liveView");
  return { title: t("title") };
}

export default async function LiveViewPage({
  params,
}: LiveViewPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "request:live-view")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("liveView");
  const canRequest = can(session.user.role, "request:live-view");
  const canApprove = can(session.user.role, "approve:live-view");

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
          <p className="text-muted-foreground">{t("subtitle")}</p>
        </div>
        <div className="flex items-center gap-2">
          {canApprove && (
            <Button variant="outline" size="sm" asChild>
              <Link href="/live-view/approvals">
                {t("approvalQueue")}
              </Link>
            </Button>
          )}
          {canRequest && (
            <Button size="sm" asChild>
              <Link href="/live-view/request">
                <PlusCircle className="mr-2 h-4 w-4" aria-hidden="true" />
                {t("newRequest")}
              </Link>
            </Button>
          )}
        </div>
      </div>

      <LiveViewSessionsClient currentUserId={session.user.id} userRole={session.user.role} />
    </div>
  );
}
