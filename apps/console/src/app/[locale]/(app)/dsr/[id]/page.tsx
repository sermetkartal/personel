import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect, notFound } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { getDSR } from "@/lib/api/dsr";
import { DSRDetailClient } from "./dsr-detail-client";

interface DSRDetailPageProps {
  params: Promise<{ locale: string; id: string }>;
}

export async function generateMetadata({ params }: DSRDetailPageProps) {
  const { id } = await params;
  const t = await getTranslations("dsr");
  return { title: `${t("detail.title")} ${id.slice(0, 8)}` };
}

export default async function DSRDetailPage({
  params,
}: DSRDetailPageProps): Promise<JSX.Element> {
  const { locale, id } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:dsr")) {
    redirect(`/${locale}/unauthorized`);
  }

  let dsr;
  try {
    dsr = await getDSR(id, { token: session.user.access_token });
  } catch {
    notFound();
  }

  return (
    <DSRDetailClient
      dsr={dsr}
      role={session.user.role}
      canExecuteErasure={can(session.user.role, "execute:dsr-erasure")}
    />
  );
}
