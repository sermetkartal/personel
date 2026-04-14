import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { getPolicy } from "@/lib/api/policy";
import type { Policy } from "@/lib/api/types";
import { PolicyEditorClient } from "./policy-editor-client";

interface PolicyEditorPageProps {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ id?: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("policies");
  return { title: t("editor.title") };
}

export default async function PolicyEditorPage({
  params,
  searchParams,
}: PolicyEditorPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const { id } = await searchParams;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "manage:policies")) {
    redirect(`/${locale}/unauthorized`);
  }

  let initial: Policy | null = null;
  if (id) {
    try {
      initial = await getPolicy(id, { token: session.user.access_token });
    } catch {
      // Fall through to "new policy" mode on load failure.
      initial = null;
    }
  }

  return <PolicyEditorClient locale={locale} initial={initial} />;
}
