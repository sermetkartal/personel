import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { FileSignature } from "lucide-react";
import { listConsents, type ConsentList } from "@/lib/api/kvkk";
import { listUsers } from "@/lib/api/users";
import type { UserList } from "@/lib/api/types";
import { AcikRizaClient } from "./acik-riza-client";

interface KvkkAcikRizaPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("kvkk.acikRiza");
  return { title: t("title") };
}

export default async function KvkkAcikRizaPage({
  params,
}: KvkkAcikRizaPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:kvkk")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("kvkk.acikRiza");

  let initialConsents: ConsentList = { items: [] };
  try {
    initialConsents = await listConsents("dlp", {
      token: session.user.access_token,
    });
  } catch {
    // Degraded.
  }

  let initialUsers: UserList = {
    items: [],
    pagination: { page: 1, page_size: 100, total: 0 },
  };
  try {
    initialUsers = await listUsers(
      { page_size: 100 },
      { token: session.user.access_token },
    );
  } catch {
    // Degraded.
  }

  const canManage = can(session.user.role, "manage:kvkk");

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center gap-3">
        <FileSignature className="h-7 w-7 text-muted-foreground" aria-hidden="true" />
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
          <p className="text-muted-foreground">{t("subtitle")}</p>
        </div>
      </div>

      <p className="text-sm text-muted-foreground">{t("intro")}</p>

      <AcikRizaClient
        initialConsents={initialConsents}
        initialUsers={initialUsers}
        canManage={canManage}
      />
    </div>
  );
}
