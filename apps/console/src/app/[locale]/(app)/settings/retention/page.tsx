import { getTranslations } from "next-intl/server";
import { redirect } from "next/navigation";
import { getSession } from "@/lib/auth/session";
import { can } from "@/lib/auth/rbac";
import {
  getRetention,
  DEFAULT_KVKK_RETENTION,
  type RetentionPolicy,
} from "@/lib/api/settings-extended";
import { RetentionForm } from "./retention-form";

interface Props {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("settings.retention");
  return { title: t("title") };
}

/**
 * Retention policy settings.
 *
 * The backend enforces KVKK floors authoritatively, but the form also
 * validates them client-side so the operator gets immediate feedback.
 * The "reset to minimum" button replaces all six fields with the
 * `DEFAULT_KVKK_RETENTION` baseline.
 */
export default async function RetentionSettingsPage({
  params,
}: Props): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:settings")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("settings.retention");

  let initial: RetentionPolicy = DEFAULT_KVKK_RETENTION;
  try {
    initial = await getRetention({ token: session.user.access_token });
  } catch {
    // Backend not ready → default to KVKK minimum.
  }

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold tracking-tight">{t("title")}</h2>
        <p className="text-sm text-muted-foreground">{t("subtitle")}</p>
      </div>

      <RetentionForm initial={initial} token={session.user.access_token} />
    </div>
  );
}
