import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect, notFound } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { getLiveViewSession } from "@/lib/api/liveview";
import { LiveKitViewer } from "@/components/live-view/livekit-viewer";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { ShieldAlert } from "lucide-react";
import { formatDateTR } from "@/lib/utils";

interface SessionPageProps {
  params: Promise<{ locale: string; sessionId: string }>;
}

export async function generateMetadata({ params }: SessionPageProps) {
  const { sessionId } = await params;
  const t = await getTranslations("liveView.viewer");
  return { title: `${t("titlePrefix")} ${sessionId.slice(0, 8)}` };
}

export default async function LiveViewSessionPage({
  params,
}: SessionPageProps): Promise<JSX.Element> {
  const { locale, sessionId } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "watch:live-view")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("liveView.viewer");

  let liveSession;
  try {
    liveSession = await getLiveViewSession(sessionId);
  } catch {
    notFound();
  }

  // Only allow viewing active sessions
  if (liveSession.state !== "ACTIVE") {
    return (
      <div className="space-y-6 max-w-2xl animate-fade-in">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
        </div>
        <Alert variant="warning">
          <ShieldAlert className="h-4 w-4" aria-hidden="true" />
          <AlertTitle>{t("notActiveTitle")}</AlertTitle>
          <AlertDescription>
            {t("notActiveBody", { state: liveSession.state })}
            {liveSession.ended_at && (
              <span className="block mt-1 text-xs text-muted-foreground">
                {t("endedAt")}:{" "}
                <time dateTime={liveSession.ended_at}>
                  {formatDateTR(liveSession.ended_at)}
                </time>
              </span>
            )}
          </AlertDescription>
        </Alert>
      </div>
    );
  }

  const livekitUrl = process.env["LIVEKIT_URL"] ?? "ws://localhost:7880";

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
          <p className="text-xs text-muted-foreground font-mono">
            {t("sessionId")}: {liveSession.id}
          </p>
        </div>
      </div>

      {/* Audit reminder */}
      <Alert variant="default" role="note">
        <ShieldAlert className="h-4 w-4" aria-hidden="true" />
        <AlertTitle>{t("auditNoticeTitle")}</AlertTitle>
        <AlertDescription className="text-xs">{t("auditNoticeBody")}</AlertDescription>
      </Alert>

      <LiveKitViewer
        session={liveSession}
        livekitUrl={livekitUrl}
        userRole={session.user.role}
      />
    </div>
  );
}
