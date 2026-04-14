import { getTranslations } from "next-intl/server";
import { redirect } from "next/navigation";
import { getSession } from "@/lib/auth/session";
import { can } from "@/lib/auth/rbac";
import { getCaMode, type CaModeInfo } from "@/lib/api/settings-extended";
import { CaModeForm } from "./ca-mode-form";

interface Props {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("settings.tls");
  return { title: t("title") };
}

/**
 * TLS / Certificate Authority mode settings.
 *
 * The choice between Let's Encrypt, internal Vault PKI (default), and a
 * commercial CA affects every one of the 18 services in the compose
 * stack — changing it triggers a rotation of all service certs, so the
 * client-side form shows a confirm gate before submitting.
 */
export default async function TlsSettingsPage({
  params,
}: Props): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:settings")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("settings.tls");

  let initial: CaModeInfo | null = null;
  try {
    initial = await getCaMode({ token: session.user.access_token });
  } catch {
    // Backend unreachable or endpoint missing — render a degraded form
    // with the default "internal" mode selected.
  }

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold tracking-tight">{t("title")}</h2>
        <p className="text-sm text-muted-foreground">{t("subtitle")}</p>
      </div>

      <CaModeForm
        initial={initial}
        token={session.user.access_token}
        canEdit={can(session.user.role, "view:settings")}
      />
    </div>
  );
}
