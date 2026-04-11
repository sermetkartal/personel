import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { Construction } from "lucide-react";

interface LegalHoldNewPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("legalHold");
  return { title: t("new.title") };
}

export default async function LegalHoldNewPage({
  params,
}: LegalHoldNewPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "place:legal-hold")) {
    redirect(`/${locale}/unauthorized`);
  }

  return (
    <div className="space-y-6 max-w-2xl animate-fade-in">
      <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-20 text-center">
        <Construction className="mb-3 h-10 w-10 text-muted-foreground/40" aria-hidden="true" />
        <p className="text-muted-foreground text-sm">Phase 2 — new legal hold form</p>
      </div>
    </div>
  );
}
