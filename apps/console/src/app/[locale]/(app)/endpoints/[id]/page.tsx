import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect, notFound } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import {
  getEndpoint,
  listEndpointCommands,
  type EndpointCommandList,
} from "@/lib/api/endpoints";
import { EndpointDetailClient } from "./endpoint-detail-client";
import type { Endpoint } from "@/lib/api/types";

interface EndpointDetailPageProps {
  params: Promise<{ locale: string; id: string }>;
}

export async function generateMetadata({ params }: EndpointDetailPageProps) {
  const { id } = await params;
  const t = await getTranslations("endpoints");
  return { title: `${t("detail.title")} · ${id.slice(0, 8)}` };
}

export default async function EndpointDetailPage({
  params,
}: EndpointDetailPageProps): Promise<JSX.Element> {
  const { locale, id } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:endpoints")) {
    redirect(`/${locale}/unauthorized`);
  }

  const tokenOpts = { token: session.user.access_token };

  let endpoint: Endpoint;
  try {
    endpoint = await getEndpoint(id, tokenOpts);
  } catch {
    notFound();
  }

  // Command history is best-effort — if the backend hasn't shipped the
  // endpoint yet we still render the detail page with an empty list.
  let commands: EndpointCommandList = { items: [] };
  try {
    commands = await listEndpointCommands(id, tokenOpts);
  } catch {
    commands = { items: [] };
  }

  return (
    <EndpointDetailClient
      endpoint={endpoint}
      initialCommands={commands}
      role={session.user.role}
    />
  );
}
