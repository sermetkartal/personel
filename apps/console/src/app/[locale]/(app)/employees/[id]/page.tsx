import { getTranslations } from "next-intl/server";
import { redirect, notFound } from "next/navigation";
import { getSession } from "@/lib/auth/session";
import { can } from "@/lib/auth/rbac";
import { getEmployeeDetail } from "@/lib/api/employees";
import { EmployeeDetailClient } from "./detail-client";

interface EmployeeDetailPageProps {
  params: Promise<{ locale: string; id: string }>;
}

export async function generateMetadata({ params }: EmployeeDetailPageProps) {
  const { id } = await params;
  const t = await getTranslations("employees");
  return { title: `${t("detail.title")} — ${id.slice(0, 8)}` };
}

export default async function EmployeeDetailPage({
  params,
}: EmployeeDetailPageProps): Promise<JSX.Element> {
  const { locale, id } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:employees")) {
    redirect(`/${locale}/unauthorized`);
  }

  const detail = await getEmployeeDetail(id, undefined, {
    token: session.user.access_token,
  }).catch((err) => {
    console.error("[employees/detail] fetch failed:", err);
    return null;
  });

  if (!detail) {
    notFound();
  }

  return <EmployeeDetailClient detail={detail} locale={locale} />;
}
