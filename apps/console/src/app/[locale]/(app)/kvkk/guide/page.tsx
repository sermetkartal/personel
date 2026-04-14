import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { Link } from "@/lib/i18n/navigation";
import {
  BookOpen,
  FileText,
  Lock,
  Trash2,
  ShieldAlert,
  FileCheck2,
  FilePen,
  FileSignature,
  ArrowRight,
} from "lucide-react";

interface KvkkGuidePageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("kvkk.guide");
  return { title: t("title") };
}

export default async function KvkkGuidePage({
  params,
}: KvkkGuidePageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:kvkk")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("kvkk.guide");

  const steps = [
    { key: "step1", href: `/${locale}/kvkk/verbis`, icon: FileCheck2 },
    { key: "step2", href: `/${locale}/kvkk/aydinlatma`, icon: FilePen },
    { key: "step3", href: `/${locale}/kvkk/dpa`, icon: FileSignature },
    { key: "step4", href: `/${locale}/kvkk/dpia`, icon: ShieldAlert },
    { key: "step5", href: `/${locale}/kvkk/acik-riza`, icon: FileSignature },
    { key: "step6", href: `/${locale}/kvkk/dsr`, icon: FileText },
    { key: "step7", href: `/${locale}/kvkk/legal-hold`, icon: Lock },
  ] as const;

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center gap-3">
        <BookOpen className="h-7 w-7 text-muted-foreground" aria-hidden="true" />
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
          <p className="text-muted-foreground">{t("subtitle")}</p>
        </div>
      </div>

      <div className="rounded-lg border bg-card p-5">
        <h2 className="mb-2 text-lg font-semibold">{t("whatIsKvkk")}</h2>
        <p className="text-sm text-muted-foreground">{t("whatIsKvkkBody")}</p>
      </div>

      <p className="text-sm text-muted-foreground">{t("intro")}</p>

      <div>
        <h2 className="mb-3 text-lg font-semibold">{t("stepsTitle")}</h2>
        <div className="grid gap-3 md:grid-cols-2">
          {steps.map((step) => {
            const Icon = step.icon;
            return (
              <Link
                key={step.key}
                href={step.href}
                className="group flex items-start gap-3 rounded-lg border bg-card p-4 transition-colors hover:border-primary/40 hover:bg-accent/20"
              >
                <Icon
                  className="mt-0.5 h-5 w-5 text-muted-foreground group-hover:text-primary"
                  aria-hidden="true"
                />
                <div className="flex-1">
                  <div className="font-medium">{t(step.key)}</div>
                  <p className="text-xs text-muted-foreground">
                    {t(`${step.key}Desc`)}
                  </p>
                </div>
                <ArrowRight
                  className="h-4 w-4 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100"
                  aria-hidden="true"
                />
              </Link>
            );
          })}
        </div>
      </div>
    </div>
  );
}
