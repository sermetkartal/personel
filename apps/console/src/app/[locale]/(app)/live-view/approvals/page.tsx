import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { LiveViewApprovalsClient } from "./approvals-client";

interface ApprovalsPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("liveView.approval");
  return { title: t("title") };
}

export default async function LiveViewApprovalsPage({
  params,
}: ApprovalsPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  // Only HR can approve live view requests
  if (!session?.user || !can(session.user.role, "approve:live-view")) {
    redirect(`/${locale}/unauthorized`);
  }

  return (
    <div className="space-y-6 animate-fade-in">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">
          Canlı İzleme Onay Kuyruğu
        </h1>
        <p className="text-muted-foreground">
          İK onayı bekleyen canlı izleme talepleri. İkili kontrol zorunludur.
        </p>
      </div>

      <LiveViewApprovalsClient currentUserId={session.user.id} />
    </div>
  );
}
