import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { Construction } from "lucide-react";

interface EmployeesPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("employees");
  return { title: t("title") };
}

export default async function EmployeesPage({
  params,
}: EmployeesPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:employees")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("employees");

  return (
    <div className="space-y-6 animate-fade-in">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
        <p className="text-muted-foreground">{t("subtitle")}</p>
      </div>
      <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-20 text-center">
        <Construction className="mb-3 h-10 w-10 text-muted-foreground/40" aria-hidden="true" />
        <p className="text-muted-foreground text-sm">Phase 2</p>
      </div>
    </div>
  );
}
