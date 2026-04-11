import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";

interface EmployeeDetailPageProps {
  params: Promise<{ locale: string; id: string }>;
}

export async function generateMetadata({ params }: EmployeeDetailPageProps) {
  const { id } = await params;
  const t = await getTranslations("employees");
  return { title: `${t("detail.title")} ${id.slice(0, 8)}` };
}

export default async function EmployeeDetailPage({
  params,
}: EmployeeDetailPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:employees")) {
    redirect(`/${locale}/unauthorized`);
  }

  return (
    <div className="space-y-6 animate-fade-in">
      <p className="text-muted-foreground text-sm">Phase 2 — employee detail</p>
    </div>
  );
}
