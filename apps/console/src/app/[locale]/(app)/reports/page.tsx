import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { Link } from "@/lib/i18n/navigation";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { BarChart2, Activity, Clock, ShieldOff } from "lucide-react";

interface ReportsPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("reports");
  return { title: t("title") };
}

export default async function ReportsPage({
  params,
}: ReportsPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:reports")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("reports");

  const reportLinks = [
    {
      href: "/reports/productivity",
      icon: BarChart2,
      title: t("productivity.title"),
      description: t("productivity.description"),
    },
    {
      href: "/reports/top-apps",
      icon: Activity,
      title: t("topApps.title"),
      description: t("topApps.description"),
    },
    {
      href: "/reports/idle-active",
      icon: Clock,
      title: t("idleActive.title"),
      description: t("idleActive.description"),
    },
    {
      href: "/reports/app-blocks",
      icon: ShieldOff,
      title: t("appBlocks.title"),
      description: t("appBlocks.description"),
    },
  ];

  return (
    <div className="space-y-6 animate-fade-in">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
        <p className="text-muted-foreground">{t("subtitle")}</p>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        {reportLinks.map(({ href, icon: Icon, title, description }) => (
          <Link key={href} href={href}>
            <Card className="h-full transition-colors hover:bg-muted/30 cursor-pointer">
              <CardHeader className="pb-2">
                <CardTitle className="flex items-center gap-2 text-base">
                  <Icon className="h-5 w-5 text-muted-foreground" aria-hidden="true" />
                  {title}
                </CardTitle>
              </CardHeader>
              <CardContent>
                <CardDescription>{description}</CardDescription>
              </CardContent>
            </Card>
          </Link>
        ))}
      </div>
    </div>
  );
}
